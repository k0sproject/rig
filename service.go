package rig

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"time"

	"github.com/k0sproject/rig/v2/cmd"
	"github.com/k0sproject/rig/v2/initsystem"
)

// serviceFS is the subset of remotefs.FS that Service uses for SetEnvironment.
type serviceFS interface {
	MkdirAll(path string, perm fs.FileMode) error
	WriteFile(path string, data []byte, perm fs.FileMode) error
}

type serviceState int

const (
	// serviceStateStopped is the stopped state.
	serviceStateStopped serviceState = 0
	// serviceStateStarted is the started state.
	serviceStateStarted serviceState = 1
)

// Service running on a host.
type Service struct {
	runner  cmd.ContextRunner
	name    string
	initsys initsystem.ServiceManager
	fs      serviceFS
}

// Name returns the name of the service.
func (m *Service) Name() string {
	return m.name
}

// String returns a string representation of the service.
func (m *Service) String() string {
	return fmt.Sprintf("%s@%s", m.Name(), m.runner.String())
}

// Start the service. If ctx has no deadline, a 2-minute default timeout is applied.
func (m *Service) Start(ctx context.Context) error {
	ctx, cancel := withServiceTimeout(ctx)
	defer cancel()
	if err := m.initsys.StartService(ctx, m.runner, m.name); err != nil {
		return fmt.Errorf("failed to start service '%s': %w", m.name, err)
	}
	return m.waitState(ctx, serviceStateStarted)
}

// Stop the service. If ctx has no deadline, a 2-minute default timeout is applied.
func (m *Service) Stop(ctx context.Context) error {
	ctx, cancel := withServiceTimeout(ctx)
	defer cancel()
	if err := m.initsys.StopService(ctx, m.runner, m.name); err != nil {
		return fmt.Errorf("failed to stop service '%s': %w", m.name, err)
	}
	return m.waitState(ctx, serviceStateStopped)
}

// Restart the service. For init systems with a native restart operation, a
// 2-minute default timeout is applied if ctx has no deadline. For the
// stop+start fallback, each step applies its own independent default timeout
// so the combined operation can take up to 4 minutes.
func (m *Service) Restart(ctx context.Context) error {
	if restarter, ok := m.initsys.(initsystem.ServiceManagerRestarter); ok {
		rctx, cancel := withServiceTimeout(ctx)
		defer cancel()
		if err := restarter.RestartService(rctx, m.runner, m.name); err != nil {
			return fmt.Errorf("failed to restart service '%s': %w", m.name, err)
		}
		return m.waitState(rctx, serviceStateStarted)
	}
	// Fall back to stop+start; each applies its own default timeout independently.
	if err := m.Stop(ctx); err != nil {
		return fmt.Errorf("failed to stop service '%s' for restart: %w", m.name, err)
	}
	if err := m.Start(ctx); err != nil {
		return fmt.Errorf("failed to restart service '%s': %w", m.name, err)
	}
	return nil
}

const (
	serviceStatePollMinInterval = 100 * time.Millisecond
	serviceStatePollMaxInterval = 1000 * time.Millisecond
	// serviceDefaultTimeout is applied when a caller passes a context with no deadline.
	// It covers systemd's default 90-second start/stop timeout plus overhead.
	serviceDefaultTimeout = 2 * time.Minute
)

// withServiceTimeout wraps ctx with serviceDefaultTimeout when ctx has no deadline.
// The returned cancel must always be called.
func withServiceTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, serviceDefaultTimeout)
}

func (m *Service) waitState(ctx context.Context, state serviceState) error {
	delay := serviceStatePollMinInterval
	timer := time.NewTimer(delay)
	defer timer.Stop()
	for {
		running := m.initsys.ServiceIsRunning(ctx, m.runner, m.name)
		wantRunning := state == serviceStateStarted
		if running == wantRunning {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("service '%s' did not reach the desired state: %w", m.name, ctx.Err())
		case <-timer.C:
			delay *= 2
			if delay > serviceStatePollMaxInterval {
				delay = serviceStatePollMaxInterval
			}
			timer.Reset(delay)
		}
	}
}

// Enable the service. If ctx has no deadline, a 2-minute default timeout is applied.
func (m *Service) Enable(ctx context.Context) error {
	ctx, cancel := withServiceTimeout(ctx)
	defer cancel()
	if err := m.initsys.EnableService(ctx, m.runner, m.name); err != nil {
		return fmt.Errorf("failed to enable service: %w", err)
	}
	if reloader, ok := m.initsys.(initsystem.ServiceManagerReloader); ok {
		if err := reloader.DaemonReload(ctx, m.runner); err != nil {
			return fmt.Errorf("failed to reload init system after enabling service '%s': %w", m.name, err)
		}
	}
	return nil
}

// Disable the service. If ctx has no deadline, a 2-minute default timeout is applied.
func (m *Service) Disable(ctx context.Context) error {
	ctx, cancel := withServiceTimeout(ctx)
	defer cancel()
	if err := m.initsys.DisableService(ctx, m.runner, m.name); err != nil {
		return fmt.Errorf("failed to disable service '%s': %w", m.name, err)
	}
	if reloader, ok := m.initsys.(initsystem.ServiceManagerReloader); ok {
		if err := reloader.DaemonReload(ctx, m.runner); err != nil {
			return fmt.Errorf("failed to reload init system after disabling service '%s': %w", m.name, err)
		}
	}
	return nil
}

// ScriptPath returns the path to the service script.
// If ctx has no deadline, a 2-minute default timeout is applied.
func (m *Service) ScriptPath(ctx context.Context) (string, error) {
	ctx, cancel := withServiceTimeout(ctx)
	defer cancel()
	scriptPath, err := m.initsys.ServiceScriptPath(ctx, m.runner, m.name)
	if err != nil {
		return "", fmt.Errorf("failed to get service script path: %w", err)
	}
	return scriptPath, nil
}

// IsRunning returns true if the service is running.
// If ctx has no deadline, a 2-minute default timeout is applied.
func (m *Service) IsRunning(ctx context.Context) bool {
	ctx, cancel := withServiceTimeout(ctx)
	defer cancel()
	return m.initsys.ServiceIsRunning(ctx, m.runner, m.name)
}

var (
	errLogReaderNotSupported    = errors.New("init system provider does not implement log reader")
	errLogStreamerNotSupported  = errors.New("init system provider does not support log streaming")
	errEnvManagerNotSupported   = errors.New("init system provider does not support service environment management")
	errDaemonReloadNotSupported = errors.New("init system provider does not support daemon-reload")
	errServiceFSNotAvailable    = errors.New("service has no filesystem access; use client.Service() instead of GetService()")
)

// Logs returns latest log lines for the service.
// If ctx has no deadline, a 2-minute default timeout is applied.
func (m *Service) Logs(ctx context.Context, lines int) ([]string, error) {
	ctx, cancel := withServiceTimeout(ctx)
	defer cancel()
	logreader, ok := m.initsys.(initsystem.ServiceManagerLogReader)
	if !ok {
		return nil, errLogReaderNotSupported
	}

	rows, err := logreader.ServiceLogs(ctx, m.runner, m.name, lines)
	if err != nil {
		return nil, fmt.Errorf("get logs: %w", err)
	}
	return rows, nil
}

// StreamLogs streams new service log output to w from the current point in time until ctx is
// cancelled or an error occurs. Cancelling ctx is the expected way to stop streaming and does
// not return an error.
func (m *Service) StreamLogs(ctx context.Context, w io.Writer) error {
	streamer, ok := m.initsys.(initsystem.ServiceManagerLogStreamer)
	if !ok {
		return errLogStreamerNotSupported
	}
	if err := streamer.StreamServiceLogs(ctx, m.runner, m.name, w); err != nil {
		if ctx.Err() != nil {
			return nil //nolint:nilerr // context cancellation is the expected stop signal
		}
		return fmt.Errorf("stream logs for service '%s': %w", m.name, err)
	}
	return nil
}

// SetEnvironment writes environment variable overrides for the service and triggers a daemon-reload
// if the init system requires it (e.g. systemd). The init system determines the file path and format;
// any existing content is replaced. If ctx has no deadline, a 2-minute default timeout is applied.
func (m *Service) SetEnvironment(ctx context.Context, env map[string]string) error {
	ctx, cancel := withServiceTimeout(ctx)
	defer cancel()
	envManager, ok := m.initsys.(initsystem.ServiceEnvironmentManager)
	if !ok {
		return errEnvManagerNotSupported
	}
	if m.fs == nil {
		return errServiceFSNotAvailable
	}
	envPath, err := envManager.ServiceEnvironmentPath(ctx, m.runner, m.name)
	if err != nil {
		return fmt.Errorf("get environment path for service '%s': %w", m.name, err)
	}
	if err := m.fs.MkdirAll(path.Dir(envPath), 0o755); err != nil {
		return fmt.Errorf("create environment directory for service '%s': %w", m.name, err)
	}
	content := envManager.ServiceEnvironmentContent(env)
	if err := m.fs.WriteFile(envPath, []byte(content), 0o600); err != nil {
		return fmt.Errorf("write environment file for service '%s': %w", m.name, err)
	}
	if reloader, ok := m.initsys.(initsystem.ServiceManagerReloader); ok {
		if err := reloader.DaemonReload(ctx, m.runner); err != nil {
			return fmt.Errorf("daemon-reload after setting environment for service '%s': %w", m.name, err)
		}
	}
	return nil
}

// DaemonReload triggers a daemon-reload on the init system, if supported. This is useful after
// manually writing service unit files outside of rig. Enable and Disable call this automatically.
// If ctx has no deadline, a 2-minute default timeout is applied.
func (m *Service) DaemonReload(ctx context.Context) error {
	ctx, cancel := withServiceTimeout(ctx)
	defer cancel()
	reloader, ok := m.initsys.(initsystem.ServiceManagerReloader)
	if !ok {
		return errDaemonReloadNotSupported
	}
	if err := reloader.DaemonReload(ctx, m.runner); err != nil {
		return fmt.Errorf("daemon-reload: %w", err)
	}
	return nil
}

// GetService returns a manager for a single service using an auto-detected service manager implementation from the default providers.
func GetService(runner cmd.ContextRunner, name string) (*Service, error) {
	initsys, err := GetServiceManager(runner)
	if err != nil {
		return nil, fmt.Errorf("get init system service manager: %w", err)
	}
	return &Service{
		runner:  runner,
		name:    name,
		initsys: initsys,
	}, nil
}
