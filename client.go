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

// Connector is a connection that can be established
type Connector interface {
	Connect() error
}

// Disconnector is a connection that can be closed
type Disconnector interface {
	Disconnect()
}

// WindowsChecker is a type that can check if the underlying system is Windows
type WindowsChecker interface {
	IsWindows() bool
}

// InteractiveExecer is a connection that can start an interactive session
type InteractiveExecer interface {
	ExecInteractive(cmd string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error
}

// Client is the minimum interface for protocol implementations
type Client interface {
	fmt.Stringer
	ProcessStarter
	WindowsChecker
	Protocol() string
	IPAddress() string
}
