// Package protocol contains the interfaces for the protocol implementations
package protocol

import (
	"context"
	"errors"
	"fmt"
	"io"
)

var (
	// ErrValidationFailed is returned when a connection config fails validation.
	ErrValidationFailed = errors.New("validation failed")

	// ErrNonRetryable is returned when retrying an operation will not result in a
	// different outcome.
	ErrNonRetryable = errors.New("operation cannot be completed")
)

// Waiter is a process that can be waited to finish.
type Waiter interface {
	Wait() error
}

// ProcessStarter can start processes.
type ProcessStarter interface {
	StartProcess(ctx context.Context, cmd string, stdin io.Reader, stdout io.Writer, stderr io.Writer) (Waiter, error)
}

// Connector is a connection that can be established.
type Connector interface {
	Connect(ctx context.Context) error
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
	// Protocol returns the protocol family: "SSH", "WinRM", or "Local".
	// Both the native SSH and OpenSSH implementations return "SSH".
	Protocol() string
	// ProtocolName returns the specific implementation name: "SSH", "OpenSSH",
	// "WinRM", or "Local". Use this for logging or diagnostics where the
	// distinction between native SSH and OpenSSH matters.
	ProtocolName() string
	// IsConnected returns true if the connection is currently active.
	// All implementations perform an active liveness probe (e.g. SSH keepalive,
	// ssh -O check for OpenSSH multiplexing, or a no-op command for WinRM).
	// Localhost always returns true. Callers should be aware this may block
	// up to a timeout (typically 10s) and may cause side-effects on the remote.
	IsConnected() bool
	IPAddress() string
	ProcessStarter
	WindowsChecker
}
