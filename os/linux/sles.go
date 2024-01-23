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

// SLES provides OS support for Suse SUSE Linux Enterprise Server
type SLES struct {
	os.Linux
}

// InstallPackage installs packages via zypper
func (c SLES) InstallPackage(s ...string) error {
	if err := c.Exec("zypper refresh"); err != nil {
		return fmt.Errorf("failed to refresh zypper: %w", err)
	}
	cmd := strings.Builder{}
	cmd.WriteString("zypper -n install -y")
	for _, pkg := range s {
		cmd.WriteRune(' ')
		cmd.WriteString(shellescape.Quote(pkg))
	}
	if err := c.Exec(cmd.String()); err != nil {
		return fmt.Errorf("failed to install packages: %w", err)
	}
	return nil
}

func init() {
	registry.RegisterOSModule(
		func(os rig.OSVersion) bool {
			return os.ID == "sles"
		},
		func(runner exec.SimpleRunner) any {
			return &SLES{Linux: os.Linux{SimpleRunner: runner}}
		},
	)
}
