package packagemanager

import (
	"context"

	"github.com/k0sproject/rig/v2/cmd"
)

// NewYum creates a new yum package manager.
func NewYum(c cmd.ContextRunner) PackageManager {
	return newUniversalPackageManager(c, "yum", "yum", "install -y", "remove -y", "makecache")
}

// RegisterYum registers the yum package manager to a repository. It is skipped when dnf is available, as dnf is preferred.
func RegisterYum(repository *Registry) {
	repository.Register(func(runner cmd.ContextRunner) (PackageManager, bool) {
		if runner.IsWindows() {
			return nil, false
		}
		if runner.ExecContext(context.Background(), "command -v yum") != nil {
			return nil, false
		}
		if runner.ExecContext(context.Background(), "command -v dnf") == nil {
			// prefer dnf when available
			return nil, false
		}
		return NewYum(runner), true
	})
}
