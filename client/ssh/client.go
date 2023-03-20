package ssh

import (
	"bytes"
	_ "embed"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/signal"
	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

var ErrNotConnected = fmt.Errorf("not connected")

func boolPtr(b bool) *bool {
	return &b
}

type Client struct {
	conn      *ssh.Client
	name      string
	isWindows *bool
}

func NewClient(config *Config) (*Client, error) {
	client := &Client{
		name: "[ssh] " + net.JoinHostPort(config.Address, strconv.Itoa(*config.Port)),
	}

	// TODO: setup connection
	println("foo")

	return client, nil
}

func (c *Client) IsWindows() bool {
	if c.isWindows != nil {
		return *c.isWindows
	}

	serverVersion := strings.ToLower(string(c.conn.ServerVersion()))
	if strings.Contains(serverVersion, "windows") {
		c.isWindows = boolPtr(true)
		return true
	}
	for _, os := range knownPosix {
		if strings.Contains(serverVersion, os) {
			c.isWindows = boolPtr(false)
			return false
		}
	}

	// finally fall back to trying to run ver.exe
	if err := c.execErr("ver.exe"); err == nil {
		c.isWindows = boolPtr(true)
		return true
	}

	return false
}

func (c *Client) Protocol() string {
	return "SSH"
}

// IPAddress returns the connection address
func (c *Client) Address() net.Addr {
	return c.conn.RemoteAddr()
}

func (c *Client) String() string {
	return c.name
}

func (c *Client) Disconnect() error {
	return c.conn.Close()
}

func (c *Client) IsConnected() bool {
	if c.conn == nil || c.conn.Conn == nil {
		return false
	}
	_, _, err := c.conn.Conn.SendRequest("keepalive@rig", true, nil)
	return err == nil
}

func (c *Client) Exec(cmd string, stdin io.Reader, stdout, stderr io.Writer) (exec.Process, error) {
	if c.conn == nil {
		return nil, ErrNotConnected
	}

	session, err := c.conn.NewSession()
	if err != nil {
		return nil, fmt.Errorf("ssh create session: %w", err)
	}

	session.Stdin = stdin
	session.Stdout = stdout
	session.Stderr = stderr

	if err := session.Start(cmd); err != nil {
		return nil, fmt.Errorf("ssh start command: %w", err)
	}

	return session, nil
}

func (c *Client) execErr(cmd string) error {
	var errOut bytes.Buffer
	proc, err := c.Exec(cmd, nil, nil, &errOut)
	if err != nil {
		return fmt.Errorf("ssh exec: %w", err)
	}
	if err := proc.Wait(); err != nil {
		return fmt.Errorf("ssh exec result: %w (%s)", err, errOut.String())
	}
	return nil
}

// ExecInteractive executes a command on the host and copies stdin/stdout/stderr from local host
func (c *Client) ExecInteractive(cmd string, stdin io.Reader, stdout, stderr io.Writer) error {
	session, err := c.conn.NewSession()
	if err != nil {
		return fmt.Errorf("ssh new session: %w", err)
	}
	defer session.Close()

	session.Stdout = stdout
	session.Stderr = stderr

	stdinpipe, err := session.StdinPipe()
	if err != nil {
		return fmt.Errorf("get stdin pipe: %w", err)
	}
	go func() {
		_, _ = io.Copy(stdinpipe, stdin)
	}()

	cancel := signal.Forward(stdinpipe, session)
	defer cancel()

	stdinF, ok := stdin.(*os.File)
	if ok && term.IsTerminal(int(stdinF.Fd())) {
		termFd := int(stdinF.Fd())

		old, err := term.MakeRaw(termFd)
		if err != nil {
			return fmt.Errorf("make local terminal raw: %w", err)
		}

		defer func(fd int, old *term.State) {
			_ = term.Restore(fd, old)
		}(termFd, old)

		rows, cols, err := term.GetSize(termFd)
		if err != nil {
			return fmt.Errorf("get terminal size: %w", err)
		}

		modes := ssh.TerminalModes{ssh.ECHO: 1}
		err = session.RequestPty("xterm", cols, rows, modes)
		if err != nil {
			return fmt.Errorf("request pty: %w", err)
		}
	}

	if cmd == "" {
		err = session.Shell()
	} else {
		err = session.Start(cmd)
	}

	if err != nil {
		return fmt.Errorf("ssh session: %w", err)
	}

	if err := session.Wait(); err != nil {
		return fmt.Errorf("ssh session wait: %w", err)
	}

	return nil
}
