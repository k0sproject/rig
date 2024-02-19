// Package protocol : contains the interfaces for the protocol implementations
package protocol

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/k0sproject/rig/exec"
)

var (
	// ErrValidationFailed is returned when a connection config fails validation.
	ErrValidationFailed = errors.New("validation failed")

	// ErrAbort is returned when retrying an operation will not result in a
	// different outcome.
	ErrAbort = errors.New("operation can not be completed")
)

// ProcessStarter can start processes.
type ProcessStarter interface {
	StartProcess(ctx context.Context, cmd string, stdin io.Reader, stdout io.Writer, stderr io.Writer) (exec.Waiter, error)
}

// Connector is a connection that can be established.
type Connector interface {
	Connect() error
}

// Disconnector is a connection that can be closed.
type Disconnector interface {
	Disconnect()
}

// WindowsChecker is a type that can check if the underlying system is Windows.
type WindowsChecker interface {
	IsWindows() bool
}

// InteractiveExecer is a connection that can start an interactive session.
type InteractiveExecer interface {
	ExecInteractive(cmd string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error
}

// Connection is the minimum interface for protocol implementations.
type Connection interface {
	fmt.Stringer
	Protocol() string
	IPAddress() string
	ProcessStarter
	WindowsChecker
}
