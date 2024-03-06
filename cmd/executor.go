package cmd

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/k0sproject/rig/log"
	"github.com/k0sproject/rig/protocol"
)

// validate interfaces.
var (
	_ Runner        = (*Executor)(nil)
	_ SimpleRunner  = (*Executor)(nil)
	_ ContextRunner = (*Executor)(nil)
	_ Formatter     = (*Executor)(nil)
	_ fmt.Stringer  = (*Executor)(nil)

	// DisableRedact will disable all redaction of sensitive data.
	DisableRedact = false

	errInternal = errors.New("internal error")
)

// Executor is an Runner that runs commands on a host.
type Executor struct {
	log.LoggerInjectable
	connection protocol.ProcessStarter
	decorators []DecorateFunc
	isWin      func() bool
}

func isWinFunc(conn protocol.ProcessStarter) func() bool {
	return func() bool {
		if wc, ok := conn.(WindowsChecker); ok {
			return wc.IsWindows()
		}
		cmd, err := conn.StartProcess(context.Background(), "ver.exe", nil, nil, nil)
		if err != nil || cmd.Wait() != nil {
			return false
		}
		return true
	}
}

// NewExecutor returns a new Executor.
func NewExecutor(conn protocol.ProcessStarter, decorators ...DecorateFunc) *Executor {
	return &Executor{
		connection: conn,
		decorators: decorators,
		isWin:      sync.OnceValue(isWinFunc(conn)),
	}
}

// Command returns the command string decorated with the runner's decorators.
func (r *Executor) Command(cmd string) string {
	for _, decorator := range r.decorators {
		cmd = decorator(cmd)
	}
	return cmd
}

func (r *Executor) IsWindows() bool {
	return r.isWin()
}

// String returns the client's string representation.
func (r *Executor) String() string {
	if s, ok := r.connection.(fmt.Stringer); ok {
		return s.String()
	}
	return "rig-executor"
}

func getPrintfErrorAt(s string, idx int) error {
	log.Trace(context.Background(), "checking for printf error at index", log.KeyCommand, s, "index", idx)
	if idx > len(s)-6 {
		// can't fit %!a()
		return nil
	}

	if s[idx+1] != '!' {
		return nil
	}

	if (s[idx+2] < 'a' || s[idx+2] > 'z') && (s[idx+2] < 'A' || s[idx+2] > 'Z') {
		return nil
	}

	if s[idx+3] != '(' {
		return nil
	}

	end := strings.Index(s[idx:], ")")
	if end == -1 {
		return nil
	}

	return fmt.Errorf("%w: printf error at index %d: %s", ErrInvalidCommand, idx, s[idx:idx+end+1])
}

func findPrintfError(s string) error {
	log.Trace(context.Background(), "checking for printf errors", log.KeyCommand, s)
	var err error
	for idx, c := range s {
		if c == '%' && idx < len(s)-1 {
			if e := getPrintfErrorAt(s, idx); e != nil {
				err = errors.Join(e, err)
			}
		}
	}
	return err
}

// windowsWaiter is a Waiter that checks for errors written to stderr.
type windowsWaiter struct {
	waiter  protocol.Waiter
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

func isExe(cmd string) bool {
	firstWordIdx := strings.Index(cmd, " ")
	if firstWordIdx == -1 {
		return strings.HasSuffix(cmd, ".exe")
	}
	return strings.HasSuffix(cmd[:firstWordIdx], ".exe")
}

// Start starts the command and returns a Waiter.
func (r *Executor) Start(ctx context.Context, command string, opts ...ExecOption) (protocol.Waiter, error) {
	log.Trace(ctx, "starting command", log.HostAttr(r), log.KeyCommand, command)
	if ctx.Err() != nil {
		return nil, fmt.Errorf("runner context error: %w", ctx.Err())
	}
	if err := findPrintfError(command); err != nil {
		return nil, fmt.Errorf("refusing to run a command containing printf-style %%!(..) errors: %w", err)
	}

	execOpts := Build(opts...)
	r.InjectLoggerTo(execOpts)

	cmd := r.Command(execOpts.Command(command))
	if r.IsWindows() {
		// we don't know if the default shell is cmd or powershell, so to make sure commands
		// without a shell prefix go consistently go through the same shell, we default to running
		// non-prefixed commands through cmd.exe.
		if !isExe(cmd) {
			cmd = "cmd.exe /C " + cmd
		}
	}
	log.Trace(ctx, "formatted command", log.HostAttr(r), log.KeyCommand, cmd)
	if execOpts.LogCommand() {
		r.Log().Debug("executing command", log.HostAttr(r), log.KeyCommand, execOpts.Redact(cmd))
	}
	log.Trace(ctx, "starting process", log.HostAttr(r), log.KeyCommand, cmd)
	waiter, err := r.connection.StartProcess(ctx, cmd, execOpts.Stdin(), execOpts.Stdout(), execOpts.Stderr())
	if err != nil {
		log.Trace(ctx, "start process failed", log.HostAttr(r), log.KeyCommand, cmd, log.KeyError, err)
		return nil, fmt.Errorf("runner start command: %w", err)
	}
	if waiter == nil {
		log.Trace(ctx, "start process returned nil waiter", log.HostAttr(r), log.KeyCommand, cmd, log.KeyError, err)
		return nil, fmt.Errorf("%w: connection returned no error but a nil waiter", errInternal)
	}
	if !execOpts.AllowWinStderr() && r.IsWindows() {
		return &windowsWaiter{waiter, execOpts.WroteErr}, nil
	}
	log.Trace(ctx, "returning waiter", log.HostAttr(r), log.KeyCommand, cmd)
	return waiter, nil
}

// StartBackground starts the command and returns a Waiter.
func (r *Executor) StartBackground(command string, opts ...ExecOption) (protocol.Waiter, error) {
	return r.Start(context.Background(), command, opts...)
}

// ExecContext executes the command and returns an error if unsuccessful.
func (r *Executor) ExecContext(ctx context.Context, command string, opts ...ExecOption) error {
	log.Trace(ctx, "execcontext", log.HostAttr(r), log.KeyCommand, command)
	proc, err := r.Start(ctx, command, opts...)
	if err != nil {
		log.Trace(ctx, "execcontext start command failed", log.HostAttr(r), log.KeyCommand, command, log.KeyError, err)
		return fmt.Errorf("start command: %w", err)
	}
	log.Trace(ctx, "execcontext wait for process", log.HostAttr(r), log.KeyCommand, command)
	if err := proc.Wait(); err != nil {
		log.Trace(ctx, "execcontext process finished", log.HostAttr(r), log.KeyCommand, command, log.KeyError, err)
		return fmt.Errorf("command result: %w", err)
	}
	log.Trace(ctx, "execcontext process finished", log.HostAttr(r), log.KeyCommand, command)

	return nil
}

// Exec executes the command and returns an error if unsuccessful.
func (r *Executor) Exec(command string, opts ...ExecOption) error {
	return r.ExecContext(context.Background(), command, opts...)
}

// ExecOutputContext executes the command and returns the stdout output or an error.
func (r *Executor) ExecOutputContext(ctx context.Context, command string, opts ...ExecOption) (string, error) {
	out := &bytes.Buffer{}
	defer out.Reset()

	opts = append(opts, Stdout(out))

	log.Trace(ctx, "starting command for execoutput", log.HostAttr(r), log.KeyCommand, command)
	proc, err := r.Start(ctx, command, opts...)
	if err != nil {
		return "", fmt.Errorf("start command: %w", err)
	}
	log.Trace(ctx, "waiting on command", log.HostAttr(r), log.KeyCommand, command)
	if err := proc.Wait(); err != nil {
		log.Trace(ctx, "waiting returned an error", log.HostAttr(r), log.KeyCommand, command, log.KeyError, err)
		return "", fmt.Errorf("command result: %w", err)
	}

	log.Trace(ctx, "command finished", log.HostAttr(r), log.KeyCommand, command)
	execOpts := Build(opts...)
	return execOpts.FormatOutput(out.String()), nil
}

// ExecOutput executes the command and returns the stdout output or an error.
func (r *Executor) ExecOutput(command string, opts ...ExecOption) (string, error) {
	return r.ExecOutputContext(context.Background(), command, opts...)
}

func (r *Executor) ExecReaderContext(ctx context.Context, command string, opts ...ExecOption) io.Reader {
	pipeR, pipeW := io.Pipe()
	if ctx.Err() != nil {
		log.Trace(ctx, "execreader: returning context error", log.HostAttr(r), log.KeyCommand, command, log.KeyError, ctx.Err())
		pipeW.CloseWithError(fmt.Errorf("context error: %w", ctx.Err()))
		return pipeR
	}
	opts = append(opts, Stdout(pipeW))
	go func() {
		if err := r.ExecContext(ctx, command, opts...); err != nil {
			log.Trace(ctx, "execreader: execcontext returned an error", log.HostAttr(r), log.KeyCommand, command, log.KeyError, err)
			pipeW.CloseWithError(fmt.Errorf("exec reader: %w", err))
		} else {
			pipeW.CloseWithError(io.EOF)
		}
		log.Trace(ctx, "execreader: execcontext finished", log.HostAttr(r), log.KeyCommand, command)
	}()
	return pipeR
}

func (r *Executor) ExecScannerContext(ctx context.Context, command string, opts ...ExecOption) *bufio.Scanner {
	return bufio.NewScanner(r.ExecReaderContext(ctx, command, opts...))
}

func (r *Executor) ExecReader(command string, opts ...ExecOption) io.Reader {
	return r.ExecReaderContext(context.Background(), command, opts...)
}

func (r *Executor) ExecScanner(command string, opts ...ExecOption) *bufio.Scanner {
	return r.ExecScannerContext(context.Background(), command, opts...)
}

// StartProcess calls the connection's StartProcess method. This is done to satisfy the
// connection interface and thus allow chaining of runners.
func (r *Executor) StartProcess(ctx context.Context, command string, stdin io.Reader, stdout io.Writer, stderr io.Writer) (protocol.Waiter, error) {
	waiter, err := r.connection.StartProcess(ctx, r.Command(command), stdin, stdout, stderr)
	if err != nil {
		return nil, fmt.Errorf("runner start process: %w", err)
	}
	return waiter, nil
}
