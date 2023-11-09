// Package linux contains configurers for various linux based distributions
package linux

import (
	"fmt"
	"strings"

	"github.com/k0sproject/rig"
	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/os"
	"github.com/k0sproject/rig/os/registry"
)

// Debian provides OS support for Debian systems
type Debian struct {
	os.Linux
}

func init() {
	registry.RegisterOSModule(
		func(os rig.OSVersion) bool {
			return os.ID == "debian"
		},
		func() any {
			return Debian{}
		},
	)
}

// InstallPackage installs packages via apt-get
func (c Debian) InstallPackage(h os.Host, s ...string) error {
	if err := h.Execf("apt-get update", exec.Sudo(h)); err != nil {
		return fmt.Errorf("failed to update apt cache: %w", err)
	}
	if err := h.Execf("DEBIAN_FRONTEND=noninteractive apt-get install -y -q %s", strings.Join(s, " "), exec.Sudo(h)); err != nil {
		return fmt.Errorf("failed to install packages: %w", err)
	}
	return nil
}
