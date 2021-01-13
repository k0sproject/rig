package windows

import (
	"github.com/k0sproject/rig"
	"github.com/k0sproject/rig/os"
	"github.com/k0sproject/rig/os/registry"
)

// Windows2019 provides os specific functions for the "Windows Server 2019" OS
type Windows2019 struct {
	os.Windows
}

func init() {
	registry.RegisterOSModule(
		func(os *rig.Os) bool {
			return os.ID == "windows-10.0.17763"
		},
		func(h os.Host) interface{} {
			return &Windows2019{
				Windows: os.Windows{
					Host: h,
				},
			}
		},
	)
}
