package linux

import (
	"github.com/k0sproject/rig"
	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/os"
	"github.com/k0sproject/rig/os/registry"
)

// SLES provides OS support for Suse SUSE Linux Enterprise Server
type SLES struct {
	os.Linux
}

func init() {
	registry.RegisterOSModule(
		func(os rig.OSVersion) bool {
			return os.ID == "sles"
		},
		func(runner exec.SimpleRunner) any {
			return &SLES{Linux: os.Linux{SimpleRunner: runner}}
		},
	)
}
