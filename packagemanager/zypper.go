package packagemanager

import (
	"context"

	"github.com/k0sproject/rig/exec"
)

// NewZypper creates a new zypper package manager.
func NewZypper(c exec.ContextRunner) PackageManager {
	return newUniversalPackageManager(c, "zypper", "zypper", "install -y", "remove -y", "refresh")
}

// RegisterZypper registers the zypper package manager to a repository.
func RegisterZypper(repository *Provider) {
	repository.Register(func(c exec.ContextRunner) PackageManager {
		if c.IsWindows() {
			return nil
		}
		if c.ExecContext(context.Background(), "command -v zypper") != nil {
			return nil
		}
		return NewZypper(c)
	})
}
