package packagemanager

import (
	"context"

	"github.com/k0sproject/rig/cmd"
)

// NewApt creates a new apt package manager.
func NewApt(c cmd.ContextRunner) PackageManager {
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
func RegisterApt(repository *Provider) {
	repository.Register(func(c cmd.ContextRunner) (PackageManager, bool) {
		if c.IsWindows() {
			return nil, false
		}
		if c.ExecContext(context.Background(), "command -v apt-get") != nil {
			return nil, false
		}
		return NewApt(c), true
	})
}
