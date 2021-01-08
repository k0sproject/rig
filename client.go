package rig

import (
	"github.com/k0sproject/rig/exec"
)

// Connection is an interface to remote host connections
type Client interface {
	Connect() error
	Disconnect()
	Upload(source string, destination string) error
	IsWindows() bool
	Exec(string, ...exec.Option) error
	ExecInteractive(string) error
	String() string
	IsConnected() bool
}
