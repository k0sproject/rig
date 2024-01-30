package packagemanager

import (
	"context"
	"fmt"

	"github.com/k0sproject/rig/exec"
)

type Homebrew struct {
	exec.ContextRunner
}

func (h *Homebrew) Install(ctx context.Context, packageNames ...string) error {
	if err := h.ExecContext(ctx, buildCommand("brew install", packageNames...)); err != nil {
		return fmt.Errorf("failed to install homebrew packages: %w", err)
	}
	return nil
}

func (h *Homebrew) Remove(ctx context.Context, packageNames ...string) error {
	if err := h.ExecContext(ctx, buildCommand("brew uninstall", packageNames...)); err != nil {
		return fmt.Errorf("failed to remove homebrew packages: %w", err)
	}
	return nil
}

func (h *Homebrew) Update(ctx context.Context) error {
	if err := h.ExecContext(ctx, "brew update"); err != nil {
		return fmt.Errorf("failed to update homebrew: %w", err)
	}
	return nil
}

func RegisterHomebrew(repository *Repository) {
	repository.Register(func(c exec.ContextRunner) PackageManager {
		if c.IsWindows() {
			return nil
		}
		if c.ExecContext(context.Background(), "command -v brew") != nil {
			return nil
		}
		return &Homebrew{c}
	})
}
