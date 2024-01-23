package enterpriselinux

import (
	"github.com/k0sproject/rig"
	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/os"
	"github.com/k0sproject/rig/os/linux"
	"github.com/k0sproject/rig/os/registry"
)

// OracleLinux provides OS support for Oracle Linuc
type OracleLinux struct {
	linux.EnterpriseLinux
}

func init() {
	registry.RegisterOSModule(
		func(os rig.OSVersion) bool {
			return os.ID == "ol"
		},
		func(runner exec.SimpleRunner) any {
			return &OracleLinux{EnterpriseLinux: linux.EnterpriseLinux{Linux: os.Linux{SimpleRunner: runner}}}
		},
	)
}
