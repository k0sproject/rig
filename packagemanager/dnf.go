package packagemanager

import (
	"context"
	"fmt"

	"github.com/k0sproject/rig/exec"
)

type Dnf struct {
	exec.ContextRunner
}

func (d *Dnf) Install(ctx context.Context, packageNames ...string) error {
	if err := d.ExecContext(ctx, buildCommand("dnf install -y", packageNames...)); err != nil {
		return fmt.Errorf("failed to install dnf packages: %w", err)
	}
	return nil
}

func (d *Dnf) Remove(ctx context.Context, packageNames ...string) error {
	if err := d.ExecContext(ctx, buildCommand("dnf remove -y", packageNames...)); err != nil {
		return fmt.Errorf("failed to remove dnf packages: %w", err)
	}
	return nil
}

func (d *Dnf) Update(ctx context.Context) error {
	if err := d.ExecContext(ctx, "dnf makecache"); err != nil {
		return fmt.Errorf("failed to update dnf: %w", err)
	}
	return nil
}

func RegisterDnf(repository *Repository) {
	repository.Register(func(c exec.ContextRunner) PackageManager {
		if c.IsWindows() {
			return nil
		}
		if c.ExecContext(context.Background(), "command -v dnf") != nil {
			return nil
		}
		return &Dnf{c}
	})
}
