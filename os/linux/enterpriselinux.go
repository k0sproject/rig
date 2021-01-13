package linux

import (
	"strings"

	"github.com/k0sproject/rig/os"
)

type EnterpriseLinux struct {
	os.Linux
}

func (c *EnterpriseLinux) InstallPackage(s ...string) error {
	return c.Host.Execf("sudo yum install -y %s", strings.Join(s, " "))
}
