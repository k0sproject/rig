package packagemanager

import (
	"context"
	"fmt"

	"github.com/k0sproject/rig/exec"
)

type Apk struct {
	exec.ContextRunner
}

func (a *Apk) Install(ctx context.Context, packageNames ...string) error {
	if err := a.ExecContext(ctx, buildCommand("apk add", packageNames...)); err != nil {
		return fmt.Errorf("failed to install apk packages: %w", err)
	}
	return nil
}

func (a *Apk) Remove(ctx context.Context, packageNames ...string) error {
	if err := a.ExecContext(ctx, buildCommand("apk del", packageNames...)); err != nil {
		return fmt.Errorf("failed to remove apk packages: %w", err)
	}
	return nil
}

func (a *Apk) Update(ctx context.Context) error {
	if err := a.ExecContext(ctx, "apk update"); err != nil {
		return fmt.Errorf("failed to update apk: %w", err)
	}
	return nil
}

func RegisterApk(repository *Repository) {
	repository.Register(func(c exec.ContextRunner) PackageManager {
		if c.IsWindows() {
			return nil
		}
		if c.ExecContext(context.Background(), "command -v apk") != nil {
			return nil
		}
		return &Apk{c}
	})
}
