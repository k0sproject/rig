package packagemanager

import (
	"context"

	"github.com/k0sproject/rig/v2/cmd"
)

// NewDnf creates a new dnf package manager.
func NewDnf(c cmd.ContextRunner) PackageManager {
	return newUniversalPackageManager(c, "dnf", "dnf", "install -y", "remove -y", "makecache")
}

// RegisterDnf registers the dnf package manager to a repository.
func RegisterDnf(repository *Provider) {
	repository.Register(func(c cmd.ContextRunner) (PackageManager, bool) {
		if c.IsWindows() {
			return nil, false
		}
		if c.ExecContext(context.Background(), "command -v dnf") != nil {
			return nil, false
		}
		return NewDnf(c), true
	})
}
