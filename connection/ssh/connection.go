package ssh

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"sync"

	ssh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/terminal"

	"github.com/acarl005/stripansi"
	"github.com/k0sproject/rig/exec"

	"github.com/mitchellh/go-homedir"
)

// Connection describes an SSH connection
type Connection struct {
	Address string `yaml:"address" validate:"required,hostname|ip"`
	User    string `yaml:"user" validate:"omitempty,gt=2" default:"root"`
	Port    int    `yaml:"port" default:"22" validate:"gt=0,lte=65535"`
	KeyPath string `yaml:"keyPath" validate:"omitempty,file" default:"~/.ssh/id_rsa"`

	name string

	isWindows bool
	knowOs    bool
	client    *ssh.Client
}

func (c *Connection) SetDefaults() error {
	k, err := homedir.Expand(c.KeyPath)
	if err != nil {
		return err
	}
	c.KeyPath = k

	return nil
}

// String returns the connection's printable name
func (c *Connection) String() string {
	if c.name == "" {
		c.name = fmt.Sprintf("[ssh] %s:%d", c.Address, c.Port)
	}

	return c.name
}

func (c *Connection) IsConnected() bool {
	return c.client != nil
}

// Disconnect closes the SSH connection
func (c *Connection) Disconnect() {
	c.client.Close()
}

// IsWindows is true when the host is running windows
func (c *Connection) IsWindows() bool {
	if !c.knowOs {
		c.knowOs = true

		c.isWindows = c.Exec("cmd /c exit 0") == nil
	}

	return c.isWindows
}

// Connect opens the SSH connection
func (c *Connection) Connect() error {
	key, err := ioutil.ReadFile(c.KeyPath)
	if err != nil {
		return err
	}

	config := &ssh.ClientConfig{
		User:            c.User,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}
	dst := fmt.Sprintf("%s:%d", c.Address, c.Port)

	sshAgentSock := os.Getenv("SSH_AUTH_SOCK")
	signer, err := ssh.ParsePrivateKey(key)
	if err != nil && sshAgentSock == "" {
		return err
	}
	if err == nil {
		config.Auth = append(config.Auth, ssh.PublicKeys(signer))
	}

	if sshAgentSock != "" {
		sshAgent, err := net.Dial("unix", sshAgentSock)
		if err != nil {
			return fmt.Errorf("cannot connect to SSH agent auth socket %s: %s", sshAgentSock, err)
		}
		config.Auth = append(config.Auth, ssh.PublicKeysCallback(agent.NewClient(sshAgent).Signers))
	}

	client, err := ssh.Dial("tcp", dst, config)
	if err != nil {
		return err
	}
	c.client = client
	return nil
}

// Exec executes a command on the host
func (c *Connection) Exec(cmd string, opts ...exec.Option) error {
	o := exec.Build(opts...)
	session, err := c.client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	if len(o.Stdin) == 0 && c.knowOs && !c.isWindows {
		// Only request a PTY when there's no STDIN data, because
		// then you would need to send a CTRL-D after input to signal
		// the end of text
		modes := ssh.TerminalModes{ssh.ECHO: 0}
		err = session.RequestPty("xterm", 80, 40, modes)
		if err != nil {
			return err
		}
	}

	o.LogCmd(c.String(), cmd)

	stdin, _ := session.StdinPipe()
	stdout, _ := session.StdoutPipe()
	stderr, _ := session.StderrPipe()

	if err := session.Start(cmd); err != nil {
		return err
	}

	if len(o.Stdin) > 0 {
		o.LogStdin(c.String())
		io.WriteString(stdin, o.Stdin)
	}
	stdin.Close()

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		outputScanner := bufio.NewScanner(stdout)

		for outputScanner.Scan() {
			text := outputScanner.Text()
			stripped := stripansi.Strip(text)
			o.AddOutput(c.String(), stripped+"\n")
		}

		if err := outputScanner.Err(); err != nil {
			o.LogErrorf("%s: %s", c, err.Error())
		}
	}()

	gotErrors := false

	wg.Add(1)
	go func() {
		defer wg.Done()
		outputScanner := bufio.NewScanner(stderr)

		for outputScanner.Scan() {
			gotErrors = true
			o.AddOutput(c.String()+" (stderr)", outputScanner.Text()+"\n")
		}

		if err := outputScanner.Err(); err != nil {
			gotErrors = true
			o.LogErrorf("%s: %s", c, err.Error())
		}
	}()

	err = session.Wait()
	wg.Wait()

	if err != nil {
		return err
	}

	if c.knowOs && c.isWindows && (!o.AllowWinStderr && gotErrors) {
		return fmt.Errorf("command failed (received output to stderr on windows)")
	}

	return nil
}

// Upload uploads a larger file to the host.
// Use instead of configurer.WriteFile when it seems appropriate
func (c *Connection) Upload(src, dst string) error {
	if c.IsWindows() {
		return c.uploadWindows(src, dst)
	}
	return c.uploadLinux(src, dst)
}

func termSizeWNCH() []byte {
	size := make([]byte, 16)
	fd := int(os.Stdin.Fd())
	rows, cols, err := terminal.GetSize(fd)
	if err != nil {
		binary.BigEndian.PutUint32(size, 40)
		binary.BigEndian.PutUint32(size[4:], 80)
	} else {
		binary.BigEndian.PutUint32(size, uint32(cols))
		binary.BigEndian.PutUint32(size[4:], uint32(rows))
	}

	return size
}

// ExecInteractive executes a command on the host and copies stdin/stdout/stderr from local host
func (c *Connection) ExecInteractive(cmd string) error {
	session, err := c.client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	session.Stdout = os.Stdout
	session.Stderr = os.Stderr

	fd := int(os.Stdin.Fd())
	old, err := terminal.MakeRaw(fd)
	if err != nil {
		return err
	}

	defer terminal.Restore(fd, old)

	rows, cols, err := terminal.GetSize(fd)
	if err != nil {
		return err
	}

	modes := ssh.TerminalModes{ssh.ECHO: 1}
	err = session.RequestPty("xterm", cols, rows, modes)
	if err != nil {
		return err
	}

	stdinpipe, err := session.StdinPipe()
	if err != nil {
		return err
	}
	go func() {
		io.Copy(stdinpipe, os.Stdin)
	}()

	c.captureSignals(stdinpipe, session)

	if cmd == "" {
		err = session.Shell()
	} else {
		err = session.Start(cmd)
	}

	if err != nil {
		return err
	}

	return session.Wait()
}
