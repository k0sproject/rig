package initsystem

import (
	"context"
	"fmt"

	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/sh"
)

const initd = "/etc/init.d/"

// SysVinit is the service manager for SysVinit.
type SysVinit struct{}

// StartService starts a service.
func (i SysVinit) StartService(ctx context.Context, h exec.ContextRunner, s string) error {
	if err := h.ExecContext(ctx, sh.Command(initd+s, "start")); err != nil {
		return fmt.Errorf("failed to start service %s: %w", s, err)
	}
	return nil
}

// StopService stops a service.
func (i SysVinit) StopService(ctx context.Context, h exec.ContextRunner, s string) error {
	if err := h.ExecContext(ctx, sh.Command(initd+s, "stop")); err != nil {
		return fmt.Errorf("failed to stop service %s: %w", s, err)
	}
	return nil
}

// RestartService restarts a service.
func (i SysVinit) RestartService(ctx context.Context, h exec.ContextRunner, s string) error {
	if err := h.ExecContext(ctx, sh.Command(initd+s, "restart")); err != nil {
		return fmt.Errorf("failed to restart service %s: %w", s, err)
	}
	return nil
}

// ServiceIsRunning checks if a service is running.
func (i SysVinit) ServiceIsRunning(ctx context.Context, h exec.ContextRunner, s string) bool {
	return h.ExecContext(ctx, sh.Command(initd+s, "status")) == nil
}

// ServiceScriptPath returns the path to a SysVinit service script.
func (i SysVinit) ServiceScriptPath(_ context.Context, _ exec.ContextRunner, s string) (string, error) {
	return initd + s, nil
}

// EnableService for SysVinit tries to create symlinks in the appropriate runlevel directories.
func (i SysVinit) EnableService(ctx context.Context, h exec.ContextRunner, service string) error {
	if h.ExecContext(ctx, "command -v chkconfig") == nil {
		if err := h.ExecContext(ctx, sh.Command("chkconfig", "--add", service)); err != nil {
			return fmt.Errorf("failed to add service %s to chkconfig: %w", service, err)
		}
		return nil
	}
	for runlevel := 2; runlevel <= 5; runlevel++ {
		symlinkPath := fmt.Sprintf("/etc/rc%d.d/S99%s", runlevel, service)
		targetPath := initd + service
		if err := h.ExecContext(ctx, sh.Command("ln", "-s", targetPath, symlinkPath)); err != nil {
			return fmt.Errorf("failed to create symlink for service %s in runlevel %d: %w", service, runlevel, err)
		}
	}
	return nil
}

// DisableService for SysVinit tries to remove symlinks in the appropriate runlevel directories.
func (i SysVinit) DisableService(ctx context.Context, h exec.ContextRunner, s string) error {
	if h.ExecContext(ctx, "command -v chkconfig") == nil {
		if err := h.ExecContext(ctx, sh.Command("chkconfig", "--del", s)); err != nil {
			return fmt.Errorf("failed to remove service %s from chkconfig: %w", s, err)
		}
		return nil
	}
	for runlevel := 2; runlevel <= 5; runlevel++ {
		symlinkPath := fmt.Sprintf("/etc/rc%d.d/S99%s", runlevel, s)
		if err := h.ExecContext(ctx, sh.Command("rm", "-f", symlinkPath)); err != nil {
			return fmt.Errorf("failed to remove symlink for service %s in runlevel %d: %w", s, runlevel, err)
		}
	}
	return nil
}

// RegisterSysVinit registers SysVinit in a repository.
func RegisterSysVinit(repo *Provider) {
	repo.Register(func(c exec.ContextRunner) (ServiceManager, bool) {
		if c.IsWindows() {
			return nil, false
		}
		if c.ExecContext(context.Background(), "test -d /etc/init.d") != nil {
			return nil, false
		}

		return SysVinit{}, true
	})
}
