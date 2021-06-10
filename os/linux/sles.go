package linux

import (
	"strings"

	"github.com/k0sproject/rig"
	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/os"
	"github.com/k0sproject/rig/os/registry"
)

// SLES provides OS support for Suse SUSE Linux Enterprise Server
type SLES struct {
	os.Linux
}

// InstallPackage installs packages via zypper
func (c SLES) InstallPackage(h os.Host, s ...string) error {
	return h.Execf("zypper refresh && sudo zypper -n install -y %s", strings.Join(s, " "), exec.Sudo())
}

func init() {
	registry.RegisterOSModule(
		func(os rig.OSVersion) bool {
			return os.ID == "sles"
		},
		func() interface{} {
			return SLES{}
		},
	)
}
