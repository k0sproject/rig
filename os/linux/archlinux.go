package linux

import (
	"github.com/k0sproject/rig"
	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/os"
	"github.com/k0sproject/rig/os/registry"
)

// Archlinux provides OS support for Archlinux systems
type Archlinux struct {
	os.Linux
}

func init() {
	registry.RegisterOSModule(
		func(os rig.OSVersion) bool {
			return os.IDLike == "arch"
		},
		func(runner exec.SimpleRunner) any {
			return &Archlinux{Linux: os.Linux{SimpleRunner: runner}}
		},
	)
}
