// Package darwin provides a configurer for macOS
package darwin

import (
	"errors"

	"github.com/k0sproject/rig"
	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/os"
	"github.com/k0sproject/rig/os/registry"
)

// ErrNotImplemented is returned when a method is not implemented
var ErrNotImplemented = errors.New("not implemented")

// Darwin provides OS support for macOS Darwin
type Darwin struct {
	os.Linux
}

func init() {
	registry.RegisterOSModule(
		func(os rig.OSVersion) bool {
			return os.ID == "darwin"
		},
		func(runner exec.SimpleRunner) any {
			return &Darwin{Linux: os.Linux{SimpleRunner: runner}}
		},
	)
}
