package linux

import (
	"github.com/k0sproject/rig"
	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/os"
	"github.com/k0sproject/rig/os/registry"
)

// Alpine provides OS support for Alpine Linux.
type Alpine struct {
	os.Linux
}

func init() {
	registry.RegisterOSModule(
		func(os rig.OSVersion) bool {
			return os.ID == "alpine"
		},
		func(runner exec.SimpleRunner) any {
			return &Alpine{Linux: os.Linux{SimpleRunner: runner}}
		},
	)
}
