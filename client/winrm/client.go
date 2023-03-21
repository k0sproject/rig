package winrm

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"

	"github.com/k0sproject/rig/client"
	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/signal"
	"github.com/masterzen/winrm"
)

var ErrNotConnected = fmt.Errorf("not connected")

type connection interface {
	CreateShell() (shell, error)
}

type shell interface {
	Execute(cmd string) (command, error)
	Close() error
}

type command interface {
	Wait()
	Close() error
	ExitCode() int
}

type Client struct {
	conn *winrm.Client
	name string
	addr Addr
}

type Addr string

func (a Addr) Network() string {
	return "tcp"
}

func (a Addr) String() string {
	return string(a)
}

type winrmShell struct {
	shell *winrm.Shell
}

func (s *winrmShell) Execute(cmd string) (command, error) {
	c, err := s.shell.Execute(cmd)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (s *winrmShell) Close() error {
	return s.shell.Close()
}

type Command struct {
	cmd command
	sh  shell
}

func (c *Command) Wait() error {
	defer func() { _ = c.cmd.Close() }()
	defer c.sh.Close()
	c.cmd.Wait()
	if c.cmd.ExitCode() != 0 {
		return fmt.Errorf("%w: exit code %d", ErrCommandFailed, c.cmd.ExitCode())
	}
	return nil
}

func NewClient(config *Config, opts ...client.Option) (*Client, error) {
	addr := net.JoinHostPort(config.Address, strconv.Itoa(config.Port))
	client := &Client{
		addr: Addr(addr),
		name: "[winrm] " + addr,
	}

	// TODO: setup connection NOTE: winrm.NewClient does not return an error

	return client, nil
}

func (c *Client) IsWindows() bool {
	return true
}

func (c *Client) Protocol() string {
	return "WinRM"
}

// Address returns the connection address
func (c *Client) Address() net.Addr {
	return c.addr
}

func (c *Client) String() string {
	return c.name
}

func (c *Client) Disconnect() error {
	// there's no disconnect mechanism in winrm.Client
	// make sure to check that c.conn isn't nil in other methods
	c.conn = nil

	return nil
}

// Exec executes a command on the remote host and uses the passed in streams for stdin, stdout and stderr.
func (c *Client) Exec(cmd string, stdin io.Reader, stdout, stderr io.Writer) (exec.Process, error) {
	if c.conn == nil {
		return nil, ErrNotConnected
	}

	shell, err := c.conn.CreateShell()
	if err != nil {
		return nil, fmt.Errorf("winrm create shell: %w", err)
	}
	proc, err := shell.ExecuteWithContext(context.Background(), cmd)
	if err != nil {
		return nil, fmt.Errorf("winrm exec: %w", err)
	}
	return &Command{sh: &winrmShell{shell}, cmd: proc}, nil
}

func (c *Client) ExecInteractive(cmd string, stdin io.Reader, stdout, stderr io.Writer) error {
	if cmd == "" {
		cmd = "cmd.exe"
	}
	stdinOut, stdinIn := io.Pipe()

	cancel := signal.Forward(stdinIn, nil)
	defer cancel()

	go func() {
		_, _ = io.Copy(stdinIn, stdin)
		_ = stdinIn.Close()
	}()

	proc, err := c.Exec(cmd, stdinOut, stdout, stderr)
	if err != nil {
		return err
	}

	return proc.Wait()
}
