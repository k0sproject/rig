// Package localhost provides a rig protocol implementation to the local host using the os/exec package.
package localhost

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"github.com/k0sproject/rig/v2/protocol"
	"github.com/k0sproject/rig/v2/sh/shellescape"
)

const copyWaitDelay = 5 * time.Second

// Connection is a direct localhost connection.
type Connection struct{}

// Connection returns the connection itself. This is because there's no config for localhost connections.
func (c *Connection) Connection() (protocol.Connection, error) {
	return c, nil
}

// NewConnection creates a new Localhost connection. Error is currently always nil.
func NewConnection() (*Connection, error) {
	return &Connection{}, nil
}

// Protocol returns the protocol family, "Local".
func (c *Connection) Protocol() string {
	return "Local"
}

// ProtocolName returns the implementation name, "Local".
func (c *Connection) ProtocolName() string {
	return "Local"
}

// IPAddress returns the connection address.
func (c *Connection) IPAddress() string {
	return "127.0.0.1"
}

// String returns the connection's printable name.
func (c *Connection) String() string {
	return "localhost"
}

// IsWindows is true when running on a windows host.
func (c *Connection) IsWindows() bool {
	return runtime.GOOS == "windows"
}

// IsConnected always returns true for localhost — there is no connection to lose.
func (c *Connection) IsConnected() bool {
	return true
}

// StartProcess executes a command on the remote host and uses the passed in streams for stdin, stdout and stderr. It returns a Waiter with a .Wait() function that
// blocks until the command finishes and returns an error if the exit code is not zero.
func (c *Connection) StartProcess(ctx context.Context, cmd string, stdin io.Reader, stdout, stderr io.Writer) (protocol.Waiter, error) {
	command := c.command(ctx, cmd)

	command.Stdin = stdin
	command.Stdout = stdout
	command.Stderr = stderr

	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("start command: %w", err)
	}

	return command, nil
}

func (c *Connection) command(ctx context.Context, cmd string) *exec.Cmd {
	if c.IsWindows() {
		return exec.CommandContext(ctx, "cmd.exe", "/c", cmd)
	}

	return exec.CommandContext(ctx, "sh", "-c", "--", cmd)
}

// ExecInteractive executes a command on the host and passes stdin/stdout/stderr as-is to the session.
// The process is killed when ctx is cancelled. Nil streams default to os.Stdin/os.Stdout/os.Stderr.
func (c *Connection) ExecInteractive(ctx context.Context, cmd string, stdin io.Reader, stdout, stderr io.Writer) error { //nolint:cyclop
	if stdin == nil {
		stdin = os.Stdin
	}
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}
	if cmd == "" {
		cmd = os.Getenv("SHELL") + " -l"
	}

	if cmd == " -l" {
		cmd = "cmd"
	}

	parts, err := shellescape.Split(cmd)
	if err != nil {
		return fmt.Errorf("failed to parse command: %w", err)
	}
	if len(parts) == 0 || parts[0] == "" {
		return fmt.Errorf("%w: empty command", protocol.ErrValidationFailed)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current working directory: %w", err)
	}

	// try to cast the streams to files, if they are not files, use pipes
	pipes := &stdioPipes{}
	defer pipes.closeChildEnds()
	stdinR, ok := stdin.(*os.File)
	if !ok {
		if stdinR, err = pipes.in(stdin); err != nil {
			return err
		}
	}
	stdoutW, ok := stdout.(*os.File)
	if !ok {
		if stdoutW, err = pipes.out(stdout); err != nil {
			return err
		}
	}
	stderrW, ok := stderr.(*os.File)
	if !ok {
		if stderrW, err = pipes.out(stderr); err != nil {
			return err
		}
	}

	procAttr := &os.ProcAttr{
		Files: []*os.File{stdinR, stdoutW, stderrW},
		Dir:   cwd,
	}

	proc, err := os.StartProcess(parts[0], parts[1:], procAttr) //nolint:gosec // G702: intentional command execution from parsed arguments
	pipes.closeChildEnds()
	if err != nil {
		return fmt.Errorf("failed to start process: %w", err)
	}
	defer pipes.wait()

	// Kill the process when the context is done, but also stop watching when
	// the function returns normally so that the goroutine does not leak.
	watchDone := make(chan struct{})
	defer close(watchDone)
	go func() {
		select {
		case <-ctx.Done():
			_ = proc.Kill()
		case <-watchDone:
		}
	}()

	if _, err := proc.Wait(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err() //nolint:wrapcheck // context error is the real cause
		}
		return fmt.Errorf("process wait: %w", err)
	}
	return nil
}

// stdioPipes bridges non-file streams to a child process through os pipes.
type stdioPipes struct {
	childEnds []*os.File
	readers   []*os.File
	copies    sync.WaitGroup
}

func (p *stdioPipes) in(src io.Reader) (*os.File, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create pipe: %w", err)
	}
	p.childEnds = append(p.childEnds, r)
	go func() {
		defer w.Close()
		_, _ = io.Copy(w, src)
	}()
	return r, nil
}

func (p *stdioPipes) out(dst io.Writer) (*os.File, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create pipe: %w", err)
	}
	p.childEnds = append(p.childEnds, w)
	p.readers = append(p.readers, r)
	p.copies.Go(func() {
		defer r.Close()
		_, _ = io.Copy(dst, r)
	})
	return w, nil
}

// closeChildEnds closes the parent's copies of the child's pipe ends so the
// copiers see EOF once the child exits.
func (p *stdioPipes) closeChildEnds() {
	for _, f := range p.childEnds {
		_ = f.Close()
	}
	p.childEnds = nil
}

// wait waits for the output copiers to drain, bounded by copyWaitDelay
// because a daemonized grandchild can hold the pipe open indefinitely.
func (p *stdioPipes) wait() {
	done := make(chan struct{})
	go func() {
		p.copies.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(copyWaitDelay):
		for _, r := range p.readers {
			_ = r.Close()
		}
	}
}
