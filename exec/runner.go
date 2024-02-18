package exec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
)

var _ connection = (*HostRunner)(nil)

type connection interface {
	fmt.Stringer
	IsWindows() bool
	StartProcess(ctx context.Context, cmd string, stdin io.Reader, stdout io.Writer, stderr io.Writer) (Waiter, error)
}

// CommandFormatter is an interface that can format commands.
type CommandFormatter interface {
	Command(cmd string) string
	Commandf(format string, args ...any) string
}

// SimpleRunner is a command runner that can run commands without a context.
type SimpleRunner interface {
	// reconsider adding execf/execoutputf

	fmt.Stringer
	IsWindows() bool
	Exec(format string, argsOrOpts ...any) error
	ExecOutput(format string, argsOrOpts ...any) (string, error)
	StartBackground(format string, argsOrOpts ...any) (Waiter, error)
}

// ContextRunner is a command runner that can run commands with a context.
type ContextRunner interface {
	fmt.Stringer
	IsWindows() bool
	ExecContext(ctx context.Context, format string, argsOrOpts ...any) error
	ExecOutputContext(ctx context.Context, format string, argsOrOpts ...any) (string, error)
	Start(ctx context.Context, format string, argsOrOpts ...any) (Waiter, error)
}

// Runner is a full featured command runner for clients.
type Runner interface {
	SimpleRunner
	ContextRunner
	CommandFormatter
	connection
}

// validate interfaces.
var (
	_ Runner           = (*HostRunner)(nil)
	_ SimpleRunner     = (*HostRunner)(nil)
	_ ContextRunner    = (*HostRunner)(nil)
	_ CommandFormatter = (*HostRunner)(nil)
	_ fmt.Stringer     = (*HostRunner)(nil)
)

// HostRunner is an exec.Runner that runs commands on a host.
type HostRunner struct {
	connection connection
	decorators []DecorateFunc
}

// ErrWroteStderr is returned when a windows command writes to stderr, unless AllowWinStderr is set.
var ErrWroteStderr = errors.New("command wrote output to stderr")

// windowsWaiter is a Waiter that checks for errors written to stderr.
type windowsWaiter struct {
	waiter  Waiter
	wroteFn func() bool
}

// Wait waits for the command to finish and returns an error if it fails or if it wrote to stderr.
func (w *windowsWaiter) Wait() error {
	if err := w.waiter.Wait(); err != nil {
		return err //nolint:wrapcheck
	}
	if w.wroteFn() {
		return ErrWroteStderr
	}
	return nil
}

// NewHostRunner returns a new HostRunner.
func NewHostRunner(host connection, decorators ...DecorateFunc) *HostRunner {
	return &HostRunner{
		connection: host,
		decorators: decorators,
	}
}

// IsWindows returns true if the host is windows.
func (r *HostRunner) IsWindows() bool {
	return r.connection.IsWindows()
}

// Command returns the command string decorated with the runner's decorators.
func (r *HostRunner) Command(cmd string) string {
	for _, decorator := range r.decorators {
		cmd = decorator(cmd)
	}
	return cmd
}

// Commandf formats the command string and returns it.
func (r *HostRunner) Commandf(format string, args ...any) string {
	return r.Command(fmt.Sprintf(format, args...))
}

// String returns the client's string representation.
func (r *HostRunner) String() string {
	return r.connection.String()
}

// Start starts the command and returns a Waiter.
func (r *HostRunner) Start(ctx context.Context, format string, argsOrOpts ...any) (Waiter, error) {
	opts, args := groupParams(argsOrOpts...)
	execOpts := Build(opts...)
	cmd := r.Command(execOpts.Commandf(format, args...))
	execOpts.LogCmd(cmd)
	waiter, err := r.connection.StartProcess(ctx, cmd, execOpts.Stdin(), execOpts.Stdout(), execOpts.Stderr())
	if err != nil {
		return nil, fmt.Errorf("runner start command: %w", err)
	}
	if !execOpts.AllowWinStderr() && r.connection.IsWindows() {
		return &windowsWaiter{waiter, execOpts.WroteErr}, nil
	}
	return waiter, nil
}

// StartBackground starts the command and returns a Waiter.
func (r *HostRunner) StartBackground(format string, argsOrOpts ...any) (Waiter, error) {
	return r.Start(context.Background(), format, argsOrOpts...)
}

// ExecContext executes the command and returns an error if unsuccessful.
func (r *HostRunner) ExecContext(ctx context.Context, format string, argsOrOpts ...any) error {
	proc, err := r.Start(ctx, format, argsOrOpts...)
	if err != nil {
		return fmt.Errorf("start command: %w", err)
	}
	if err := proc.Wait(); err != nil {
		return fmt.Errorf("command result: %w", err)
	}

	return nil
}

// Exec executes the command and returns an error if unsuccessful.
func (r *HostRunner) Exec(format string, argsOrOpts ...any) error {
	return r.ExecContext(context.Background(), format, argsOrOpts...)
}

// ExecOutputContext executes the command and returns the stdout output or an error.
func (r *HostRunner) ExecOutputContext(ctx context.Context, format string, argsOrOpts ...any) (string, error) {
	opts, _ := groupParams(argsOrOpts...)
	execOpts := Build(opts...)
	out := &bytes.Buffer{}
	argsOrOpts = append(argsOrOpts, Stdout(out))

	proc, err := r.Start(ctx, format, argsOrOpts...)
	if err != nil {
		return "", fmt.Errorf("start command: %w", err)
	}
	if err := proc.Wait(); err != nil {
		return "", fmt.Errorf("command result: %w", err)
	}

	return execOpts.FormatOutput(out.String()), nil
}

// ExecOutput executes the command and returns the stdout output or an error.
func (r *HostRunner) ExecOutput(cmd string, argsOrOpts ...any) (string, error) {
	return r.ExecOutputContext(context.Background(), cmd, argsOrOpts...)
}

// StartProcess calls the connection's StartProcess method. This is done to satisfy the client
// interface and allow chaining of runners.
func (r *HostRunner) StartProcess(ctx context.Context, cmd string, stdin io.Reader, stdout io.Writer, stderr io.Writer) (Waiter, error) {
	waiter, err := r.connection.StartProcess(ctx, r.Command(cmd), stdin, stdout, stderr)
	if err != nil {
		return nil, fmt.Errorf("runner start process: %w", err)
	}
	return waiter, nil
}

func groupParams(params ...any) ([]Option, []any) {
	var opts []Option
	var args []any
	for _, v := range params {
		switch vv := v.(type) {
		case []any:
			o, a := groupParams(vv...)
			opts = append(opts, o...)
			args = append(args, a...)
		case Option:
			opts = append(opts, vv)
		default:
			args = append(args, vv)
		}
	}
	return opts, args
}

// NewErrorRunner returns a new ErrorRunner.
func NewErrorRunner(err error) *ErrorRunner {
	return &ErrorRunner{err: err}
}

// ErrorRunner is an exec.Runner that always returns an error.
type ErrorRunner struct {
	err error
}

// IsWindows returns false.
func (n ErrorRunner) IsWindows() bool { return false }

// String returns "always failing error runner".
func (n ErrorRunner) String() string { return "always failing error runner" }

// Exec returns the error.
func (n ErrorRunner) Exec(_ string, _ ...any) error { return n.err }

// ExecOutput returns the error.
func (n ErrorRunner) ExecOutput(_ string, _ ...any) (string, error) { return "", n.err }

// ExecContext returns the error.
func (n ErrorRunner) ExecContext(_ context.Context, _ string, _ ...any) error {
	return n.err
}

// ExecOutputContext returns the error.
func (n ErrorRunner) ExecOutputContext(_ context.Context, _ string, _ ...any) (string, error) {
	return "", n.err
}

// Commandf formats the string and returns it.
func (n ErrorRunner) Commandf(format string, args ...any) string { return fmt.Sprintf(format, args...) }

// Command returns the string as is.
func (n ErrorRunner) Command(cmd string) string { return cmd }

// Start returns the error.
func (n ErrorRunner) Start(_ context.Context, _ string, _ ...any) (Waiter, error) {
	return nil, n.err
}

// StartBackground returns the error.
func (n ErrorRunner) StartBackground(_ string, _ ...any) (Waiter, error) {
	return nil, n.err
}

// StartProcess returns the error.
func (r *ErrorRunner) StartProcess(_ context.Context, _ string, _ io.Reader, _ io.Writer, _ io.Writer) (Waiter, error) {
	return nil, r.err
}
