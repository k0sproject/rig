// Package windows provides OS support for Windows.
package windows

import (
	"strings"

	"github.com/k0sproject/rig"
	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/os"
	"github.com/k0sproject/rig/os/registry"
)

// Windows2019 provides os specific functions for the "Windows Server 2019" OS
type Windows2019 struct {
	os.Windows
}

func init() {
	registry.RegisterOSModule(
		func(os rig.OSVersion) bool {
			return os.ID == "windows" && strings.HasPrefix(os.Version, "10.0.")
		},
		func(runner exec.SimpleRunner) any {
			return Windows2019{Windows: os.Windows{SimpleRunner: runner}}
		},
	)
}
