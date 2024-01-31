package packagemanager

import (
	"context"

	"github.com/k0sproject/rig/exec"
)

// NewDnf creates a new dnf package manager.
func NewDnf(c exec.ContextRunner) PackageManager {
	return newUniversalPackageManager(c, "dnf", "dnf", "install -y", "remove -y", "makecache")
}

// RegisterDnf registers the dnf package manager to a repository.
func RegisterDnf(repository *Repository) {
	repository.Register(func(c exec.ContextRunner) PackageManager {
		if c.IsWindows() {
			return nil
		}
		if c.ExecContext(context.Background(), "command -v dnf") != nil {
			return nil
		}
		return NewDnf(c)
	})
}
