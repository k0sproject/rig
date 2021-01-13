package enterpriselinux

import (
	"github.com/k0sproject/rig"
	"github.com/k0sproject/rig/os"
	"github.com/k0sproject/rig/os/linux"
	"github.com/k0sproject/rig/os/registry"
)

// CentOS provides OS support for CentOS
type CentOS struct {
	linux.EnterpriseLinux
}

func init() {
	registry.RegisterOSModule(
		func(os rig.OSVersion) bool {
			return os.ID == "centos"
		},
		func(h os.Host) interface{} {
			return &CentOS{
				EnterpriseLinux: linux.EnterpriseLinux{
					Linux: os.Linux{
						Host: h,
					},
				},
			}
		},
	)
}
