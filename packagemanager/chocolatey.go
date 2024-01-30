package packagemanager

import (
	"context"
	"fmt"

	"github.com/k0sproject/rig/exec"
)

type Chocolatey struct {
	exec.ContextRunner
}

func (c *Chocolatey) Install(ctx context.Context, packageNames ...string) error {
	if err := c.ExecContext(ctx, buildCommand("choco install", packageNames...)+" -y"); err != nil {
		return fmt.Errorf("failed to install chocolatey packages: %w", err)
	}
	return nil
}

func (c *Chocolatey) Remove(ctx context.Context, packageNames ...string) error {
	if err := c.ExecContext(ctx, buildCommand("choco uninstall", packageNames...)+" -y"); err != nil {
		return fmt.Errorf("failed to remove chocolatey packages: %w", err)
	}
	return nil
}

func (c *Chocolatey) Update(ctx context.Context) error {
	if err := c.ExecContext(ctx, "choco upgrade all -y"); err != nil {
		return fmt.Errorf("failed to update chocolatey: %w", err)
	}
	return nil
}

func RegisterChocolatey(repository *Repository) {
	repository.Register(func(c exec.ContextRunner) PackageManager {
		if !c.IsWindows() {
			return nil
		}
		if c.ExecContext(context.Background(), "where choco.exe") != nil {
			return nil
		}
		return &Chocolatey{c}
	})
}
