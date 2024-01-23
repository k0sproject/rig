package linux

import (
	"fmt"
	"strings"

	"github.com/alessio/shellescape"
	"github.com/k0sproject/rig/os"
)

// EnterpriseLinux is a base package for several RHEL-like enterprise linux distributions
type EnterpriseLinux struct {
	os.Linux
}

// InstallPackage installs packages via yum
func (c EnterpriseLinux) InstallPackage(s ...string) error {
	cmd := strings.Builder{}
	cmd.WriteString("yum install -y")
	for _, pkg := range s {
		cmd.WriteRune(' ')
		cmd.WriteString(shellescape.Quote(pkg))
	}

	if err := c.Exec(cmd.String()); err != nil {
		return fmt.Errorf("failed to install packages: %w", err)
	}

	return nil
}
