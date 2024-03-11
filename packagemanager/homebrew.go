package packagemanager

import (
	"context"

	"github.com/k0sproject/rig/v2/cmd"
)

// NewHomebrew creates a new homebrew package manager.
func NewHomebrew(c cmd.ContextRunner) PackageManager {
	return newUniversalPackageManager(c, "homebrew", "brew", "install", "uninstall", "update")
}

// RegisterHomebrew registers the homebrew package manager to a repository.
func RegisterHomebrew(repository *Provider) {
	repository.Register(func(c cmd.ContextRunner) (PackageManager, bool) {
		if c.IsWindows() {
			return nil, false
		}
		if c.ExecContext(context.Background(), "command -v brew") != nil {
			return nil, false
		}
		return NewHomebrew(c), true
	})
}
