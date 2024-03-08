package packagemanager

import (
	"context"

	"github.com/k0sproject/rig/v2/cmd"
)

// NewWinget creates a new winget package manager.
func NewWinget(c cmd.ContextRunner) PackageManager {
	return newUniversalPackageManager(c, "winget", "winget.exe", "install", "uninstall", "upgrade --all")
}

// RegisterWinget registers the winget (preinstalled on win10+) package manager to a repository.
func RegisterWinget(repository *Provider) {
	repository.Register(func(c cmd.ContextRunner) (PackageManager, bool) {
		if !c.IsWindows() {
			return nil, false
		}
		if c.ExecContext(context.Background(), "where.exe winget.exe") != nil {
			return nil, false
		}
		return NewWinget(c), true
	})
}
