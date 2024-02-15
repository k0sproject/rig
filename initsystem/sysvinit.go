package initsystem

import (
	"context"
	"fmt"

	"github.com/alessio/shellescape"
	"github.com/k0sproject/rig/exec"
)

// SysVinit is the service manager for SysVinit.
type SysVinit struct{}

// StartService starts a service.
func (i SysVinit) StartService(ctx context.Context, h exec.ContextRunner, s string) error {
	if err := h.ExecContext(ctx, "/etc/init.d/%s start", s); err != nil {
		return fmt.Errorf("failed to start service %s: %w", s, err)
	}
	return nil
}

// StopService stops a service.
func (i SysVinit) StopService(ctx context.Context, h exec.ContextRunner, s string) error {
	if err := h.ExecContext(ctx, "/etc/init.d/%s stop", s); err != nil {
		return fmt.Errorf("failed to stop service %s: %w", s, err)
	}
	return nil
}

// RestartService restarts a service.
func (i SysVinit) RestartService(ctx context.Context, h exec.ContextRunner, s string) error {
	if err := h.ExecContext(ctx, "/etc/init.d/%s restart", s); err != nil {
		return fmt.Errorf("failed to restart service %s: %w", s, err)
	}
	return nil
}

// ServiceIsRunning checks if a service is running.
func (i SysVinit) ServiceIsRunning(ctx context.Context, h exec.ContextRunner, s string) bool {
	return h.ExecContext(ctx, "/etc/init.d/%s status", s) == nil
}

// ServiceScriptPath returns the path to a SysVinit service script.
func (i SysVinit) ServiceScriptPath(_ context.Context, _ exec.ContextRunner, s string) (string, error) {
	return "/etc/init.d/" + s, nil
}

// EnableService for SysVinit tries to create symlinks in the appropriate runlevel directories.
func (i SysVinit) EnableService(ctx context.Context, h exec.ContextRunner, service string) error {
	if h.ExecContext(ctx, "command -v chkconfig") == nil {
		if err := h.ExecContext(ctx, "chkconfig --add %s", service); err != nil {
			return fmt.Errorf("failed to add service %s to chkconfig: %w", service, err)
		}
		return nil
	}
	for runlevel := 2; runlevel <= 5; runlevel++ {
		symlinkPath := fmt.Sprintf("/etc/rc%d.d/S99%s", runlevel, service)
		targetPath := "/etc/init.d/" + service
		if err := h.ExecContext(ctx, "ln -s %s %s", shellescape.Quote(targetPath), shellescape.Quote(symlinkPath)); err != nil {
			return fmt.Errorf("failed to create symlink for service %s in runlevel %d: %w", service, runlevel, err)
		}
	}
	return nil
}

// DisableService for SysVinit tries to remove symlinks in the appropriate runlevel directories.
func (i SysVinit) DisableService(ctx context.Context, h exec.ContextRunner, s string) error {
	if h.ExecContext(ctx, "command -v chkconfig") == nil {
		if err := h.ExecContext(ctx, "chkconfig --del %s", s); err != nil {
			return fmt.Errorf("failed to remove service %s from chkconfig: %w", s, err)
		}
		return nil
	}
	for runlevel := 2; runlevel <= 5; runlevel++ {
		symlinkPath := fmt.Sprintf("/etc/rc%d.d/S99%s", runlevel, s)
		if err := h.ExecContext(ctx, "rm -f %s", shellescape.Quote(symlinkPath)); err != nil {
			return fmt.Errorf("failed to remove symlink for service %s in runlevel %d: %w", s, runlevel, err)
		}
	}
	return nil
}

// RegisterSysVinit registers SysVinit in a repository.
func RegisterSysVinit(repo *Provider) {
	repo.Register(func(c exec.ContextRunner) ServiceManager {
		if c.IsWindows() {
			return nil
		}
		if c.ExecContext(context.Background(), "test -d /etc/init.d") != nil {
			return nil
		}

		return SysVinit{}
	})
}
