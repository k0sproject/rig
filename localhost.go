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

	"github.com/alessio/shellescape"
	"github.com/k0sproject/rig/exec"
	ps "github.com/k0sproject/rig/powershell"
	"github.com/kballard/go-shellquote"
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

// Exec executes a command on the host
func (c *Localhost) Exec(cmd string, opts ...exec.Option) error {
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
	}()

	err = command.Wait()
	wg.Wait()
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

	return osexec.Command("bash", "-c", "--", cmd), nil
}

// Upload copies a larger file to another path on the host.
func (c *Localhost) Upload(src, dst string, opts ...exec.Option) error {
	var remoteErr error
	defer func() {
		if remoteErr != nil {
			if c.IsWindows() {
				_ = c.Exec(fmt.Sprintf(`del %s`, ps.DoubleQuote(dst)))
			} else {
				_ = c.Exec(fmt.Sprintf(`rm -f -- %s`, shellescape.Quote(dst)))
			}
		}
	}()

	inFile, err := os.Open(src)
	if err != nil {
		return ErrInvalidPath.Wrapf("failed to open local file %s: %w", src, err)
	}
	defer inFile.Close()

	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("failed to create destination file %s: %w", dst, err)
	}
	defer out.Close()
	_, err = io.Copy(out, inFile)
	if err != nil {
		return fmt.Errorf("failed to copy local file %s to remote %s: %w", src, dst, err)
	}
	return nil
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

	parts, err := shellquote.Split(cmd)
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
