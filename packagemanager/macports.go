package packagemanager

import (
	"context"

	"github.com/k0sproject/rig/exec"
)

// NewMacports creates a new macports package manager.
func NewMacports(c exec.ContextRunner) PackageManager {
	return newUniversalPackageManager(c, "macports", "port", "install", "uninstall", "selfupdate")
}

// RegisterMacports registers the macports package manager to a repository.
func RegisterMacports(repository *Provider) {
	repository.Register(func(c exec.ContextRunner) PackageManager {
		if c.IsWindows() {
			return nil
		}
		// seems a bit suspiciously vague, maybe we should check port --help and match against that?
		if c.ExecContext(context.Background(), "command -v port") != nil {
			return nil
		}
		return NewMacports(c)
	})
}
