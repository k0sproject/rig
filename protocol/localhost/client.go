package localhost

import (
	"fmt"
	"io"
	"net"
	"os"
	osexec "os/exec"
	"runtime"

	"github.com/google/shlex"
	"github.com/k0sproject/rig/exec"
)

var localAddress = &localAddr{}

type localAddr struct {
	net.TCPAddr
}

func (a *localAddr) String() string {
	return "127.0.0.1:0"
}

type Client struct{}

func (c Client) Address() net.Addr {
	return localAddress
}

func (c Client) String() string {
	return "[local] localhost"
}

func (c Client) Protocol() string {
	return "Local"
}

func (c Client) IsWindows() bool {
	return runtime.GOOS == "windows"
}

// Disconnect on local connection does nothing
func (c Client) Disconnect() error {
	return nil
}

func (c Client) command(cmd string) *osexec.Cmd {
	if c.IsWindows() {
		return osexec.Command("cmd.exe", "/c", cmd)
	}

	return osexec.Command("bash", "-c", "--", cmd)
}

// Exec executes a command on the remote host and uses the passed in streams for stdin, stdout and stderr. It returns a Waiter with a .Wait() function that
// blocks until the command finishes and returns an error if the exit code is not zero.
func (c Client) Exec(cmd string, stdin io.Reader, stdout, stderr io.Writer) (exec.Process, error) {
	command := c.command(cmd)
	command.Stdin = stdin
	command.Stdout = stdout
	command.Stderr = stderr

	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("local exec: %w", err)
	}

	return command, nil
}

func (c Client) loginshell() string {
	if c.IsWindows() {
		return "cmd.exe"
	}

	return "bash -l"
}

func (c Client) ExecInteractive(cmd string, stdin io.Reader, stdout, stderr io.Writer) error {
	if cmd == "" {
		cmd = c.loginshell()
	}

	parts, err := shlex.Split(cmd)
	if err != nil {
		return fmt.Errorf("failed to parse command: %w", err)
	}

	execPath, err := osexec.LookPath(parts[0])
	if err != nil {
		return fmt.Errorf("failed to find executable: %w", err)
	}
	parts[0] = execPath

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current working directory: %w", err)
	}

	files := make([]*os.File, 3)
	for i, f := range []any{stdin, stdout, stderr} {
		if f == nil {
			return fmt.Errorf("stdin, stdout and stderr must be set")
		}
		file, ok := f.(*os.File)
		if !ok {
			return fmt.Errorf("stdin, stdout and stderr must be os.File")
		}
		files[i] = file
	}

	pa := &os.ProcAttr{Files: files, Dir: cwd}

	proc, err := os.StartProcess(parts[0], parts[1:], pa)
	if err != nil {
		return fmt.Errorf("start process: %w", err)
	}

	if _, err := proc.Wait(); err != nil {
		return fmt.Errorf("command failed: %w", err)
	}

	return nil
}
