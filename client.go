package rig

import (
	"github.com/k0sproject/rig/exec"
)

// Client is an interface to a remote host connection
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
