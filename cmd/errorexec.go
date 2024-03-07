package cmd

import (
	"bufio"
	"context"
	"io"

	"github.com/k0sproject/rig/protocol"
)

var _ Runner = (*ErrorExecutor)(nil)

// ErrorExecutor is an executor that always returns the given error. It implements
// the [Runner] interface.
//
// This is used to make some of the accessors easier to use without having to check for
// the second return value. Instead of a, err := c.Foo(), you can use
// c.Foo().DoSomething() and it will return the error from Foo() when DoSomething()
// is called.
type ErrorExecutor struct {
	Err error
}

// NewErrorExecutor returns a new ErrorExecutor with the given error.
func NewErrorExecutor(err error) *ErrorExecutor {
	return &ErrorExecutor{Err: err}
}

// Command returns the command string as is.
func (r *ErrorExecutor) Command(cmd string) string { return cmd }

// IsWindows returns false.
func (r *ErrorExecutor) IsWindows() bool { return false }

// String returns "error-executor".
func (r *ErrorExecutor) String() string { return "error-executor" }

// Start returns the given error.
func (r *ErrorExecutor) Start(_ context.Context, _ string, _ ...ExecOption) (protocol.Waiter, error) {
	return nil, r.Err
}

// StartBackground returns the given error.
func (r *ErrorExecutor) StartBackground(_ string, _ ...ExecOption) (protocol.Waiter, error) {
	return nil, r.Err
}

// ExecContext returns the given error.
func (r *ErrorExecutor) ExecContext(_ context.Context, _ string, _ ...ExecOption) error {
	return r.Err
}

// Exec returns the given error.
func (r *ErrorExecutor) Exec(_ string, _ ...ExecOption) error { return r.Err }

// ExecOutputContext returns the given error and an empty string.
func (r *ErrorExecutor) ExecOutputContext(_ context.Context, _ string, _ ...ExecOption) (string, error) {
	return "", r.Err
}

// ExecOutput returns the given error and an empty string.
func (r *ErrorExecutor) ExecOutput(_ string, _ ...ExecOption) (string, error) {
	return "", r.Err
}

// ExecReaderContext returns a pipereader where the writer end is closed with the given error.
func (r *ErrorExecutor) ExecReaderContext(_ context.Context, _ string, _ ...ExecOption) io.Reader {
	pr, pw := io.Pipe()
	pw.CloseWithError(r.Err)
	return pr
}

// ExecReader returns a pipereader where the writer end is closed with the given error.
func (r *ErrorExecutor) ExecReader(command string, opts ...ExecOption) io.Reader {
	return r.ExecReaderContext(context.Background(), command, opts...)
}

// ExecScannerContext returns a scanner that reads from a pipereader where the writer end is closed with the given error.
func (r *ErrorExecutor) ExecScannerContext(ctx context.Context, command string, opts ...ExecOption) *bufio.Scanner {
	return bufio.NewScanner(r.ExecReaderContext(ctx, command, opts...))
}

// ExecScanner returns a scanner that reads from a pipereader where the writer end is closed with the given error.
func (r *ErrorExecutor) ExecScanner(command string, opts ...ExecOption) *bufio.Scanner {
	return r.ExecScannerContext(context.Background(), command, opts...)
}

// StartProcess returns the given error.
func (r *ErrorExecutor) StartProcess(_ context.Context, _ string, _ io.Reader, _ io.Writer, _ io.Writer) (protocol.Waiter, error) {
	return nil, r.Err
}
