package linux

import (
	"strings"

	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/os"
)

// EnterpriseLinux is a base package for several RHEL-like enterprise linux distributions
type EnterpriseLinux struct {
	os.Linux
}

// InstallPackage installs packages via yum
func (c EnterpriseLinux) InstallPackage(h os.Host, s ...string) error {
	if err := h.Execf("yum install -y %s", strings.Join(s, " "), exec.Sudo(h)); err != nil {
		return exec.ErrRemote.Wrapf("failed to install packages: %w", err)
	}

	return nil
}
