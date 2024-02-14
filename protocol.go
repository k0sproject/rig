package rig

import (
	"context"
	"fmt"
	"io"

	"github.com/k0sproject/rig/exec"
)

// ProcessStarter can start processes
type ProcessStarter interface {
	StartProcess(ctx context.Context, cmd string, stdin io.Reader, stdout io.Writer, stderr io.Writer) (exec.Waiter, error)
}

// connector is a connection that can be established
type connector interface {
	Connect() error
}

// disconnector is a connection that can be closed
type disconnector interface {
	Disconnect()
}

// WindowsChecker is a type that can check if the underlying system is Windows
type WindowsChecker interface {
	IsWindows() bool
}

// interactiveExecer is a connection that can start an interactive session
type interactiveExecer interface {
	ExecInteractive(cmd string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error
}

// Protocol is the minimum interface for protocol implementations
type Protocol interface {
	fmt.Stringer
	ProcessStarter
	WindowsChecker
	Protocol() string
	IPAddress() string
}
