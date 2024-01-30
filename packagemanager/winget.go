package packagemanager

import (
	"context"
	"fmt"

	"github.com/k0sproject/rig/exec"
)

type Winget struct {
	exec.ContextRunner
}

func (w *Winget) Install(ctx context.Context, packageNames ...string) error {
	if err := w.ExecContext(ctx, buildCommand("winget install", packageNames...)); err != nil {
		return fmt.Errorf("failed to install winget packages: %w", err)
	}
	return nil
}

func (w *Winget) Remove(ctx context.Context, packageNames ...string) error {
	if err := w.ExecContext(ctx, buildCommand("winget uninstall", packageNames...)); err != nil {
		return fmt.Errorf("failed to remove winget packages: %w", err)
	}
	return nil
}

func (w *Winget) Update(ctx context.Context) error {
	if err := w.ExecContext(ctx, "winget upgrade --all"); err != nil {
		return fmt.Errorf("failed to update winget: %w", err)
	}
	return nil
}

func RegisterWinget(repository *Repository) {
	repository.Register(func(c exec.ContextRunner) PackageManager {
		if c.IsWindows() {
			return &Winget{c}
		}
		if c.ExecContext(context.Background(), "where winget") != nil {
			return nil
		}
		return nil
	})
}
