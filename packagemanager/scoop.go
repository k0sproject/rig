package packagemanager

import (
	"context"

	"github.com/k0sproject/rig/exec"
)

// NewScoop creates a new scoop package manager.
func NewScoop(c exec.ContextRunner) PackageManager {
	return newUniversalPackageManager(c, "scoop", "scoop.exe", "install", "uninstall", "update *")
}

// RegisterScoop registers the scoop package manager to a repository.
func RegisterScoop(repository *Provider) {
	repository.Register(func(c exec.ContextRunner) (PackageManager, bool) {
		if !c.IsWindows() {
			return nil, false
		}
		if c.ExecContext(context.Background(), "where.exe scoop.exe") != nil {
			return nil, false
		}
		return NewScoop(c), true
	})
}
