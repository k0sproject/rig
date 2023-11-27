package rig

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/log"
	ps "github.com/k0sproject/rig/powershell"
	"github.com/masterzen/winrm"
	"github.com/mitchellh/go-homedir"
)

// WinRM describes a WinRM connection with its configuration options
type WinRM struct {
	Address       string `yaml:"address" validate:"required,hostname|ip"`
	User          string `yaml:"user" validate:"omitempty,gt=2" default:"Administrator"`
	Port          int    `yaml:"port" default:"5985" validate:"gt=0,lte=65535"`
	Password      string `yaml:"password,omitempty"`
	UseHTTPS      bool   `yaml:"useHTTPS" default:"false"`
	Insecure      bool   `yaml:"insecure" default:"false"`
	UseNTLM       bool   `yaml:"useNTLM" default:"false"`
	CACertPath    string `yaml:"caCertPath,omitempty" validate:"omitempty,file"`
	CertPath      string `yaml:"certPath,omitempty" validate:"omitempty,file"`
	KeyPath       string `yaml:"keyPath,omitempty" validate:"omitempty,file"`
	TLSServerName string `yaml:"tlsServerName,omitempty" validate:"omitempty,hostname|ip"`
	Bastion       *SSH   `yaml:"bastion,omitempty"`

	name string

	caCert []byte
	key    []byte
	cert   []byte

	client *winrm.Client
}

// SetDefaults sets various default values
func (c *WinRM) SetDefaults() {
	if p, err := homedir.Expand(c.CACertPath); err == nil {
		c.CACertPath = p
	}

	if p, err := homedir.Expand(c.CertPath); err == nil {
		c.CertPath = p
	}

	if p, err := homedir.Expand(c.KeyPath); err == nil {
		c.KeyPath = p
	}

	if c.Port == 5985 && c.UseHTTPS {
		c.Port = 5986
	}
}

// Protocol returns the protocol name, "WinRM"
func (c *WinRM) Protocol() string {
	return "WinRM"
}

// IPAddress returns the connection address
func (c *WinRM) IPAddress() string {
	return c.Address
}

// String returns the connection's printable name
func (c *WinRM) String() string {
	if c.name == "" {
		c.name = fmt.Sprintf("[winrm] %s:%d", c.Address, c.Port)
	}

	return c.name
}

// IsConnected returns true if the client is connected
func (c *WinRM) IsConnected() bool {
	return c.client != nil
}

// IsWindows always returns true on winrm
func (c *WinRM) IsWindows() bool {
	return true
}

func (c *WinRM) loadCertificates() error {
	c.caCert = nil
	if c.CACertPath != "" {
		ca, err := os.ReadFile(c.CACertPath)
		if err != nil {
			return ErrInvalidPath.Wrapf("load ca cert: %w", err)
		}
		c.caCert = ca
	}

	c.cert = nil
	if c.CertPath != "" {
		cert, err := os.ReadFile(c.CertPath)
		if err != nil {
			return ErrInvalidPath.Wrapf("load cert: %w", err)
		}
		c.cert = cert
	}

	c.key = nil
	if c.KeyPath != "" {
		key, err := os.ReadFile(c.KeyPath)
		if err != nil {
			return ErrInvalidPath.Wrapf("load key: %w", err)
		}
		c.key = key
	}

	return nil
}

// Connect opens the WinRM connection
func (c *WinRM) Connect() error {
	if err := c.loadCertificates(); err != nil {
		return ErrCantConnect.Wrapf("failed to load certificates: %w", err)
	}

	endpoint := &winrm.Endpoint{
		Host:          c.Address,
		Port:          c.Port,
		HTTPS:         c.UseHTTPS,
		Insecure:      c.Insecure,
		TLSServerName: c.TLSServerName,
		Timeout:       time.Minute,
	}

	if len(c.caCert) > 0 {
		endpoint.CACert = c.caCert
	}

	if len(c.cert) > 0 {
		endpoint.Cert = c.cert
	}

	if len(c.key) > 0 {
		endpoint.Key = c.key
	}

	params := winrm.DefaultParameters

	if c.Bastion != nil {
		err := c.Bastion.Connect()
		if err != nil {
			return fmt.Errorf("bastion connect: %w", err)
		}
		params.Dial = c.Bastion.client.Dial
	}

	if c.UseNTLM {
		params.TransportDecorator = func() winrm.Transporter { return &winrm.ClientNTLM{} }
	}

	if c.UseHTTPS && len(c.cert) > 0 {
		params.TransportDecorator = func() winrm.Transporter { return &winrm.ClientAuthRequest{} }
	}

	client, err := winrm.NewClientWithParameters(endpoint, c.User, c.Password, params)
	if err != nil {
		return fmt.Errorf("create winrm client: %w", err)
	}

	log.Debugf("%s: testing connection", c)
	_, err = client.RunWithContext(context.Background(), "echo ok", io.Discard, io.Discard)
	if err != nil {
		return fmt.Errorf("test connection: %w", err)
	}
	log.Debugf("%s: test passed", c)

	c.client = client

	return nil
}

// Disconnect closes the WinRM connection
func (c *WinRM) Disconnect() {
	c.client = nil
}

// Exec executes a command on the host
func (c *WinRM) Exec(cmd string, opts ...exec.Option) error { //nolint:funlen,cyclop
	execOpts := exec.Build(opts...)
	shell, err := c.client.CreateShell()
	if err != nil {
		return fmt.Errorf("create shell: %w", err)
	}
	defer shell.Close()

	execOpts.LogCmd(c.String(), cmd)

	command, err := shell.ExecuteWithContext(context.Background(), cmd)
	if err != nil {
		return fmt.Errorf("execute command: %w", err)
	}

	var wg sync.WaitGroup

	if execOpts.Stdin != "" {
		execOpts.LogStdin(c.String())
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer command.Stdin.Close()
			_, _ = command.Stdin.Write([]byte(execOpts.Stdin))
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		if execOpts.Writer == nil {
			outputScanner := bufio.NewScanner(command.Stdout)

			for outputScanner.Scan() {
				execOpts.AddOutput(c.String(), outputScanner.Text()+"\n", "")
			}

			if err := outputScanner.Err(); err != nil {
				execOpts.LogErrorf("%s: %s", c, err.Error())
			}
			command.Stdout.Close()
		} else {
			if _, err := io.Copy(execOpts.Writer, command.Stdout); err != nil {
				execOpts.LogErrorf("%s: failed to stream stdout: %v", c, err)
			}
		}
	}()

	gotErrors := false

	wg.Add(1)
	go func() {
		defer wg.Done()
		outputScanner := bufio.NewScanner(command.Stderr)

		for outputScanner.Scan() {
			gotErrors = true
			execOpts.AddOutput(c.String(), "", outputScanner.Text()+"\n")
		}

		if err := outputScanner.Err(); err != nil {
			gotErrors = true
			execOpts.LogErrorf("%s: %s", c, err.Error())
		}
		command.Stderr.Close()
	}()

	command.Wait()

	wg.Wait()

	if ec := command.ExitCode(); ec > 0 {
		return ErrCommandFailed.Wrapf("non-zero exit code %d", ec)
	}
	if !execOpts.AllowWinStderr && gotErrors {
		return ErrCommandFailed.Wrapf("received data in stderr")
	}

	return nil
}

// ExecInteractive executes a command on the host and copies stdin/stdout/stderr from local host
func (c *WinRM) ExecInteractive(cmd string) error {
	if cmd == "" {
		cmd = "cmd"
	}
	_, err := c.client.RunWithContextWithInput(context.Background(), cmd, os.Stdout, os.Stderr, os.Stdin)
	if err != nil {
		return fmt.Errorf("execute command interactive: %w", err)
	}
	return nil
}

// Upload uploads a file from local src path to remote path dst
func (c *WinRM) Upload(src, dst string, opts ...exec.Option) error { //nolint:funlen,gocognit,cyclop
	var err error
	defer func() {
		if err != nil {
			_ = c.Exec(fmt.Sprintf(`del %s`, ps.DoubleQuote(dst)), opts...)
		}
	}()
	psCmd := ps.UploadCmd(dst)
	stat, err := os.Stat(src)
	if err != nil {
		return ErrInvalidPath.Wrapf("stat source file: %w", err)
	}
	sha256DigestLocalObj := sha256.New()
	sha256DigestLocal := ""
	sha256DigestRemote := ""
	srcSize := uint64(stat.Size())
	var bytesSent uint64
	var realSent uint64
	var fdClosed bool
	srcFd, err := os.Open(src)
	if err != nil {
		return ErrInvalidPath.Wrapf("open source file: %w", err)
	}
	defer func() {
		if !fdClosed {
			_ = srcFd.Close()
			fdClosed = true
		}
	}()
	shell, err := c.client.CreateShell()
	if err != nil {
		return fmt.Errorf("create shell: %w", err)
	}
	defer shell.Close()
	execOpts := exec.Build(opts...)
	upcmd, err := execOpts.Command("powershell -ExecutionPolicy Unrestricted -EncodedCommand " + psCmd)
	if err != nil {
		return fmt.Errorf("build command: %w", err)
	}

	cmd, err := shell.ExecuteWithContext(context.Background(), upcmd)
	if err != nil {
		return fmt.Errorf("execute command: %w", err)
	}

	// Create a dummy request to get its length
	dummy := winrm.NewSendInputRequest("dummydummydummy", "dummydummydummy", "dummydummydummy", []byte(""), false, winrm.DefaultParameters)
	maxInput := len(dummy.String()) - 100                                       //nolint:gomnd
	bufferCapacity := (winrm.DefaultParameters.EnvelopeSize - maxInput) / 4 * 3 //nolint:gomnd
	base64LineBufferCapacity := bufferCapacity/3*4 + 2                          //nolint:gomnd
	base64LineBuffer := make([]byte, base64LineBufferCapacity)
	base64LineBuffer[base64LineBufferCapacity-2] = '\r'
	base64LineBuffer[base64LineBufferCapacity-1] = '\n'
	buffer := make([]byte, bufferCapacity)
	var bufferLength int

	var ended bool

	for {
		var n int
		n, err = srcFd.Read(buffer)
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
			b, err := cmd.Stdin.Write(base64LineBuffer)
			realSent += uint64(b)
			if ended {
				cmd.Stdin.Close()
			}

			bufferLength = 0
			if err != nil {
				return fmt.Errorf("write to remote stdin: %w", err)
			}
		}
	}
	_ = srcFd.Close()
	fdClosed = true
	if errors.Is(err, io.EOF) {
		err = nil
	}
	if err != nil {
		cmd.Close()
		return fmt.Errorf("write buffer loop: %w", err)
	}
	if !ended {
		_, _ = sha256DigestLocalObj.Write(buffer[:bufferLength])
		sha256DigestLocal = hex.EncodeToString(sha256DigestLocalObj.Sum(nil))
		base64.StdEncoding.Encode(base64LineBuffer, buffer[:bufferLength])
		i := base64.StdEncoding.EncodedLen(bufferLength)
		base64LineBuffer[i] = '\r'
		base64LineBuffer[i+1] = '\n'
		_, err = cmd.Stdin.Write(base64LineBuffer[:i+2])
		if err != nil {
			if !strings.Contains(err.Error(), ps.PipeHasEnded) && !strings.Contains(err.Error(), ps.PipeIsBeingClosed) {
				cmd.Close()
				return fmt.Errorf("write to remote stdin: %w", err)
			}
			// ignore pipe errors that results from passing true to cmd.SendInput
		}
		cmd.Stdin.Close()
	}
	var wg sync.WaitGroup
	wg.Add(2) //nolint:gomnd
	var stderr bytes.Buffer
	var stdout bytes.Buffer
	go func() {
		defer wg.Done()
		_, err = io.Copy(&stderr, cmd.Stderr)
		if err != nil {
			stderr.Reset()
		}
	}()
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(cmd.Stdout)
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
	cmd.Wait()
	wg.Wait()

	if cmd.ExitCode() != 0 {
		return ErrCommandFailed.Wrapf("non-zero exit code %d during upload", cmd.ExitCode())
	}
	if sha256DigestRemote == "" {
		return ErrChecksumMismatch.Wrapf("unexpected empty checksum for target file")
	} else if sha256DigestRemote != sha256DigestLocal {
		return ErrChecksumMismatch.Wrapf("upload file checksum mismatch (local = %s, remote = %s)", sha256DigestLocal, sha256DigestRemote)
	}

	return nil
}
