package client

import (
	"io"
	"net"

	"github.com/k0sproject/rig/exec"
)

type Connection interface {
	Protocol() string
	Address() net.Addr
	String() string
	Disconnect() error
	IsWindows() bool
	Exec(string, io.Reader, io.Writer, io.Writer) (exec.Process, error)
	ExecInteractive(string, io.Reader, io.Writer, io.Writer) error
}
