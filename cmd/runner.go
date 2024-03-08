// Package cmd defines types and functions for running commands.
package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/k0sproject/rig/v2/protocol"
)

var (
	// ErrInvalidCommand is returned when a command is somehow invalid.
	ErrInvalidCommand = errors.New("invalid command")

	// ErrWroteStderr is returned when a windows command writes to stderr, unless AllowWinStderr is set.
	ErrWroteStderr = errors.New("command wrote output to stderr")
)

// DecorateFunc is a function that takes a string and returns a decorated string.
type DecorateFunc func(string) string

// Formatter is an interface that can format commands.
type Formatter interface {
	Command(cmd string) string
}

// WindowsChecker is implemented by types that can check if the underlying host OS is Windows.
type WindowsChecker interface {
	IsWindows() bool
}

// SimpleRunner is a command runner that can run commands without a context.
type SimpleRunner interface {
	fmt.Stringer
	WindowsChecker
	Exec(command string, opts ...ExecOption) error
	ExecOutput(command string, opts ...ExecOption) (string, error)
	ExecReader(command string, opts ...ExecOption) io.Reader
	ExecScanner(command string, opts ...ExecOption) *bufio.Scanner
	StartBackground(command string, opts ...ExecOption) (protocol.Waiter, error)
}

// ContextRunner is a command runner that can run commands with a context.
type ContextRunner interface {
	fmt.Stringer
	WindowsChecker
	ExecContext(ctx context.Context, command string, opts ...ExecOption) error
	ExecOutputContext(ctx context.Context, command string, opts ...ExecOption) (string, error)
	ExecReaderContext(ctx context.Context, command string, opts ...ExecOption) io.Reader
	Start(ctx context.Context, command string, opts ...ExecOption) (protocol.Waiter, error)
}

// Runner is a full featured command runner for clients.
type Runner interface {
	Formatter
	WindowsChecker
	SimpleRunner
	ContextRunner
	Formatter
	// ProcessStarter is included to allow runners to accept another runner as their connection for chaining.
	protocol.ProcessStarter
}
