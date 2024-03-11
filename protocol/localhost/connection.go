// Package localhost provides a rig protocol implementation to the local host using the os/exec package.
package localhost

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"

	"github.com/k0sproject/rig/v2/protocol"
	"github.com/k0sproject/rig/v2/sh/shellescape"
)

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

// Protocol returns the protocol name, "Local".
func (c *Connection) Protocol() string {
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
func (c *Connection) ExecInteractive(cmd string, stdin io.Reader, stdout, stderr io.Writer) error { //nolint:cyclop
	if cmd == "" {
		cmd = os.Getenv("SHELL") + " -l"
	}

	if cmd == " -l" {
		cmd = "cmd"
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current working directory: %w", err)
	}

	// try to cast the streams to files, if they are not files, use pipes
	var stdinR, stdoutW, stderrW *os.File
	if f, ok := stdin.(*os.File); ok {
		stdinR = f
	} else {
		r, w, err := os.Pipe()
		if err != nil {
			return fmt.Errorf("failed to create pipe: %w", err)
		}
		go func() {
			defer w.Close()
			_, _ = io.Copy(w, stdin)
		}()
		stdinR = r
	}
	if f, ok := stdout.(*os.File); ok {
		stdoutW = f
	} else {
		r, w, err := os.Pipe()
		if err != nil {
			return fmt.Errorf("failed to create pipe: %w", err)
		}
		go func() {
			defer r.Close()
			_, _ = io.Copy(stdout, r)
		}()
		stdoutW = w
	}
	if f, ok := stderr.(*os.File); ok {
		stderrW = f
	} else {
		r, w, err := os.Pipe()
		if err != nil {
			return fmt.Errorf("failed to create pipe: %w", err)
		}
		go func() {
			defer r.Close()
			_, _ = io.Copy(stderr, r)
		}()
		stderrW = w
	}

	procAttr := &os.ProcAttr{
		Files: []*os.File{stdinR, stdoutW, stderrW},
		Dir:   cwd,
	}

	parts, err := shellescape.Split(cmd)
	if err != nil {
		return fmt.Errorf("failed to parse command: %w", err)
	}

	proc, err := os.StartProcess(parts[0], parts[1:], procAttr)
	if err != nil {
		return fmt.Errorf("failed to start process: %w", err)
	}

	if _, err := proc.Wait(); err != nil {
		return fmt.Errorf("process wait: %w", err)
	}
	return nil
}
