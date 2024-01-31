package packagemanager

import (
	"context"

	"github.com/k0sproject/rig/exec"
)

// NewChocolatey creates a new chocolatey package manager.
func NewChocolatey(c exec.ContextRunner) PackageManager {
	return newUniversalPackageManager(c, "chocolatey", "choco", "install -y", "uninstall -y", "upgrade all -y")
}

// RegisterChocolatey registers the chocolatey package manager to a repository.
func RegisterChocolatey(repository *Repository) {
	repository.Register(func(c exec.ContextRunner) PackageManager {
		if !c.IsWindows() {
			return nil
		}
		if c.ExecContext(context.Background(), "where choco.exe") != nil {
			return nil
		}
		return NewChocolatey(c)
	})
}
