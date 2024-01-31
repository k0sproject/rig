package packagemanager

import (
	"context"

	"github.com/k0sproject/rig/exec"
)

// NewApt creates a new apt package manager.
func NewApt(c exec.ContextRunner) PackageManager {
	return newUniversalPackageManager(
		c,
		"apt",
		"DEBIAN_FRONTEND=noninteractive APT_LISTCHANGES_FRONTEND=none apt-get",
		"install -y",
		"remove -y",
		"update",
	)
}

// RegisterApt registers the apt package manager to a repository.
func RegisterApt(repository *Repository) {
	repository.Register(func(c exec.ContextRunner) PackageManager {
		if c.IsWindows() {
			return nil
		}
		if c.ExecContext(context.Background(), "command -v apk") != nil {
			return nil
		}
		return NewApt(c)
	})
}
