package cmd

import (
	"bufio"
	"context"
	"io"

	"github.com/k0sproject/rig/protocol"
)

type ErrorExecutor struct {
	Err error
}

func NewErrorExecutor(err error) *ErrorExecutor {
	return &ErrorExecutor{Err: err}
}

func (r *ErrorExecutor) Command(cmd string) string { return cmd }
func (r *ErrorExecutor) IsWindows() bool           { return false }
func (r *ErrorExecutor) String() string            { return "error-executor" }
func (r *ErrorExecutor) Start(ctx context.Context, command string, opts ...ExecOption) (protocol.Waiter, error) {
	return nil, r.Err
}
func (r *ErrorExecutor) StartBackground(command string, opts ...ExecOption) (protocol.Waiter, error) {
	return nil, r.Err
}
func (r *ErrorExecutor) ExecContext(ctx context.Context, command string, opts ...ExecOption) error {
	return r.Err
}
func (r *ErrorExecutor) Exec(command string, opts ...ExecOption) error { return r.Err }
func (r *ErrorExecutor) ExecOutputContext(ctx context.Context, command string, opts ...ExecOption) (string, error) {
	return "", r.Err
}
func (r *ErrorExecutor) ExecOutput(command string, opts ...ExecOption) (string, error) {
	return "", r.Err
}
func (r *ErrorExecutor) ExecReaderContext(ctx context.Context, command string, opts ...ExecOption) io.Reader {
	pr, pw := io.Pipe()
	pw.CloseWithError(r.Err)
	return pr
}
func (r *ErrorExecutor) ExecReader(command string, opts ...ExecOption) io.Reader {
	return r.ExecReaderContext(context.Background(), command, opts...)
}

func (r *ErrorExecutor) ExecScannerContext(ctx context.Context, command string, opts ...ExecOption) *bufio.Scanner {
	return bufio.NewScanner(r.ExecReaderContext(ctx, command, opts...))
}

func (r *ErrorExecutor) ExecScanner(command string, opts ...ExecOption) *bufio.Scanner {
	return r.ExecScannerContext(context.Background(), command, opts...)
}

func (r *ErrorExecutor) StartProcess(ctx context.Context, command string, stdin io.Reader, stdout io.Writer, stderr io.Writer) (protocol.Waiter, error) {
	return nil, r.Err
}
