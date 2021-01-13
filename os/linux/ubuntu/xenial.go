package ubuntu

import (
	"github.com/k0sproject/rig"
	"github.com/k0sproject/rig/os"
	"github.com/k0sproject/rig/os/linux"
	"github.com/k0sproject/rig/os/registry"
)

type Xenial struct {
	linux.Ubuntu
}

func init() {
	registry.RegisterOSModule(
		func(os *rig.Os) bool {
			return os.ID == "ubuntu" && os.Version == "16.04"
		},
		func(h os.Host) interface{} {
			return &Xenial{
				Ubuntu: linux.Ubuntu{
					Linux: os.Linux{
						Host: h,
					},
				},
			}
		},
	)
}
