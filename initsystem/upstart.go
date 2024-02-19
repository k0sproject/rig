package initsystem

import (
	"context"
	"fmt"
	"strings"

	"github.com/alessio/shellescape"
	"github.com/k0sproject/rig/exec"
)

// Upstart is the init system used by Ubuntu 14.04 and older.
type Upstart struct{}

// StartService starts a service.
func (i Upstart) StartService(ctx context.Context, h exec.ContextRunner, s string) error {
	if err := h.ExecContext(ctx, "initctl start %s", s); err != nil {
		return fmt.Errorf("failed to start service %s: %w", s, err)
	}
	return nil
}

// StopService stops a service.
func (i Upstart) StopService(ctx context.Context, h exec.ContextRunner, s string) error {
	if err := h.ExecContext(ctx, "initctl stop %s", s); err != nil {
		return fmt.Errorf("failed to stop service %s: %w", s, err)
	}
	return nil
}

// RestartService restarts a service.
func (i Upstart) RestartService(ctx context.Context, h exec.ContextRunner, s string) error {
	if err := h.ExecContext(ctx, "initctl restart %s", s); err != nil {
		return fmt.Errorf("failed to restart service %s: %w", s, err)
	}
	return nil
}

// ServiceIsRunning checks if a service is running.
func (i Upstart) ServiceIsRunning(ctx context.Context, h exec.ContextRunner, s string) bool {
	return h.ExecContext(ctx, "initctl status %s | grep -q 'start/running'", s) == nil
}

// ServiceScriptPath returns the path to an Upstart service configuration file.
func (i Upstart) ServiceScriptPath(_ context.Context, _ exec.ContextRunner, s string) (string, error) {
	return "/etc/init/" + s + ".conf", nil
}

// EnableService for Upstart (might involve creating a symlink or managing an override file).
func (i Upstart) EnableService(ctx context.Context, h exec.ContextRunner, s string) error {
	overridePath := fmt.Sprintf("/etc/init/%s.override", s)
	if err := h.ExecContext(ctx, "rm -f %s", shellescape.Quote(overridePath)); err != nil {
		return fmt.Errorf("failed to remove override file %s: %w", overridePath, err)
	}
	return nil
}

// DisableService for Upstart.
func (i Upstart) DisableService(ctx context.Context, h exec.ContextRunner, s string) error {
	overridePath := fmt.Sprintf("/etc/init/%s.override", s)
	if err := h.ExecContext(ctx, "echo 'manual' > %s", shellescape.Quote(overridePath)); err != nil {
		return fmt.Errorf("failed to create override file %s: %w", overridePath, err)
	}
	return nil
}

// ServiceLogs returns the logs for a service from /var/log/upstart/<service>.log. It's not guaranteed
// that the log file exists or that the service logs to this file.
func (i Upstart) ServiceLogs(ctx context.Context, h exec.ContextRunner, s string, lines int) ([]string, error) {
	out, err := h.ExecOutputContext(ctx, "tail -n %d %s", lines, shellescape.Quote("/var/log/upstart/"+s+".log"))
	if err != nil {
		return nil, fmt.Errorf("failed to get logs for service %s: %w", s, err)
	}
	return strings.Split(out, "\n"), nil
}

// RegisterUpstart registers Upstart in a repository.
func RegisterUpstart(repo *Provider) {
	repo.Register(func(c exec.ContextRunner) (ServiceManager, bool) {
		if c.IsWindows() {
			return nil, false
		}
		if c.ExecContext(context.Background(), "command -v initctl > /dev/null 2>&1") != nil {
			return nil, false
		}

		return Upstart{}, true
	})
}
