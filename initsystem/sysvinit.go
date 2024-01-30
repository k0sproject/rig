package initsystem

import (
	"context"
	"fmt"

	"github.com/alessio/shellescape"
	"github.com/k0sproject/rig/exec"
)

type SysVinit struct{}

func (i SysVinit) StartService(ctx context.Context, h exec.ContextRunner, s string) error {
	if err := h.ExecContext(ctx, "/etc/init.d/%s start", s); err != nil {
		return fmt.Errorf("failed to start service %s: %w", s, err)
	}
	return nil
}

func (i SysVinit) StopService(ctx context.Context, h exec.ContextRunner, s string) error {
	if err := h.ExecContext(ctx, "/etc/init.d/%s stop", s); err != nil {
		return fmt.Errorf("failed to stop service %s: %w", s, err)
	}
	return nil
}

func (i SysVinit) RestartService(ctx context.Context, h exec.ContextRunner, s string) error {
	if err := h.ExecContext(ctx, "/etc/init.d/%s restart", s); err != nil {
		return fmt.Errorf("failed to restart service %s: %w", s, err)
	}
	return nil
}

func (i SysVinit) ServiceIsRunning(ctx context.Context, h exec.ContextRunner, s string) bool {
	return h.ExecContext(ctx, "/etc/init.d/%s status", s) == nil
}

// ServiceScriptPath returns the path to a SysVinit service script
func (i SysVinit) ServiceScriptPath(ctx context.Context, h exec.ContextRunner, s string) (string, error) {
	return "/etc/init.d/" + s, nil
}

// EnableService for SysVinit tries to create symlinks in the appropriate runlevel directories
func (i SysVinit) EnableService(ctx context.Context, h exec.ContextRunner, s string) error {
	if h.ExecContext(ctx, "command -v chkconfig") == nil {
		return h.ExecContext(ctx, "chkconfig --add %s", s)
	}
	for runlevel := 2; runlevel <= 5; runlevel++ {
		symlinkPath := fmt.Sprintf("/etc/rc%d.d/S99%s", runlevel, s)
		targetPath := fmt.Sprintf("/etc/init.d/%s", s)
		if err := h.ExecContext(ctx, "ln -s %s %s", shellescape.Quote(targetPath), shellescape.Quote(symlinkPath)); err != nil {
			return fmt.Errorf("failed to create symlink for service %s in runlevel %d: %w", s, runlevel, err)
		}
	}
	return nil
}

// DisableService for SysVinit tries to remove symlinks in the appropriate runlevel directories
func (i SysVinit) DisableService(ctx context.Context, h exec.ContextRunner, s string) error {
	if h.ExecContext(ctx, "command -v chkconfig") == nil {
		return h.ExecContext(ctx, "chkconfig --del %s", s)
	}
	for runlevel := 2; runlevel <= 5; runlevel++ {
		symlinkPath := fmt.Sprintf("/etc/rc%d.d/S99%s", runlevel, s)
		if err := h.ExecContext(ctx, "rm -f %s", shellescape.Quote(symlinkPath)); err != nil {
			return fmt.Errorf("failed to remove symlink for service %s in runlevel %d: %w", s, runlevel, err)
		}
	}
	return nil
}

func RegisterSysVinit(repo *Repository) {
	repo.Register("sysvinit", func(c exec.ContextRunner) ServiceManager {
		if c.IsWindows() {
			return nil
		}
		if c.ExecContext(context.Background(), "test -d /etc/init.d") != nil {
			return nil
		}

		return SysVinit{}
	})
}
