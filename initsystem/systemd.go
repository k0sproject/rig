package initsystem

import (
	"context"
	"fmt"
	"path"
	"strconv"
	"strings"

	"github.com/k0sproject/rig/exec"
)

// Systemd is found by default on most linux distributions today.
type Systemd struct{}

// StartService starts a service.
func (i Systemd) StartService(ctx context.Context, h exec.ContextRunner, s string) error {
	if err := h.ExecContext(ctx, "systemctl start %s 2> /dev/null", s); err != nil {
		return fmt.Errorf("failed to start service %s: %w", s, err)
	}
	return nil
}

// EnableService enables a service.
func (i Systemd) EnableService(ctx context.Context, h exec.ContextRunner, s string) error {
	if err := h.ExecContext(ctx, "systemctl enable %s 2> /dev/null", s); err != nil {
		return fmt.Errorf("failed to enable service %s: %w", s, err)
	}
	return nil
}

// DisableService disables a service.
func (i Systemd) DisableService(ctx context.Context, h exec.ContextRunner, s string) error {
	if err := h.ExecContext(ctx, "systemctl disable %s 2> /dev/null", s); err != nil {
		return fmt.Errorf("failed to disable service %s: %w", s, err)
	}
	return nil
}

// StopService stops a service.
func (i Systemd) StopService(ctx context.Context, h exec.ContextRunner, s string) error {
	if err := h.ExecContext(ctx, "systemctl stop %s 2> /dev/null", s); err != nil {
		return fmt.Errorf("failed to stop service %s: %w", s, err)
	}
	return nil
}

// RestartService restarts a service.
func (i Systemd) RestartService(ctx context.Context, h exec.ContextRunner, s string) error {
	if err := h.ExecContext(ctx, "systemctl restart %s 2> /dev/null", s); err != nil {
		return fmt.Errorf("failed to restart service %s: %w", s, err)
	}
	return nil
}

// DaemonReload reloads init system configuration.
func (i Systemd) DaemonReload(ctx context.Context, h exec.ContextRunner) error {
	if err := h.ExecContext(ctx, "systemctl daemon-reload 2> /dev/null"); err != nil {
		return fmt.Errorf("failed to daemon-reload: %w", err)
	}
	return nil
}

// ServiceIsRunning returns true if a service is running.
func (i Systemd) ServiceIsRunning(ctx context.Context, h exec.ContextRunner, s string) bool {
	return h.ExecContext(ctx, `systemctl status %s 2> /dev/null | grep -q "(running)"`, s) == nil
}

// ServiceScriptPath returns the path to a service configuration file.
func (i Systemd) ServiceScriptPath(ctx context.Context, h exec.ContextRunner, s string) (string, error) {
	out, err := h.ExecOutputContext(ctx, `systemctl show -p FragmentPath %s.service 2> /dev/null | cut -d"=" -f2`, s)
	if err != nil {
		return "", fmt.Errorf("failed to get service %s script path: %w", s, err)
	}
	return strings.TrimSpace(out), nil
}

// ServiceEnvironmentPath returns a path to an environment override file path.
func (i Systemd) ServiceEnvironmentPath(ctx context.Context, h exec.ContextRunner, s string) (string, error) {
	sp, err := i.ServiceScriptPath(ctx, h, s)
	if err != nil {
		return "", err
	}
	dn := path.Dir(sp)
	return path.Join(dn, s+"service.d", "env.conf"), nil
}

// ServiceEnvironmentContent returns a formatted string for a service environment override file.
func (i Systemd) ServiceEnvironmentContent(env map[string]string) string {
	var b strings.Builder
	fmt.Fprintln(&b, "[Service]")
	for k, v := range env {
		env := fmt.Sprintf("%s=%s", k, v)
		_, _ = fmt.Fprintf(&b, "Environment=%s\n", strconv.Quote(env))
	}

	return b.String()
}

// ServiceLogs returns the last n lines of a service log.
func (i Systemd) ServiceLogs(ctx context.Context, h exec.ContextRunner, s string, lines int) ([]string, error) {
	out, err := h.ExecOutputContext(ctx, "journalctl -n %d -u %s 2> /dev/null", lines, s)
	if err != nil {
		return nil, fmt.Errorf("failed to get logs for service %s: %w", s, err)
	}

	return strings.Split(out, "\n"), nil
}

// RegisterSystemd registers systemd into a repository.
func RegisterSystemd(repo *Provider) {
	repo.Register(func(c exec.ContextRunner) (ServiceManager, bool) {
		if c.IsWindows() {
			return nil, false
		}
		if c.ExecContext(context.Background(), "stat /run/systemd/system") != nil {
			return nil, false
		}

		return Systemd{}, true
	})
}
