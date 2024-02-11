package packagemanager

import (
	"context"

	"github.com/k0sproject/rig/exec"
)

// NewScoop creates a new scoop package manager.
func NewScoop(c exec.ContextRunner) PackageManager {
	return newUniversalPackageManager(c, "scoop", "scoop", "install", "uninstall", "update *")
}

// RegisterScoop registers the apk package manager to a repository.
func RegisterScoop(repository *Provider) {
	repository.Register(func(c exec.ContextRunner) PackageManager {
		if !c.IsWindows() {
			return nil
		}
		if c.ExecContext(context.Background(), "where scoop.exe") != nil {
			return nil
		}
		return NewScoop(c)
	})
}
