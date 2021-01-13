package main

import (
	"fmt"

	"github.com/k0sproject/rig"
	"github.com/k0sproject/rig/client/local"
	"github.com/k0sproject/rig/os/registry"
	_ "github.com/k0sproject/rig/os/support"
)

type os interface {
	Pwd() string
}

type Host struct {
	rig.Connection

	Os os
}

func (h *Host) LoadOS() error {
	bf, err := registry.GetOSModuleBuilder(h.OsInfo)
	if err != nil {
		return err
	}

	h.Os = bf(h).(os)

	return nil
}

func main() {
	h := Host{
		Connection: rig.Connection{
			Localhost: &local.Client{
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

	fmt.Printf("%s: host pwd:%s\n", h.String(), h.Os.Pwd())
}
