package darwin

import (
	"strings"

	"github.com/k0sproject/rig"
	"github.com/k0sproject/rig/os"
	"github.com/k0sproject/rig/os/registry"
)

type Darwin struct {
	os.Linux
}

func (c *Darwin) InstallPackage(s ...string) error {
	return c.Host.Execf("brew install %s", strings.Join(s, " "))
}

func init() {
	registry.RegisterOSModule(
		func(os *rig.Os) bool {
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
