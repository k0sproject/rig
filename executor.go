package rig

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/k0sproject/rig/exec"
)

// validate interfaces.
var (
	_ exec.Runner           = (*HostRunner)(nil)
	_ exec.SimpleRunner     = (*HostRunner)(nil)
	_ exec.ContextRunner    = (*HostRunner)(nil)
	_ exec.CommandFormatter = (*HostRunner)(nil)
	_ fmt.Stringer          = (*HostRunner)(nil)

	DisableRedact = false
)

// DisableRedact will disable all redaction of sensitive data.

// Waiter is a process that can be waited to finish.
type Waiter interface {
	Wait() error
}

// HostRunner is an exec.Runner that runs commands on a host.
type HostRunner struct {
	connection exec.ProcessStarter
	decorators []exec.DecorateFunc
}

// NewHostRunner returns a new HostRunner.
func NewHostRunner(host exec.ProcessStarter, decorators ...exec.DecorateFunc) *HostRunner {
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

var printfErrorRegex = regexp.MustCompile(`[^\\]%![a-zA-Z]\(.+?\)`)

// windowsWaiter is a Waiter that checks for errors written to stderr.
type windowsWaiter struct {
	waiter  exec.Waiter
	wroteFn func() bool
}

// Wait waits for the command to finish and returns an error if it fails or if it wrote to stderr.
func (w *windowsWaiter) Wait() error {
	if err := w.waiter.Wait(); err != nil {
		return err //nolint:wrapcheck
	}
	if w.wroteFn() {
		return exec.ErrWroteStderr
	}
	return nil
}

func isExe(cmd string) bool {
	firstWordIdx := strings.Index(cmd, " ")
	if firstWordIdx == -1 {
		return strings.HasSuffix(cmd, ".exe")
	}
	return strings.HasSuffix(cmd[:firstWordIdx], ".exe")
}

// Start starts the command and returns a Waiter.
func (r *HostRunner) Start(ctx context.Context, command string, opts ...exec.Option) (exec.Waiter, error) {
	if ctx.Err() != nil {
		return nil, fmt.Errorf("runner context error: %w", ctx.Err())
	}
	if printfErrorRegex.MatchString(command) {
		return nil, fmt.Errorf("%w: refusing to run a command containing printf-style %%!(..) errors: %s", exec.ErrInvalidCommand, command)
	}

	execOpts := exec.Build(opts...)
	cmd := r.Command(execOpts.Command(command))
	if r.connection.IsWindows() {
		// we don't know if the default shell is cmd or powershell, so to make sure commands
		// without a shell prefix go consistently go through the same shell, we default to running
		// non-prefixed commands through cmd.exe.
		if !isExe(cmd) {
			cmd = "cmd.exe /C " + cmd
		}
	}
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
func (r *HostRunner) StartBackground(command string, opts ...exec.Option) (exec.Waiter, error) {
	return r.Start(context.Background(), command, opts...)
}

// ExecContext executes the command and returns an error if unsuccessful.
func (r *HostRunner) ExecContext(ctx context.Context, command string, opts ...exec.Option) error {
	proc, err := r.Start(ctx, command, opts...)
	if err != nil {
		return fmt.Errorf("start command: %w", err)
	}
	if err := proc.Wait(); err != nil {
		return fmt.Errorf("command result: %w", err)
	}

	return nil
}

// Exec executes the command and returns an error if unsuccessful.
func (r *HostRunner) Exec(command string, opts ...exec.Option) error {
	return r.ExecContext(context.Background(), command, opts...)
}

// ExecOutputContext executes the command and returns the stdout output or an error.
func (r *HostRunner) ExecOutputContext(ctx context.Context, command string, opts ...exec.Option) (string, error) {
	out := &bytes.Buffer{}
	defer out.Reset()

	opts = append(opts, exec.Stdout(out))

	proc, err := r.Start(ctx, command, opts...)
	if err != nil {
		return "", fmt.Errorf("start command: %w", err)
	}
	if err := proc.Wait(); err != nil {
		return "", fmt.Errorf("command result: %w", err)
	}

	execOpts := exec.Build(opts...)
	return execOpts.FormatOutput(out.String()), nil
}

// ExecOutput executes the command and returns the stdout output or an error.
func (r *HostRunner) ExecOutput(command string, opts ...exec.Option) (string, error) {
	return r.ExecOutputContext(context.Background(), command, opts...)
}

// ExecScannerContext executes the command and returns a bufio.Scanner to read the stdout output.
// The scanner will close when the command finishes and scanner.Err() may return the command's error.
func (r *HostRunner) ExecScannerContext(ctx context.Context, command string, opts ...exec.Option) (*bufio.Scanner, error) {
	if ctx.Err() != nil {
		return nil, fmt.Errorf("runner context error: %w", ctx.Err())
	}
	pipeR, pipeW := io.Pipe()
	opts = append(opts, exec.Stdout(pipeW))

	proc, err := r.Start(ctx, command, opts...)
	if err != nil {
		pipeW.Close()
		pipeR.Close()
		return nil, fmt.Errorf("start command: %w", err)
	}

	go func() {
		if err := proc.Wait(); err != nil {
			pipeW.CloseWithError(fmt.Errorf("command wait: %w", err))
			return
		}
		pipeW.Close()
	}()

	scanner := bufio.NewScanner(pipeR)
	return scanner, nil
}

// ExecScanner executes the command and returns a bufio.Scanner to read the stdout output.
// The scanner will close when the command finishes and scanner.Err() may return the command's error.
func (r *HostRunner) ExecScanner(command string, opts ...exec.Option) (*bufio.Scanner, error) {
	return r.ExecScannerContext(context.Background(), command, opts...)
}

// StartProcess calls the connection's StartProcess method. This is done to satisfy the
// connection interface and thus allow chaining of runners.
func (r *HostRunner) StartProcess(ctx context.Context, command string, stdin io.Reader, stdout io.Writer, stderr io.Writer) (exec.Waiter, error) {
	waiter, err := r.connection.StartProcess(ctx, r.Command(command), stdin, stdout, stderr)
	if err != nil {
		return nil, fmt.Errorf("runner start process: %w", err)
	}
	return waiter, nil
}
