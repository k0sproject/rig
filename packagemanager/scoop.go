package packagemanager

import (
	"context"
	"fmt"

	"github.com/k0sproject/rig/exec"
)

type Scoop struct {
	exec.ContextRunner
}

func (s *Scoop) Install(ctx context.Context, packageNames ...string) error {
	if err := s.ExecContext(ctx, buildCommand("scoop install", packageNames...)); err != nil {
		return fmt.Errorf("failed to install scoop packages: %w", err)
	}
	return nil
}

func (s *Scoop) Remove(ctx context.Context, packageNames ...string) error {
	if err := s.ExecContext(ctx, buildCommand("scoop uninstall", packageNames...)); err != nil {
		return fmt.Errorf("failed to remove scoop packages: %w", err)
	}
	return nil
}

func (s *Scoop) Update(ctx context.Context) error {
	if err := s.ExecContext(ctx, "scoop update *"); err != nil {
		return fmt.Errorf("failed to update scoop: %w", err)
	}
	return nil
}

func RegisterScoop(repository *Repository) {
	repository.Register(func(c exec.ContextRunner) PackageManager {
		if !c.IsWindows() {
			return nil
		}
		if c.ExecContext(context.Background(), "where scoop.exe") != nil {
			return nil
		}
		return nil
	})
}
