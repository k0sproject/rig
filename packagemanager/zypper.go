package packagemanager

import (
	"context"

	"github.com/k0sproject/rig/v2/cmd"
)

// NewZypper creates a new zypper package manager.
//
// The install action passes --allow-vendor-change so that a package which
// replaces one provided by a different vendor resolves non-interactively
// instead of cancelling with exit code 4.
func NewZypper(c cmd.ContextRunner) PackageManager {
	return newUniversalPackageManager(c, "zypper", "zypper", "install -y --allow-vendor-change", "remove -y", "refresh")
}

// RegisterZypper registers the zypper package manager to a repository.
func RegisterZypper(repository *Registry) {
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
