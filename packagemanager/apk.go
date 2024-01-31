package packagemanager

import (
	"context"
	"fmt"

	"github.com/k0sproject/rig/exec"
)

// Apk is the package manager for Alpine Linux.
type Apk struct {
	exec.ContextRunner
}

// Install given packages.
func (a *Apk) Install(ctx context.Context, packageNames ...string) error {
	if err := a.ExecContext(ctx, buildCommand("apk add", packageNames...)); err != nil {
		return fmt.Errorf("failed to install apk packages: %w", err)
	}
	return nil
}

// Remove given packages.
func (a *Apk) Remove(ctx context.Context, packageNames ...string) error {
	if err := a.ExecContext(ctx, buildCommand("apk del", packageNames...)); err != nil {
		return fmt.Errorf("failed to remove apk packages: %w", err)
	}
	return nil
}

// Update the package list.
func (a *Apk) Update(ctx context.Context) error {
	if err := a.ExecContext(ctx, "apk update"); err != nil {
		return fmt.Errorf("failed to update apk: %w", err)
	}
	return nil
}

// RegisterApk registers the apk package manager to a repository.
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
