package linux

import (
	"fmt"
	"strings"

	"github.com/k0sproject/rig"
	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/os"
	"github.com/k0sproject/rig/os/registry"
)

// Archlinux provides OS support for Archlinux systems
type Archlinux struct {
	os.Linux
}

func init() {
	registry.RegisterOSModule(
		func(os rig.OSVersion) bool {
			return os.IDLike == "arch"
		},
		func() interface{} {
			return Archlinux{}
		},
	)
}

// InstallPackage installs packages via pacman
func (c Archlinux) InstallPackage(h os.Host, s ...string) error {
	if err := h.Execf("pacman -S --noconfirm --noprogressbar %s", strings.Join(s, " "), exec.Sudo(h)); err != nil {
		return fmt.Errorf("failed to install packages: %w", err)
	}

	return nil
}
