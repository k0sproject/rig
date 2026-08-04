package packagemanager

import (
	"context"

	"github.com/k0sproject/rig/v2/cmd"
)

// NewZypper creates a new zypper package manager.
//
// The install action passes --allow-vendor-change so that a package which
// obsoletes/replaces one provided by a different vendor (e.g. installing
// Mirantis/Docker containerd.io on a SLES cloud image that ships SUSE's own
// containerd) resolves non-interactively instead of silently cancelling with
// exit code 4. NOTE: this is deliberately aggressive about package adoption --
// it lets zypper switch a package's vendor/origin during a normal install,
// which some operators may consider undesirable as a global default. See the
// PR discussion for whether this should instead be gated behind an opt-in
// install option.
func NewZypper(c cmd.ContextRunner) PackageManager {
	return newUniversalPackageManager(c, "zypper", "zypper", "install -y --allow-vendor-change", "remove -y", "refresh")
}

// RegisterZypper registers the zypper package manager to a repository.
func RegisterZypper(repository *Registry) {
	repository.Register(func(c cmd.ContextRunner) (PackageManager, bool) {
		if c.IsWindows() {
			return nil, false
		}
		if c.ExecContext(context.Background(), "command -v zypper") != nil {
			return nil, false
		}
		return NewZypper(c), true
	})
}
