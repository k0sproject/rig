package linux

import (
	"fmt"
	"strings"

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
func (c SLES) InstallPackage(h os.Host, s ...string) error {
	cmd, err := h.Sudo(fmt.Sprintf("zypper -n install -y %s", strings.Join(s, " ")))
	if err != nil {
		return err
	}
	if err := h.Exec("zypper refresh", exec.Sudo(h)); err != nil {
		return err
	}
	return h.Execf("zypper -n install -y %s", strings.Join(s, " "), exec.Sudo(h))
}

func init() {
	registry.RegisterOSModule(
		func(os rig.OSVersion) bool {
			return os.ID == "sles"
		},
		func() interface{} {
			return SLES{}
		},
	)
}
