package linux

import (
	"strings"

	"github.com/k0sproject/rig"
	"github.com/k0sproject/rig/os"
	"github.com/k0sproject/rig/os/registry"
)

// SLES provides OS support for Suse SUSE Linux Enterprise Server
type SLES struct {
	os.Linux
}

// InstallPackage installs packages via zypper
func (c *SLES) InstallPackage(s ...string) error {
	return c.Host.Execf("sudo zypper refresh && sudo zypper -n install -y %s", strings.Join(s, " "))
}

func init() {
	registry.RegisterOSModule(
		func(os *rig.Os) bool {
			return os.ID == "sles"
		},
		func(h os.Host) interface{} {
			return &SLES{
				Linux: os.Linux{
					Host: h,
				},
			}
		},
	)
}
