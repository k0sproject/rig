package ssh

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"sync"

	ssh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/terminal"

	"github.com/acarl005/stripansi"
	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/util"
	log "github.com/sirupsen/logrus"
)

// Connection describes an SSH connection
type Connection struct {
	Address string
	User    string
	Port    int
	KeyPath string

	name string

	isWindows bool
	knowOs    bool
	client    *ssh.Client
}

// SetName sets the connection's printable name
func (c *Connection) SetName(n string) {
	c.name = n
}

// String returns the connection's printable name
func (c *Connection) String() string {
	if c.name == "" {
		return fmt.Sprintf("%s:%d", c.Address, c.Port)
	}

	return c.name
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
	key, err := util.LoadExternalFile(c.KeyPath)
	if err != nil {
		return err
	}

	config := &ssh.ClientConfig{
		User:            c.User,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}
	address := fmt.Sprintf("%s:%d", c.Address, c.Port)

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
		log.Tracef("using SSH auth sock %s", sshAgentSock)
		config.Auth = append(config.Auth, ssh.PublicKeysCallback(agent.NewClient(sshAgent).Signers))
	}

	client, err := ssh.Dial("tcp", address, config)
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
		log.Tracef("%s: stdout loop exited", c)
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
		log.Tracef("%s: stderr loop exited", c)
	}()

	log.Tracef("%s: waiting for command exit", c)
	err = session.Wait()
	log.Tracef("%s: waiting for syncgroup done", c)
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
		log.Tracef("error getting window size: %s", err.Error())
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

	log.Tracef("requesting pty")
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
		log.Tracef("stdin closed")
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
