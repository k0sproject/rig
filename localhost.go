package rig

import (
	"bufio"
	"io"
	"os"
	osexec "os/exec"
	"os/user"
	"runtime"
	"strings"
	"sync"

	"github.com/k0sproject/rig/exec"
	"github.com/kballard/go-shellquote"
)

const name = "[local] localhost"

// Localhost is a direct localhost connection
type Localhost struct {
	Enabled bool `yaml:"enabled" validate:"required,eq=true" default:"true"`

	cansudo bool
	user    string
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
	if !c.IsWindows() && c.Exec("sudo -n true") == nil {
		c.cansudo = true
	}
	if user, err := user.Current(); err == nil {
		c.user = user.Username
	}
	return nil
}

// Disconnect on local connection does nothing
func (c *Localhost) Disconnect() {}

// Exec executes a command on the host
func (c *Localhost) Exec(cmd string, opts ...exec.Option) error {
	o := exec.Build(opts...)
	command := c.command(cmd)

	if o.Stdin != "" {
		o.LogStdin(name)

		command.Stdin = strings.NewReader(o.Stdin)
	}

	stdout, err := command.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		return err
	}

	o.LogCmd(name, cmd)

	if err := command.Start(); err != nil {
		return err
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		outputScanner := bufio.NewScanner(stdout)

		for outputScanner.Scan() {
			o.AddOutput(name, outputScanner.Text()+"\n", "")
		}

		wg.Done()
	}()
	wg.Add(1)
	go func() {
		outputScanner := bufio.NewScanner(stderr)

		for outputScanner.Scan() {
			o.AddOutput(name, "", outputScanner.Text()+"\n")
		}

		wg.Done()
	}()

	err = command.Wait()
	wg.Wait()
	return err
}

func (c *Localhost) command(cmd string) *osexec.Cmd {
	if c.IsWindows() {
		return osexec.Command(cmd)
	}

	if c.cansudo && c.user != "" {
		return osexec.Command("sudo", "-n", "-s", "--", "su", "-l", "-c", cmd, c.user)
	}

	return osexec.Command("bash", "-c", "--", cmd)
}

// Upload copies a larger file to another path on the host.
func (c *Localhost) Upload(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
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
		return err
	}

	pa := os.ProcAttr{
		Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
		Dir:   cwd,
	}

	parts, err := shellquote.Split(cmd)
	if err != nil {
		return err
	}

	proc, err := os.StartProcess(parts[0], parts[1:], &pa)
	if err != nil {
		return err
	}

	_, err = proc.Wait()
	println("shell exited")
	return err
}
