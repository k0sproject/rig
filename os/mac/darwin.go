package darwin

import (
	"strings"

	"github.com/k0sproject/rig"
	"github.com/k0sproject/rig/os"
	"github.com/k0sproject/rig/os/initsystem"
	"github.com/k0sproject/rig/os/registry"
)

// Darwin provides OS support for macOS Darwin
type Darwin struct {
	os.Linux

	initSystem os.InitSystem
}

// Kind returns "darwin"
func (c *Darwin) Kind() string {
	return "darwin"
}

// InitSystem is an accessor to Darwin init system (start service, stop service, ...)
func (c *Darwin) InitSystem() os.InitSystem {
	if c.initSystem == nil {
		c.initSystem = &initsystem.Darwin{Host: c.Host}
	}
	return c.initSystem
}

// InstallPackage installs a package using brew
func (c *Darwin) InstallPackage(s ...string) error {
	return c.Host.Execf("brew install %s", strings.Join(s, " "))
}

func init() {
	registry.RegisterOSModule(
		func(os *rig.OSVersion) bool {
			return os.ID == "darwin"
		},
		func(h os.Host) interface{} {
			return &Darwin{
				Linux: os.Linux{
					Host: h,
				},
			}
		},
	)
}
