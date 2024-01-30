package packagemanager

import (
	"context"
	"fmt"

	"github.com/k0sproject/rig/exec"
)

type MacPorts struct {
	exec.ContextRunner
}

func (m *MacPorts) Install(ctx context.Context, packageNames ...string) error {
	if err := m.ExecContext(ctx, buildCommand("port install", packageNames...)); err != nil {
		return fmt.Errorf("failed to install macports packages: %w", err)
	}
	return nil
}

func (m *MacPorts) Remove(ctx context.Context, packageNames ...string) error {
	if err := m.ExecContext(ctx, buildCommand("port uninstall", packageNames...)); err != nil {
		return fmt.Errorf("failed to remove macports packages: %w", err)
	}
	return nil
}

func (m *MacPorts) Update(ctx context.Context) error {
	if err := m.ExecContext(ctx, "port selfupdate"); err != nil {
		return fmt.Errorf("failed to update macports: %w", err)
	}
	return nil
}

func RegisterMacPorts(repository *Repository) {
	repository.Register(func(c exec.ContextRunner) PackageManager {
		if c.IsWindows() {
			return nil
		}
		if c.ExecContext(context.Background(), "command -v port") != nil {
			return nil
		}
		return &MacPorts{c}
	})
}
