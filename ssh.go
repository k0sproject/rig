package rig

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"

	ssh "golang.org/x/crypto/ssh"

	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/terminal"

	"github.com/acarl005/stripansi"
	"github.com/alessio/shellescape"
	"github.com/k0sproject/rig/exec"
	ps "github.com/k0sproject/rig/powershell"

	"github.com/mitchellh/go-homedir"
)

// SSH describes an SSH connection
type SSH struct {
	Address string `yaml:"address" validate:"required,hostname|ip"`
	User    string `yaml:"user" validate:"omitempty,gt=2" default:"root"`
	Port    int    `yaml:"port" default:"22" validate:"gt=0,lte=65535"`
	KeyPath string `yaml:"keyPath" validate:"omitempty"`
	HostKey string `yaml:"hostKey,omitempty"`
	Bastion *SSH   `yaml:"bastion,omitempty"`

	name string

	isWindows      bool
	knowOs         bool
	keypathDefault bool

	client *ssh.Client
}

const DefaultKeypath = "~/.ssh/id_rsa"

// SetDefaults sets various default values
func (c *SSH) SetDefaults() {
	if c.KeyPath == "" {
		c.KeyPath = DefaultKeypath
		c.keypathDefault = true
	}
	if k, err := homedir.Expand(c.KeyPath); err == nil {
		c.KeyPath = k
	}
}

// Protocol returns the protocol name, "SSH"
func (c *SSH) Protocol() string {
	return "SSH"
}

// IPAddress returns the connection address
func (c *SSH) IPAddress() string {
	return c.Address
}

// String returns the connection's printable name
func (c *SSH) String() string {
	if c.name == "" {
		c.name = fmt.Sprintf("[ssh] %s:%d", c.Address, c.Port)
	}

	return c.name
}

// IsConnected returns true if the client is connected
func (c *SSH) IsConnected() bool {
	return c.client != nil
}

// Disconnect closes the SSH connection
func (c *SSH) Disconnect() {
	c.client.Close()
}

// IsWindows is true when the host is running windows
func (c *SSH) IsWindows() bool {
	if !c.knowOs && c.client != nil {
		c.isWindows = c.Exec("cmd /c exit 0") == nil
		c.knowOs = true

	}

	return c.isWindows
}

// create human-readable SSH-key strings
func keyString(k ssh.PublicKey) string {
	return k.Type() + " " + base64.StdEncoding.EncodeToString(k.Marshal()) // e.g. "ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTY...."
}

func trustedHostKeyCallback(trustedKey string) ssh.HostKeyCallback {
	return func(_ string, _ net.Addr, k ssh.PublicKey) error {
		ks := keyString(k)
		if trustedKey != ks {
			return fmt.Errorf("SSH host key verification failed")
		}

		return nil
	}
}

// Connect opens the SSH connection
func (c *SSH) Connect() error {
	config := &ssh.ClientConfig{
		User: c.User,
	}

	if c.HostKey == "" {
		config.HostKeyCallback = ssh.InsecureIgnoreHostKey()
	} else {
		config.HostKeyCallback = trustedHostKeyCallback(c.HostKey)
	}

	sshAgentSock := os.Getenv("SSH_AUTH_SOCK")

	if sshAgentSock != "" {
		sshAgent, err := net.Dial("unix", sshAgentSock)
		if err != nil {
			return fmt.Errorf("cannot connect to SSH agent auth socket %s: %s", sshAgentSock, err)
		}
		config.Auth = append(config.Auth, ssh.PublicKeysCallback(agent.NewClient(sshAgent).Signers))
	}

	_, err := os.Stat(c.KeyPath)
	if err != nil && !c.keypathDefault {
		return err
	}
	if err == nil {
		var key []byte
		key, err = os.ReadFile(c.KeyPath)
		if err != nil {
			return err
		}
		signer, err := ssh.ParsePrivateKey(key)
		if err != nil && sshAgentSock == "" {
			return err
		}
		config.Auth = append(config.Auth, ssh.PublicKeys(signer))
	}

	dst := fmt.Sprintf("%s:%d", c.Address, c.Port)

	var client *ssh.Client

	if c.Bastion == nil {
		client, err = ssh.Dial("tcp", dst, config)
		if err != nil {
			return err
		}
	} else {
		if err := c.Bastion.Connect(); err != nil {
			return err
		}
		bconn, err := c.Bastion.client.Dial("tcp", dst)
		if err != nil {
			return err
		}
		c, chans, reqs, err := ssh.NewClientConn(bconn, dst, config)
		if err != nil {
			return err
		}
		client = ssh.NewClient(c, chans, reqs)
	}

	c.client = client
	return nil
}

// Exec executes a command on the host
func (c *SSH) Exec(cmd string, opts ...exec.Option) error {
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
		if _, err := io.WriteString(stdin, o.Stdin); err != nil {
			return err
		}
	}
	stdin.Close()

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		if o.Writer == nil {
			outputScanner := bufio.NewScanner(stdout)

			for outputScanner.Scan() {
				text := outputScanner.Text()
				stripped := stripansi.Strip(text)
				o.AddOutput(c.String(), stripped+"\n", "")
			}

			if err := outputScanner.Err(); err != nil {
				o.LogErrorf("%s: %s", c, err.Error())
			}
		} else {
			if _, err := io.Copy(o.Writer, stdout); err != nil {
				o.LogErrorf("%s: failed to stream stdout", c, err.Error())
			}
		}
	}()

	gotErrors := false

	wg.Add(1)
	go func() {
		defer wg.Done()
		outputScanner := bufio.NewScanner(stderr)

		for outputScanner.Scan() {
			gotErrors = true
			o.AddOutput(c.String(), "", outputScanner.Text()+"\n")
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
func (c *SSH) Upload(src, dst string) error {
	if c.IsWindows() {
		return c.uploadWindows(src, dst)
	}
	return c.uploadLinux(src, dst)
}

// ExecInteractive executes a command on the host and copies stdin/stdout/stderr from local host
func (c *SSH) ExecInteractive(cmd string) error {
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

	defer func(fd int, old *terminal.State) {
		_ = terminal.Restore(fd, old)
	}(fd, old)

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
		_, _ = io.Copy(stdinpipe, os.Stdin)
	}()

	captureSignals(stdinpipe, session)

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

func (c *SSH) uploadLinux(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	var tmpFile string
	if err := c.Exec("mktemp 2> /dev/null", exec.Output(&tmpFile)); err != nil {
		return err
	}
	defer func() { _ = c.Exec(fmt.Sprintf("rm -f %s", shellescape.Quote(tmpFile))) }()
	tmpFile = strings.TrimSpace(tmpFile)

	session, err := c.client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	hostIn, err := session.StdinPipe()
	if err != nil {
		return err
	}

	gw, err := gzip.NewWriterLevel(hostIn, gzip.BestSpeed)
	if err != nil {
		return err
	}

	err = session.Start(fmt.Sprintf(`gzip -d > %s`, shellescape.Quote(tmpFile)))
	if err != nil {
		return err
	}

	if _, err := io.Copy(gw, in); err != nil {
		return err
	}
	gw.Close()
	hostIn.Close()

	if err := session.Wait(); err != nil {
		return err
	}

	return c.Exec(fmt.Sprintf("sudo install -D %s %s", shellescape.Quote(tmpFile), shellescape.Quote(dst)))
}

func (c *SSH) uploadWindows(src, dst string) error {
	psCmd := ps.UploadCmd(dst)
	stat, err := os.Stat(src)
	if err != nil {
		return err
	}
	sha256DigestLocalObj := sha256.New()
	sha256DigestLocal := ""
	sha256DigestRemote := ""
	srcSize := uint64(stat.Size())
	var bytesSent uint64
	var realSent uint64
	var fdClosed bool
	fd, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() {
		if !fdClosed {
			_ = fd.Close()
			fdClosed = true
		}
	}()
	session, err := c.client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	hostIn, err := session.StdinPipe()
	if err != nil {
		return err
	}
	hostOut, err := session.StdoutPipe()
	if err != nil {
		return err
	}
	hostErr, err := session.StderrPipe()
	if err != nil {
		return err
	}

	psRunCmd := "powershell -ExecutionPolicy Unrestricted -EncodedCommand " + psCmd
	if err := session.Start(psRunCmd); err != nil {
		return err
	}

	bufferCapacity := 262143 // use 256kb chunks
	base64LineBufferCapacity := bufferCapacity/3*4 + 2
	base64LineBuffer := make([]byte, base64LineBufferCapacity)
	base64LineBuffer[base64LineBufferCapacity-2] = '\r'
	base64LineBuffer[base64LineBufferCapacity-1] = '\n'
	buffer := make([]byte, bufferCapacity)
	var bufferLength int

	var ended bool

	for {
		var n int
		n, err = fd.Read(buffer)
		bufferLength += n
		if err != nil {
			break
		}
		if bufferLength == bufferCapacity {
			base64.StdEncoding.Encode(base64LineBuffer, buffer)
			bytesSent += uint64(bufferLength)
			_, _ = sha256DigestLocalObj.Write(buffer)
			if bytesSent >= srcSize {
				ended = true
				sha256DigestLocal = hex.EncodeToString(sha256DigestLocalObj.Sum(nil))
			}
			b, err := hostIn.Write(base64LineBuffer)
			realSent += uint64(b)
			if ended {
				hostIn.Close()
			}

			bufferLength = 0
			if err != nil {
				return err
			}
		}
	}
	_ = fd.Close()
	fdClosed = true
	if err == io.EOF {
		err = nil
	}
	if err != nil {
		return err
	}
	if !ended {
		_, _ = sha256DigestLocalObj.Write(buffer[:bufferLength])
		sha256DigestLocal = hex.EncodeToString(sha256DigestLocalObj.Sum(nil))
		base64.StdEncoding.Encode(base64LineBuffer, buffer[:bufferLength])
		i := base64.StdEncoding.EncodedLen(bufferLength)
		base64LineBuffer[i] = '\r'
		base64LineBuffer[i+1] = '\n'
		_, err = hostIn.Write(base64LineBuffer[:i+2])
		if err != nil {
			if !strings.Contains(err.Error(), ps.PipeHasEnded) && !strings.Contains(err.Error(), ps.PipeIsBeingClosed) {
				return err
			}
			// ignore pipe errors that results from passing true to cmd.SendInput
		}
		hostIn.Close()
	}
	var wg sync.WaitGroup
	var stderr bytes.Buffer
	var stdout bytes.Buffer

	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(&stderr, hostErr)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(hostOut)
		for scanner.Scan() {
			var output struct {
				Sha256 string `json:"sha256"`
			}
			if json.Unmarshal(scanner.Bytes(), &output) == nil {
				sha256DigestRemote = output.Sha256
			} else {
				_, _ = stdout.Write(scanner.Bytes())
				_, _ = stdout.WriteString("\n")
			}
		}
		if err := scanner.Err(); err != nil {
			stdout.Reset()
		}
	}()

	if err := session.Wait(); err != nil {
		return err
	}

	wg.Wait()

	if sha256DigestRemote == "" {
		return fmt.Errorf("copy file command did not output the expected JSON to stdout but exited with code 0")
	} else if sha256DigestRemote != sha256DigestLocal {
		return fmt.Errorf("copy file checksum mismatch (local = %s, remote = %s)", sha256DigestLocal, sha256DigestRemote)
	}

	return nil
}
