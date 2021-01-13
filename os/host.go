package os

import (
	"github.com/k0sproject/rig/exec"
)

type Host interface {
	Upload(source string, destination string) error
	Exec(string, ...exec.Option) error
	ExecWithOutput(string, ...exec.Option) (string, error)
	Execf(string, ...interface{}) error
	ExecWithOutputf(string, ...interface{}) (string, error)
	String() string
}
