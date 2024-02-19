package rig

import (
	"context"
	"errors"
	"fmt"

	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/initsystem"
)

type serviceState int

const (
	// serviceStateStopped is the stopped state.
	serviceStateStopped serviceState = 0
	// serviceStateStarted is the started state.
	serviceStateStarted serviceState = 1
)

// ErrEmptyResult is returned when a command returns an empty result.
var ErrEmptyResult = errors.New("empty result")

// Service is an interface for managing a service on an initsystem.
type Service struct {
	runner  exec.ContextRunner
	name    string
	initsys initsystem.ServiceManager
}

// Name returns the name of the service.
func (m *Service) Name() string {
	return m.name
}

// String returns a string representation of the service.
func (m *Service) String() string {
	return fmt.Sprintf("%s@%s", m.Name(), m.runner.String())
}

// Start the service.
func (m *Service) Start(ctx context.Context) error {
	if err := m.initsys.StartService(ctx, m.runner, m.name); err != nil {
		return fmt.Errorf("failed to start service '%s': %w", m.name, err)
	}
	return m.waitState(ctx, serviceStateStarted)
}

// Stop the service.
func (m *Service) Stop(ctx context.Context) error {
	if err := m.initsys.StopService(ctx, m.runner, m.name); err != nil {
		return fmt.Errorf("failed to stop service '%s': %w", m.name, err)
	}
	return m.waitState(ctx, serviceStateStopped)
}

// Restart the service.
func (m *Service) Restart(ctx context.Context) error {
	if restarter, ok := m.initsys.(initsystem.ServiceManagerRestarter); ok {
		if err := restarter.RestartService(ctx, m.runner, m.name); err != nil {
			return fmt.Errorf("failed to restart service '%s': %w", m.name, err)
		}
	}
	if err := m.Stop(ctx); err != nil {
		return fmt.Errorf("failed to stop service '%s' for restart: %w", m.name, err)
	}
	if err := m.Start(ctx); err != nil {
		return fmt.Errorf("failed to restart service '%s: %w", m.name, err)
	}
	return nil
}

func (m *Service) waitState(ctx context.Context, state serviceState) error {
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("service '%s' did not reach the desired state: %w", m.name, ctx.Err())
		default:
			if m.initsys.ServiceIsRunning(ctx, m.runner, m.name) == (state == serviceStateStarted) {
				return nil
			}
		}
	}
}

// Enable the service.
func (m *Service) Enable(ctx context.Context) error {
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

// Disable the service.
func (m *Service) Disable(ctx context.Context) error {
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
func (m *Service) ScriptPath(ctx context.Context) (string, error) {
	path, err := m.initsys.ServiceScriptPath(ctx, m.runner, m.name)
	if err != nil {
		return "", fmt.Errorf("failed to get service script path: %w", err)
	}
	return path, nil
}

// IsRunning returns true if the service is running.
func (m *Service) IsRunning(ctx context.Context) bool {
	return m.initsys.ServiceIsRunning(ctx, m.runner, m.name)
}

var errLogReaderNotSupported = errors.New("init system provider does not implement log reader")

// Logs returns latest log lines for the service.
func (m *Service) Logs(ctx context.Context, lines int) ([]string, error) {
	logreader, ok := m.initsys.(initsystem.ServiceManagerLogReader)
	if !ok {
		return nil, errLogReaderNotSupported
	}

	rows, err := logreader.ServiceLogs(ctx, m.runner, m.name, lines)
	if err != nil {
		return nil, fmt.Errorf("get logs: %w", err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("get logs: %w", ErrEmptyResult)
	}
	return rows, nil
}

// GetService returns a manager for a single service using an auto-detected service manager implementation from the default providers.
func GetService(runner exec.ContextRunner, name string) (*Service, error) {
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
