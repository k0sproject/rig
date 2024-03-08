package initsystem

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/k0sproject/rig/v2/cmd"
	"github.com/k0sproject/rig/v2/sh"
)

// OpenRC is found on some linux systems, often installed on Alpine for example.
type OpenRC struct{}

const rcservice = sh.CommandBuilder("rc-service")

var rcserviceCmd = rcservice.Args

// StartService starts a service.
func (i OpenRC) StartService(ctx context.Context, h cmd.ContextRunner, s string) error {
	if err := h.ExecContext(ctx, rcserviceCmd(s, "start").String()); err != nil {
		return fmt.Errorf("failed to start service %s: %w", s, err)
	}
	return nil
}

// StopService stops a service.
func (i OpenRC) StopService(ctx context.Context, h cmd.ContextRunner, s string) error {
	if err := h.ExecContext(ctx, rcserviceCmd(s, "stop").String()); err != nil {
		return fmt.Errorf("failed to stop service %s: %w", s, err)
	}
	return nil
}

// ServiceScriptPath returns the path to a service configuration file.
func (i OpenRC) ServiceScriptPath(ctx context.Context, h cmd.ContextRunner, s string) (string, error) {
	out, err := h.ExecOutputContext(ctx, rcserviceCmd("-r", s).String())
	if err != nil {
		return "", fmt.Errorf("failed to get service script path for %s: %w", s, err)
	}
	return strings.TrimSpace(out), nil
}

// RestartService restarts a service.
func (i OpenRC) RestartService(ctx context.Context, h cmd.ContextRunner, s string) error {
	if err := h.ExecContext(ctx, rcserviceCmd(s, "restart").String()); err != nil {
		return fmt.Errorf("failed to restart service %s: %w", s, err)
	}
	return nil
}

// EnableService enables a service.
func (i OpenRC) EnableService(ctx context.Context, h cmd.ContextRunner, s string) error {
	if err := h.ExecContext(ctx, sh.Command("rc-update", "add", s)); err != nil {
		return fmt.Errorf("failed to enable service %s: %w", s, err)
	}
	return nil
}

// DisableService disables a service.
func (i OpenRC) DisableService(ctx context.Context, h cmd.ContextRunner, s string) error {
	if err := h.ExecContext(ctx, sh.Command("rc-update", "del", s)); err != nil {
		return fmt.Errorf("failed to disable service %s: %w", s, err)
	}
	return nil
}

// ServiceIsRunning returns true if a service is running.
func (i OpenRC) ServiceIsRunning(ctx context.Context, h cmd.ContextRunner, s string) bool {
	return h.ExecContext(ctx, rcserviceCmd(s, "status").Pipe("grep", "-q", "status: started").String()) == nil
}

// ServiceEnvironmentPath returns a path to an environment override file path.
func (i OpenRC) ServiceEnvironmentPath(_ context.Context, _ cmd.ContextRunner, s string) (string, error) {
	return path.Join("/etc/conf.d", s), nil
}

// ServiceEnvironmentContent returns a formatted string for a service environment override file.
func (i OpenRC) ServiceEnvironmentContent(env map[string]string) string {
	var b strings.Builder
	b.Grow(len(env) * 24)
	for k, v := range env {
		_, _ = fmt.Fprintf(&b, "export %s=%s\n", k, v)
	}

	return b.String()
}

// RegisterOpenRC registers OpenRC to a repository.
func RegisterOpenRC(repo *Provider) {
	repo.Register(func(c cmd.ContextRunner) (ServiceManager, bool) {
		if c.IsWindows() {
			return nil, false
		}
		if c.ExecContext(context.Background(), `command -v openrc-init > /dev/null 2>&1 || (stat /etc/inittab > /dev/null 2>&1 && (grep ::sysinit: /etc/inittab | grep -q openrc) )`) != nil {
			return nil, false
		}
		return &OpenRC{}, true
	})
}
