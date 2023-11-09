package os

import (
	"github.com/k0sproject/rig/exec"
)

// Host is an interface to a host object that has the functions needed by the various OS support packages
type Host interface {
	Upload(source, destination string, opts ...exec.Option) error
	Exec(cmd string, opts ...exec.Option) error
	ExecOutput(cmd string, opts ...exec.Option) (string, error)
	Execf(cmd string, argsOrOpts ...any) error
	ExecOutputf(cmd string, argsOrOpts ...any) (string, error)
	String() string
	Sudo(cmd string) (string, error)
}
