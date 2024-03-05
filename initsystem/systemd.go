package initsystem

import (
	"context"
	"fmt"
	"path"
	"strconv"
	"strings"

	"github.com/k0sproject/rig/cmd"
	"github.com/k0sproject/rig/sh"
	"github.com/k0sproject/rig/sh/shellescape"
)

// Systemd is found by default on most linux distributions today.
type Systemd struct{}

const systemctl = sh.CommandBuilder("systemctl")

var systemctlCmd = systemctl.Args

// StartService starts a service.
func (i Systemd) StartService(ctx context.Context, h cmd.ContextRunner, s string) error {
	if err := h.ExecContext(ctx, systemctlCmd("start", s).String()); err != nil {
		return fmt.Errorf("failed to start service %s: %w", s, err)
	}
	return nil
}

// EnableService enables a service.
func (i Systemd) EnableService(ctx context.Context, h cmd.ContextRunner, s string) error {
	if err := h.ExecContext(ctx, systemctlCmd("enable", s).String()); err != nil {
		return fmt.Errorf("failed to enable service %s: %w", s, err)
	}
	return nil
}

// DisableService disables a service.
func (i Systemd) DisableService(ctx context.Context, h cmd.ContextRunner, s string) error {
	if err := h.ExecContext(ctx, systemctlCmd("disable", s).String()); err != nil {
		return fmt.Errorf("failed to disable service %s: %w", s, err)
	}
	return nil
}

// StopService stops a service.
func (i Systemd) StopService(ctx context.Context, h cmd.ContextRunner, s string) error {
	if err := h.ExecContext(ctx, systemctlCmd("stop", s).String()); err != nil {
		return fmt.Errorf("failed to stop service %s: %w", s, err)
	}
	return nil
}

// RestartService restarts a service.
func (i Systemd) RestartService(ctx context.Context, h cmd.ContextRunner, s string) error {
	if err := h.ExecContext(ctx, systemctlCmd("restart", s).String()); err != nil {
		return fmt.Errorf("failed to restart service %s: %w", s, err)
	}
	return nil
}

// DaemonReload reloads init system configuration.
func (i Systemd) DaemonReload(ctx context.Context, h cmd.ContextRunner) error {
	if err := h.ExecContext(ctx, systemctlCmd("daemon-reload").String()); err != nil {
		return fmt.Errorf("failed to daemon-reload: %w", err)
	}
	return nil
}

// ServiceIsRunning returns true if a service is running.
func (i Systemd) ServiceIsRunning(ctx context.Context, h cmd.ContextRunner, s string) bool {
	return h.ExecContext(ctx, systemctlCmd("status", s).Pipe("grep", "-q", "(running)").String()) == nil
}

// ServiceScriptPath returns the path to a service configuration file.
func (i Systemd) ServiceScriptPath(ctx context.Context, h cmd.ContextRunner, s string) (string, error) {
	out, err := h.ExecOutputContext(ctx, systemctlCmd("show", "-p", "FragmentPath", s+".service").Pipe("cut", `-d"="`, "-f2").String())
	if err != nil {
		return "", fmt.Errorf("failed to get service %s script path: %w", s, err)
	}
	return strings.TrimSpace(out), nil
}

// ServiceEnvironmentPath returns a path to an environment override file path.
func (i Systemd) ServiceEnvironmentPath(ctx context.Context, h cmd.ContextRunner, s string) (string, error) {
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
	b.Grow(10 + (len(env) * 30))
	b.WriteString("[Service]\n")
	for k, v := range env {
		b.WriteString("Environment=")
		b.WriteString(shellescape.Quote(k + "=" + v))
		b.WriteByte('\n')
	}

	return b.String()
}

// ServiceLogs returns the last n lines of a service log.
func (i Systemd) ServiceLogs(ctx context.Context, h cmd.ContextRunner, s string, lines int) ([]string, error) {
	out, err := h.ExecOutputContext(ctx, sh.Command("journalctl", "-n", strconv.Itoa(lines), "-u", s))
	if err != nil {
		return nil, fmt.Errorf("failed to get logs for service %s: %w", s, err)
	}

	return strings.Split(out, "\n"), nil
}

// RegisterSystemd registers systemd into a repository.
func RegisterSystemd(repo *Provider) {
	repo.Register(func(c cmd.ContextRunner) (ServiceManager, bool) {
		if c.IsWindows() {
			return nil, false
		}
		if c.ExecContext(context.Background(), "stat /run/systemd/system") != nil {
			return nil, false
		}

		return Systemd{}, true
	})
}
