package packagemanager

import (
	"context"

	"github.com/k0sproject/rig/exec"
)

// NewYum creates a new yum package manager.
func NewYum(c exec.ContextRunner) PackageManager {
	return newUniversalPackageManager(c, "yum", "yum", "install -y", "remove -y", "makecache")
}

// RegisterYum registers the dnf package manager to a repository.
func RegisterYum(repository *Provider) {
	repository.Register(func(runner exec.ContextRunner) (PackageManager, bool) {
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
