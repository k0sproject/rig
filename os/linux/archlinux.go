package linux

import (
	"fmt"
	"strings"

	"github.com/alessio/shellescape"
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
		func(runner exec.SimpleRunner) any {
			return &Archlinux{Linux: os.Linux{SimpleRunner: runner}}
		},
	)
}

// InstallPackage installs packages via pacman
func (c Archlinux) InstallPackage(s ...string) error {
	cmd := strings.Builder{}
	cmd.WriteString("pacman -S --noconfirm --noprogressbar")
	for _, pkg := range s {
		cmd.WriteRune(' ')
		cmd.WriteString(shellescape.Quote(pkg))
	}

	if err := c.Exec(cmd.String()); err != nil {
		return fmt.Errorf("failed to install packages: %w", err)
	}

	return nil
}
