package packagemanager

import (
	"context"

	"github.com/k0sproject/rig/v2/cmd"
)

// NewChocolatey creates a new chocolatey package manager.
func NewChocolatey(c cmd.ContextRunner) PackageManager {
	return newUniversalPackageManager(c, "chocolatey", "choco.exe", "install -y", "uninstall -y", "upgrade all -y")
}

// RegisterChocolatey registers the chocolatey package manager to a repository.
func RegisterChocolatey(repository *Provider) {
	repository.Register(func(c cmd.ContextRunner) (PackageManager, bool) {
		if !c.IsWindows() {
			return nil, false
		}
		if c.ExecContext(context.Background(), "where.exe choco.exe") != nil {
			return nil, false
		}
		return NewChocolatey(c), true
	})
}
