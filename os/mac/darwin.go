// Package darwin provides a configurer for macOS
package darwin

import (
	"errors"
	"fmt"
	"strings"

	"github.com/alessio/shellescape"
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

// InstallPackage installs a package using brew
func (c Darwin) InstallPackage(s ...string) error {
	cmd := strings.Builder{}
	cmd.WriteString("brew install")
	for _, pkg := range s {
		cmd.WriteRune(' ')
		cmd.WriteString(shellescape.Quote(pkg))
	}

	if err := c.Exec(cmd.String()); err != nil {
		return fmt.Errorf("failed to install packages %s: %w", s, err)
	}
	return nil
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
