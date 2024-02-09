package packagemanager

import (
	"context"

	"github.com/k0sproject/rig/exec"
)

// NewWinget creates a new winget package manager.
func NewWinget(c exec.ContextRunner) PackageManager {
	return newUniversalPackageManager(c, "winget", "winget", "install", "uninstall", "upgrade --all")
}

// RegisterWinget registers the winget (preinstalled on win10+) package manager to a repository.
func RegisterWinget(repository *Repository) {
	repository.Register(func(c exec.ContextRunner) PackageManager {
		if !c.IsWindows() {
			return nil
		}
		if c.ExecContext(context.Background(), "where.exe winget") != nil {
			return nil
		}
		return NewWinget(c)
	})
}
