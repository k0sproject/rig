package main

/*
	This example shows how the OS support mechanism can be used to add utility
	functions for multiple target operating systems.

	The reason "host.Os" does not come out of the box with rig.Connection is that
	most likely you want to write functions of your own and have a certain set of
	operating systems to support. This way, you are free to have a "host.Os" that
	implements your own interface and you will have type checking and code
	completion in full effect.
*/

import (
	"fmt"

	"github.com/k0sproject/rig"
	"github.com/k0sproject/rig/os"
	"github.com/k0sproject/rig/os/registry"
	_ "github.com/k0sproject/rig/os/support"
)

type configurer interface {
	Pwd(os.Host) string
}

// Host is a host that utilizes rig for connections
type Host struct {
	rig.Connection

	Configurer configurer
}

// LoadOS is a function that assigns a OS support package to the host and
// typecasts it to a suitable interface
func (h *Host) LoadOS() error {
	bf, err := registry.GetOSModuleBuilder(*h.OSVersion)
	if err != nil {
		return err
	}

	h.Configurer = bf().(configurer)

	return nil
}

func main() {
	h := &Host{
		Connection: rig.Connection{
			Localhost: &rig.Localhost{
				Enabled: true,
			},
		},
	}

	if err := h.Connect(); err != nil {
		panic(err)
	}

	if err := h.LoadOS(); err != nil {
		panic(err)
	}

	fmt.Println("OS Info:")
	fmt.Printf("%+v\n", h.OSVersion)
	fmt.Printf("Host PWD:\n%s\n", h.Configurer.Pwd(h))
}
