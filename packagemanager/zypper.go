package packagemanager

import (
	"context"
	"fmt"

	"github.com/k0sproject/rig/exec"
)

type Zypper struct {
	exec.ContextRunner
}

func (z *Zypper) Install(ctx context.Context, packageNames ...string) error {
	if err := z.ExecContext(ctx, buildCommand("zypper install -y", packageNames...)); err != nil {
		return fmt.Errorf("failed to install zypper packages: %w", err)
	}
	return nil
}

func (z *Zypper) Remove(ctx context.Context, packageNames ...string) error {
	if err := z.ExecContext(ctx, buildCommand("zypper remove -y", packageNames...)); err != nil {
		return fmt.Errorf("failed to remove zypper packages: %w", err)
	}
	return nil
}

func (z *Zypper) Update(ctx context.Context) error {
	if err := z.ExecContext(ctx, "zypper refresh"); err != nil {
		return fmt.Errorf("failed to update zypper: %w", err)
	}
	return nil
}

func RegisterZypper(repository *Repository) {
	repository.Register(func(c exec.ContextRunner) PackageManager {
		if c.IsWindows() {
			return nil
		}
		if c.ExecContext(context.Background(), "command -v zypper") != nil {
			return nil
		}
		return &Zypper{c}
	})
}
