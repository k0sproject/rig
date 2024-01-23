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

// Alpine provides OS support for Alpine Linux.
type Alpine struct {
	os.Linux
}

func init() {
	registry.RegisterOSModule(
		func(os rig.OSVersion) bool {
			return os.ID == "alpine"
		},
		func(runner exec.SimpleRunner) any {
			return &Alpine{Linux: os.Linux{SimpleRunner: runner}}
		},
	)
}

// InstallPackage installs packages via apk.
func (l Alpine) InstallPackage(pkgs ...string) error {
	if err := l.Exec("apk update"); err != nil {
		return fmt.Errorf("failed to update apk cache: %w", err)
	}

	if len(pkgs) < 1 {
		return nil
	}

	var cmd strings.Builder
	cmd.WriteString("apk add --")
	for _, pkg := range pkgs {
		cmd.WriteRune(' ')
		cmd.WriteString(shellescape.Quote(pkg))
	}

	if err := l.Exec(cmd.String()); err != nil {
		return fmt.Errorf("failed to install apk packages: %w", err)
	}

	return nil
}
