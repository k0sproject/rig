package packagemanager

import (
	"context"

	"github.com/k0sproject/rig/exec"
)

// NewApk creates a new apk package manager.
func NewApk(c exec.ContextRunner) PackageManager {
	return newUniversalPackageManager(c, "apk", "apk", "add", "del", "update")
}

// RegisterApk registers the apk package manager to a repository.
func RegisterApk(repository *Provider) {
	repository.Register(func(c exec.ContextRunner) (PackageManager, bool) {
		if c.IsWindows() {
			return nil, false
		}
		if c.ExecContext(context.Background(), "command -v apk") != nil {
			return nil, false
		}
		return NewApk(c), true
	})
}
