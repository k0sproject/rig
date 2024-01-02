package rig

import (
	"bufio"
	"fmt"
	"io"
	"os"
	osexec "os/exec"
	"runtime"
	"strings"
	"sync"

	"github.com/k0sproject/rig/exec"
	"github.com/mattn/go-shellwords"
)

const name = "[local] localhost"

// Localhost is a direct localhost connection
type Localhost struct {
	Enabled bool `yaml:"enabled" validate:"required,eq=true" default:"true"`
}

// Protocol returns the protocol name, "Local"
func (c *Localhost) Protocol() string {
	return "Local"
}

// IPAddress returns the connection address
func (c *Localhost) IPAddress() string {
	return "127.0.0.1"
}

// String returns the connection's printable name
func (c *Localhost) String() string {
	return name
}

// IsConnected for local connections is always true
func (c *Localhost) IsConnected() bool {
	return true
}

// IsWindows is true when running on a windows host
func (c *Localhost) IsWindows() bool {
	return runtime.GOOS == "windows"
}

// Connect on local connection does nothing
func (c *Localhost) Connect() error {
	return nil
}

// Disconnect on local connection does nothing
func (c *Localhost) Disconnect() {}

// ExecStreams executes a command on the remote host and uses the passed in streams for stdin, stdout and stderr. It returns a Waiter with a .Wait() function that
// blocks until the command finishes and returns an error if the exit code is not zero.
func (c *Localhost) ExecStreams(cmd string, stdin io.ReadCloser, stdout, stderr io.Writer, opts ...exec.Option) (exec.Waiter, error) {
	execOpts := exec.Build(opts...)
	command, err := c.command(cmd, execOpts)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to build command: %w", ErrCommandFailed, err)
	}

	command.Stdin = stdin
	command.Stdout = stdout
	command.Stderr = stderr

	execOpts.LogCmd(name, cmd)

	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("%w: failed to start: %w", ErrCommandFailed, err)
	}

	return command, nil
}

// Exec executes a command on the host
func (c *Localhost) Exec(cmd string, opts ...exec.Option) error { //nolint:cyclop
	execOpts := exec.Build(opts...)
	command, err := c.command(cmd, execOpts)
	if err != nil {
		return err
	}

	if execOpts.Stdin != "" {
		execOpts.LogStdin(name)

		command.Stdin = strings.NewReader(execOpts.Stdin)
	}

	stdout, err := command.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdout pipe: %w", err)
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to get stderr pipe: %w", err)
	}

	execOpts.LogCmd(name, cmd)

	if err := command.Start(); err != nil {
		return fmt.Errorf("failed to start command: %w", err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()

		if execOpts.Writer == nil {
			outputScanner := bufio.NewScanner(stdout)

			for outputScanner.Scan() {
				execOpts.AddOutput(name, outputScanner.Text()+"\n", "")
			}
			if err := outputScanner.Err(); err != nil {
				execOpts.LogErrorf("%s: failed to scan stdout: %v", c, err)
			}
		} else {
			if _, err := io.Copy(execOpts.Writer, stdout); err != nil {
				execOpts.LogErrorf("%s: failed to stream stdout: %v", c, err)
			}
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()

		outputScanner := bufio.NewScanner(stderr)

		for outputScanner.Scan() {
			execOpts.AddOutput(name, "", outputScanner.Text()+"\n")
		}
		if err := outputScanner.Err(); err != nil {
			execOpts.LogErrorf("%s: failed to scan stderr: %v", c, err)
		}
	}()

	wg.Wait()
	err = command.Wait()
	if err != nil {
		return fmt.Errorf("command wait: %w", err)
	}
	return nil
}

func (c *Localhost) command(cmd string, o *exec.Options) (*osexec.Cmd, error) {
	cmd, err := o.Command(cmd)
	if err != nil {
		return nil, fmt.Errorf("build command: %w", err)
	}

	if c.IsWindows() {
		return osexec.Command("cmd.exe", "/c", cmd), nil
	}

	return osexec.Command("sh", "-c", "--", cmd), nil
}

// ExecInteractive executes a command on the host and copies stdin/stdout/stderr from local host
func (c *Localhost) ExecInteractive(cmd string) error {
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

	pa := os.ProcAttr{
		Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
		Dir:   cwd,
	}

	parts, err := shellwords.Parse(cmd)
	if err != nil {
		return fmt.Errorf("failed to parse command: %w", err)
	}

	proc, err := os.StartProcess(parts[0], parts[1:], &pa)
	if err != nil {
		return fmt.Errorf("failed to start process: %w", err)
	}

	if _, err := proc.Wait(); err != nil {
		return fmt.Errorf("process wait: %w", err)
	}
	return nil
}
