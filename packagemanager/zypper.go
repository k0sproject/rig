package packagemanager

import (
	"context"

	"github.com/k0sproject/rig/cmd"
)

// NewZypper creates a new zypper package manager.
func NewZypper(c cmd.ContextRunner) PackageManager {
	return newUniversalPackageManager(c, "zypper", "zypper", "install -y", "remove -y", "refresh")
}

// RegisterZypper registers the zypper package manager to a repository.
func RegisterZypper(repository *Provider) {
	repository.Register(func(c cmd.ContextRunner) (PackageManager, bool) {
		if c.IsWindows() {
			return nil, false
		}
		if c.ExecContext(context.Background(), "command -v zypper") != nil {
			return nil, false
		}
		return NewZypper(c), true
	})
}
