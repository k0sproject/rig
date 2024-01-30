// Package linux contains configurers for various linux based distributions
package linux

import (
	"github.com/k0sproject/rig"
	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/os"
	"github.com/k0sproject/rig/os/registry"
)

// Debian provides OS support for Debian systems
type Debian struct {
	os.Linux
}

func init() {
	registry.RegisterOSModule(
		func(os rig.OSVersion) bool {
			return os.ID == "debian"
		},
		func(runner exec.SimpleRunner) any {
			return &Debian{Linux: os.Linux{SimpleRunner: runner}}
		},
	)
}
