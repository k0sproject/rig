package enterpriselinux

import (
	"github.com/k0sproject/rig"
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
		func(os *rig.OSVersion) bool {
			return os.ID == "ol"
		},
		func(h os.Host) interface{} {
			return &OracleLinux{
				EnterpriseLinux: linux.EnterpriseLinux{
					Linux: os.Linux{
						Host: h,
					},
				},
			}
		},
	)
}
