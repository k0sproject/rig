package linux

import (
	"github.com/k0sproject/rig/os"
)

// EnterpriseLinux is a base package for several RHEL-like enterprise linux distributions
type EnterpriseLinux struct {
	os.Linux
}
