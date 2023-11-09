// Package enterpriselinux provides OS modules for Enterprise Linux based distributions
package enterpriselinux

import (
	"github.com/k0sproject/rig"
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
		func() any {
			return CentOS{}
		},
	)
}
