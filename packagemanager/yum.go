package packagemanager

import (
	"context"
	"fmt"

	"github.com/k0sproject/rig/exec"
)

type Yum struct {
	exec.ContextRunner
}

func (y *Yum) Install(ctx context.Context, packageNames ...string) error {
	if err := y.ExecContext(ctx, buildCommand("yum install -y", packageNames...)); err != nil {
		return fmt.Errorf("failed to install yum packages: %w", err)
	}
	return nil
}

func (y *Yum) Remove(ctx context.Context, packageNames ...string) error {
	if err := y.ExecContext(ctx, buildCommand("yum remove -y", packageNames...)); err != nil {
		return fmt.Errorf("failed to remove yum packages: %w", err)
	}
	return nil
}

func (y *Yum) Update(ctx context.Context) error {
	if err := y.ExecContext(ctx, "yum makecache"); err != nil {
		return fmt.Errorf("failed to update yum: %w", err)
	}
	return nil
}

func RegisterYum(repository *Repository) {
	repository.Register(func(c exec.ContextRunner) PackageManager {
		if c.IsWindows() {
			return nil
		}
		if c.ExecContext(context.Background(), "command -v yum") != nil {
			return nil
		}
		return &Yum{c}
	})
}
