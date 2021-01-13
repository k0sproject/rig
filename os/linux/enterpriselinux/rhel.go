package enterpriselinux

import (
	"github.com/k0sproject/rig"
	"github.com/k0sproject/rig/os"
	"github.com/k0sproject/rig/os/linux"
	"github.com/k0sproject/rig/os/registry"
)

type RHEL struct {
	linux.EnterpriseLinux
}

func init() {
	registry.RegisterOSModule(
		func(os *rig.Os) bool {
			return os.ID == "rhel"
		},
		func(h os.Host) interface{} {
			return &RHEL{
				EnterpriseLinux: linux.EnterpriseLinux{
					Linux: os.Linux{
						Host: h,
					},
				},
			}
		},
	)
}
