package packagemanager

import (
	"context"
	"fmt"

	"github.com/k0sproject/rig/exec"
)

type Pacman struct {
	exec.ContextRunner
}

func (p *Pacman) Install(ctx context.Context, packageNames ...string) error {
	if err := p.ExecContext(ctx, buildCommand("pacman -S --noconfirm", packageNames...)); err != nil {
		return fmt.Errorf("failed to install pacman packages: %w", err)
	}
	return nil
}

func (p *Pacman) Remove(ctx context.Context, packageNames ...string) error {
	if err := p.ExecContext(ctx, buildCommand("pacman -R --noconfirm", packageNames...)); err != nil {
		return fmt.Errorf("failed to remove pacman packages: %w", err)
	}
	return nil
}

func (p *Pacman) Update(ctx context.Context) error {
	if err := p.ExecContext(ctx, "pacman -Syu --noconfirm"); err != nil {
		return fmt.Errorf("failed to update pacman: %w", err)
	}
	return nil
}

func RegisterPacman(repository *Repository) {
	repository.Register(func(c exec.ContextRunner) PackageManager {
		if c.IsWindows() {
			return nil
		}
		if c.ExecContext(context.Background(), "command -v pacman") != nil {
			return nil
		}
		return &Pacman{c}
	})
}
