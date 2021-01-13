package linux

import (
	"strings"

	"github.com/k0sproject/rig/os"
)

type Ubuntu struct {
	os.Linux
}

func (c *Ubuntu) InstallPackage(s ...string) error {
	return c.Host.Execf("sudo apt-get update && sudo DEBIAN_FRONTEND=noninteractive apt-get install -y -q %s", strings.Join(s, " "))
}
