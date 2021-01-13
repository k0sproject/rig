package linux

import (
	"strings"

	"github.com/k0sproject/rig"
	"github.com/k0sproject/rig/os"
	"github.com/k0sproject/rig/os/registry"
)

// Ubuntu provides OS support for Ubuntu systems
type Ubuntu struct {
	os.Linux
}

func init() {
	registry.RegisterOSModule(
		func(os rig.OSVersion) bool {
			return os.ID == "ubuntu"
		},
		func(h os.Host) interface{} {
			return &Ubuntu{
				Linux: os.Linux{
					Host: h,
				},
			}
		},
	)
}

// InstallPackage installs packages via apt-get
func (c *Ubuntu) InstallPackage(s ...string) error {
	return c.Host.Execf("sudo apt-get update && sudo DEBIAN_FRONTEND=noninteractive apt-get install -y -q %s", strings.Join(s, " "))
}
