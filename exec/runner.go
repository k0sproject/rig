package exec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
)

type client interface {
	fmt.Stringer
	IsWindows() bool
	StartProcess(ctx context.Context, cmd string, stdin io.Reader, stdout io.Writer, stderr io.Writer) (Waiter, error)
}

type CommandFormatter interface {
	Command(cmd string) string
	Commandf(format string, args ...any) string
}

type SimpleRunner interface {
	// reconsider adding execf/execoutputf

	fmt.Stringer
	IsWindows() bool
	Exec(format string, argsOrOpts ...any) error
	ExecOutput(format string, argsOrOpts ...any) (string, error)
	StartBackground(format string, argsOrOpts ...any) (Waiter, error)
}

type ContextRunner interface {
	fmt.Stringer
	IsWindows() bool
	ExecContext(ctx context.Context, format string, argsOrOpts ...any) error
	ExecOutputContext(ctx context.Context, format string, argsOrOpts ...any) (string, error)
	Start(ctx context.Context, format string, argsOrOpts ...any) (Waiter, error)
}

type Runner interface {
	SimpleRunner
	ContextRunner
	CommandFormatter
}

// validate interfaces
var (
	_ Runner           = (*HostRunner)(nil)
	_ SimpleRunner     = (*HostRunner)(nil)
	_ ContextRunner    = (*HostRunner)(nil)
	_ CommandFormatter = (*HostRunner)(nil)
	_ fmt.Stringer     = (*HostRunner)(nil)
)

type HostRunner struct {
	client     client
	decorators []DecorateFunc
}

// this way you could have something like:
// h.Sudo().Exec("apt-get install -y nginx")
// which would create a SudoRunner like this:
// NewRunner(h, SudoDecorator)
//
// maybe there could be a k0s-runner like:
// h.K0s().Exec("install")
// or a kubectl-runner like:
// h.Kubectl(kubectl.WithKubeconfigPath(...)).Exec("apply -f somefile.yaml")

var ErrWroteErr = errors.New("command output to stderr")

// windowsWaiter is a Waiter that checks for errors written to stderr
type windowsWaiter struct {
	waiter  Waiter
	wroteFn func() bool
}

func (w *windowsWaiter) Wait() error {
	if err := w.waiter.Wait(); err != nil {
		return err //nolint:wrapcheck
	}
	if w.wroteFn() {
		return ErrWroteErr
	}
	return nil
}

func NewHostRunner(host client, decorators ...DecorateFunc) *HostRunner {
	return &HostRunner{
		client:     host,
		decorators: decorators,
	}
}

func (r *HostRunner) IsWindows() bool {
	return r.client.IsWindows()
}

func (r *HostRunner) Command(cmd string) string {
	for _, decorator := range r.decorators {
		cmd = decorator(cmd)
	}
	return cmd
}

func (r *HostRunner) Commandf(format string, args ...any) string {
	return r.Command(fmt.Sprintf(format, args...))
}

func (r *HostRunner) String() string {
	return r.client.String()
}

func (r *HostRunner) Start(ctx context.Context, format string, argsOrOpts ...any) (Waiter, error) {
	opts, args := groupParams(argsOrOpts...)
	execOpts := Build(opts...)
	cmd := r.Command(execOpts.Commandf(format, args...))
	execOpts.LogCmd(r.String(), cmd)
	waiter, err := r.client.StartProcess(ctx, cmd, execOpts.Stdin(), execOpts.Stdout(), execOpts.Stderr())
	if err != nil {
		return nil, fmt.Errorf("runner start command: %w", err)
	}
	if !execOpts.AllowWinStderr() && r.client.IsWindows() {
		return &windowsWaiter{waiter, execOpts.WroteErr}, nil
	}
	return waiter, nil
}

func (r *HostRunner) StartBackground(format string, argsOrOpts ...any) (Waiter, error) {
	return r.Start(context.Background(), format, argsOrOpts...)
}

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

func (r *HostRunner) Exec(format string, argsOrOpts ...any) error {
	return r.ExecContext(context.Background(), format, argsOrOpts...)
}

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

func (r *HostRunner) ExecOutput(cmd string, argsOrOpts ...any) (string, error) {
	return r.ExecOutputContext(context.Background(), cmd, argsOrOpts...)
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

type ErrorRunner struct {
	Err error
}
func (n ErrorRunner) IsWindows() bool {
	return false
}
func (n ErrorRunner) String() string {
	return "always failing error runner"
}
func (n ErrorRunner) Exec(format string, argsOrOpts ...any) error {
	return n.Err
}
func (n ErrorRunner) ExecOutput(format string, argsOrOpts ...any) (string, error) {
	return "", n.Err
}
func (n ErrorRunner) ExecContext(ctx context.Context, format string, argsOrOpts ...any) error {
	return n.Err
}
func (n ErrorRunner) ExecOutputContext(ctx context.Context, format string, argsOrOpts ...any) (string, error) {
	return "", n.Err
}
func (n ErrorRunner) Commandf(format string, args ...any) string {
	return fmt.Sprintf(format, args...)
}
func (n ErrorRunner) Command(cmd string) string {
	return cmd
}
func (n ErrorRunner) Start(ctx context.Context, format string, argsOrOpts ...any) (Waiter, error) {
	return nil, n.Err
}
func (n ErrorRunner) StartBackground(format string, argsOrOpts ...any) (Waiter, error) {
	return nil, n.Err
}
