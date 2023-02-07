// Package linux contains configurers for various linux based distributions
package linux

import (
	"github.com/k0sproject/rig"
	"github.com/k0sproject/rig/os/registry"
)

// Ubuntu provides OS support for Ubuntu systems
type Ubuntu struct {
	Debian
}

func init() {
	registry.RegisterOSModule(
		func(os rig.OSVersion) bool {
			return os.ID == "ubuntu"
		},
		func() interface{} {
			return Ubuntu{}
		},
	)
}
