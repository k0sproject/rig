package linux

import (
	"strings"

	"github.com/k0sproject/rig/os"
)

// EnterpriseLinux is a base package for several RHEL-like enterprise linux distributions
type EnterpriseLinux struct {
	os.Linux
}

// InstallPackage installs packages via yum
func (c EnterpriseLinux) InstallPackage(h os.Host, s ...string) error {
	return h.Execf("sudo yum install -y %s", strings.Join(s, " "))
}
