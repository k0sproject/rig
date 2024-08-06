package rig

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/log"
	"github.com/masterzen/winrm"
	"github.com/mitchellh/go-homedir"
)

// WinRM describes a WinRM connection with its configuration options
type WinRM struct {
	Address       string `yaml:"address" validate:"required,hostname_rfc1123|ip"`
	User          string `yaml:"user" validate:"omitempty,gt=2" default:"Administrator"`
	Port          int    `yaml:"port" default:"5985" validate:"gt=0,lte=65535"`
	Password      string `yaml:"password,omitempty"`
	UseHTTPS      bool   `yaml:"useHTTPS" default:"false"`
	Insecure      bool   `yaml:"insecure" default:"false"`
	UseNTLM       bool   `yaml:"useNTLM" default:"false"`
	CACertPath    string `yaml:"caCertPath,omitempty" validate:"omitempty,file"`
	CertPath      string `yaml:"certPath,omitempty" validate:"omitempty,file"`
	KeyPath       string `yaml:"keyPath,omitempty" validate:"omitempty,file"`
	TLSServerName string `yaml:"tlsServerName,omitempty" validate:"omitempty,hostname_rfc1123|ip"`
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
			return fmt.Errorf("%w: load ca-cerrt %s: %w", ErrInvalidPath, c.CACertPath, err)
		}
		c.caCert = ca
	}

	c.cert = nil
	if c.CertPath != "" {
		cert, err := os.ReadFile(c.CertPath)
		if err != nil {
			return fmt.Errorf("%w: load cert %s: %w", ErrInvalidPath, c.CertPath, err)
		}
		c.cert = cert
	}

	c.key = nil
	if c.KeyPath != "" {
		key, err := os.ReadFile(c.KeyPath)
		if err != nil {
			return fmt.Errorf("%w: load key %s: %w", ErrInvalidPath, key, err)
		}
		c.key = key
	}

	return nil
}

// Connect opens the WinRM connection
func (c *WinRM) Connect() error {
	if err := c.loadCertificates(); err != nil {
		return fmt.Errorf("%w: failed to load certificates: %w", ErrCantConnect, err)
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

// Command implements the Waiter interface
type Command struct {
	sh     *winrm.Shell
	cmd    *winrm.Command
	stdin  io.ReadCloser
	stdout io.Writer
	stderr io.Writer
	wg     sync.WaitGroup
}

// Wait blocks until the command finishes
func (c *Command) Wait() (err error) { //nolint:nonamedreturns // needed for panic recovery
	defer func() {
		if r := recover(); err == nil && r != nil {
			if strings.Contains(fmt.Sprint(r), "close of closed channel") {
				log.Debugf("recovered from a panic in Command.Wait: %v", r)
			} else {
				panic(r)
			}
		}
	}()

	defer c.sh.Close()
	defer c.cmd.Close()

	c.wg.Wait()
	c.cmd.Wait()
	log.Debugf("command finished")
	if c.cmd.ExitCode() != 0 {
		err = fmt.Errorf("%w: exit code %d", ErrCommandFailed, c.cmd.ExitCode())
	}
	return err
}

// Close terminates the command
func (c *Command) Close() error {
	if err := c.cmd.Close(); err != nil {
		return fmt.Errorf("close command: %w", err)
	}
	return nil
}

// ExecStreams executes a command on the remote host and uses the passed in streams for stdin, stdout and stderr. It returns a Waiter with a .Wait() function that
// blocks until the command finishes and returns an error if the exit code is not zero.
func (c *WinRM) ExecStreams(cmd string, stdin io.ReadCloser, stdout, stderr io.Writer, opts ...exec.Option) (exec.Waiter, error) {
	if c.client == nil {
		return nil, ErrNotConnected
	}
	execOpts := exec.Build(opts...)
	command, err := execOpts.Command(cmd)
	if err != nil {
		return nil, fmt.Errorf("%w: build command: %w", ErrCommandFailed, err)
	}
	if len(command) > 8191 {
		return nil, fmt.Errorf("%w: command too long (%d/%d)", ErrCommandFailed, len(command), 8191)
	}

	execOpts.LogCmd(c.String(), cmd)
	shell, err := c.client.CreateShell()
	if err != nil {
		return nil, fmt.Errorf("%w: create shell: %w", ErrCantConnect, err)
	}
	proc, err := shell.ExecuteWithContext(context.Background(), command)
	if err != nil {
		return nil, fmt.Errorf("%w: execute command: %w", ErrCommandFailed, err)
	}
	res := &Command{sh: shell, cmd: proc, stdin: stdin, stdout: stdout, stderr: stderr}
	if res.stdin == nil {
		proc.Stdin.Close()
	} else {
		res.wg.Add(1)
		go func() {
			defer res.wg.Done()
			log.Debugf("copying data to command stdin")
			n, err := io.Copy(res.cmd.Stdin, res.stdin)
			if err != nil {
				log.Errorf("copying data to command stdin failed: %v", err)
			}
			log.Debugf("finished copying %d bytes to stdin", n)
		}()
	}
	res.wg.Add(2)
	started := time.Now()
	go func() {
		defer res.wg.Done()
		log.Debugf("copying data from command stdout")
		n, err := io.Copy(res.stdout, res.cmd.Stdout)
		if err != nil {
			log.Errorf("copying data from command stdout failed after %s: %v", time.Since(started), err)
		}
		log.Debugf("finished copying %d bytes from stdout", n)
	}()
	go func() {
		defer res.wg.Done()
		log.Debugf("copying data from command stderr")
		n, err := io.Copy(res.stderr, res.cmd.Stderr)
		if err != nil {
			log.Errorf("copying data from command stderr failed after %s: %v", time.Since(started), err)
		}
		log.Debugf("finished copying %d bytes from stderr", n)
	}()
	return res, nil
}

// Exec executes a command on the host
func (c *WinRM) Exec(cmd string, opts ...exec.Option) error { //nolint:cyclop
	execOpts := exec.Build(opts...)
	shell, err := c.client.CreateShell()
	if err != nil {
		return fmt.Errorf("%w: create shell: %w", ErrCommandFailed, err)
	}
	defer shell.Close()

	execOpts.LogCmd(c.String(), cmd)

	command, err := shell.ExecuteWithContext(context.Background(), cmd)
	if err != nil {
		return fmt.Errorf("%w: execute command: %w", ErrCommandFailed, err)
	}
	defer command.Close()

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
		// ignore channel close panics
		defer func() {
			if r := recover(); r != nil {
				log.Debugf("recovered from a panic while reading stderr: %v", r)
			}
		}()
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

	var errors []string

	wg.Add(1)
	go func() {
		// ignore channel close panics
		defer func() {
			if r := recover(); r != nil {
				log.Debugf("recovered from a panic while reading stderr: %v", r)
			}
		}()
		defer wg.Done()
		outputScanner := bufio.NewScanner(command.Stderr)

		for outputScanner.Scan() {
			msg := outputScanner.Text()
			if msg != "" {
				errors = append(errors, msg)
				execOpts.LogErrorf("%s: %s", c, msg)
			}
		}

		if err := outputScanner.Err(); err != nil {
			execOpts.LogErrorf("%s: %s", c, err.Error())
		}
		command.Stderr.Close()
	}()

	wg.Wait()
	command.Wait()

	if ec := command.ExitCode(); ec > 0 {
		return fmt.Errorf("%w: non-zero exit code: %d", ErrCommandFailed, ec)
	}
	if !execOpts.AllowWinStderr && len(errors) > 0 {
		return fmt.Errorf("%w: received data in stderr: %s", ErrCommandFailed, strings.Join(errors, "\n"))
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
