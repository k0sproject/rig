package os

import (
	"io"
	"io/fs"

	"github.com/k0sproject/rig/exec"
)

// Host is an interface to a host object that has the functions needed by the various OS support packages
type Host interface {
	Upload(source, destination string, perm fs.FileMode, opts ...exec.Option) error
	Exec(cmd string, opts ...exec.Option) error
	ExecOutput(cmd string, opts ...exec.Option) (string, error)
	Execf(cmd string, argsOrOpts ...any) error
	ExecOutputf(cmd string, argsOrOpts ...any) (string, error)
	ExecStreams(cmd string, stdin io.ReadCloser, stdout io.Writer, stderr io.Writer, opts ...exec.Option) (exec.Waiter, error)
	String() string
	Sudo(cmd string) (string, error)
}
