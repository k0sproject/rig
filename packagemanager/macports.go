package packagemanager

import (
	"context"

	"github.com/k0sproject/rig/v2/cmd"
)

// NewMacports creates a new macports package manager.
func NewMacports(c cmd.ContextRunner) PackageManager {
	return newUniversalPackageManager(c, "macports", "port", "install", "uninstall", "selfupdate")
}

// RegisterMacports registers the macports package manager to a repository.
func RegisterMacports(repository *Provider) {
	repository.Register(func(c cmd.ContextRunner) (PackageManager, bool) {
		if c.IsWindows() {
			return nil, false
		}
		// seems a bit suspiciously vague, maybe we should check port --help and match against that?
		if c.ExecContext(context.Background(), "command -v port") != nil {
			return nil, false
		}
		return NewMacports(c), true
	})
}
