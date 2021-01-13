package winrm

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"os"
	"sync"
	"time"

	"github.com/k0sproject/rig/exec"

	"github.com/masterzen/winrm"
)

// Client describes a WinRM connection with its configuration options
type Client struct {
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

	name string

	caCert []byte
	key    []byte
	cert   []byte

	client *winrm.Client
}

// String returns the connection's printable name
func (c *Client) String() string {
	if c.name == "" {
		c.name = fmt.Sprintf("[winrm] %s:%d", c.Address, c.Port)
	}

	return c.name
}

// IsConnected returns true if the client is connected
func (c *Client) IsConnected() bool {
	return c.client != nil
}

// IsWindows is here to satisfy the interface, WinRM hosts are expected to always run windows
func (c *Client) IsWindows() bool {
	return true
}

func (c *Client) loadCertificates() error {
	c.caCert = nil
	if c.CACertPath != "" {
		ca, err := ioutil.ReadFile(c.CACertPath)
		if err != nil {
			return err
		}
		c.caCert = ca
	}

	c.cert = nil
	if c.CertPath != "" {
		cert, err := ioutil.ReadFile(c.CertPath)
		if err != nil {
			return err
		}
		c.cert = cert
	}

	c.key = nil
	if c.KeyPath != "" {
		key, err := ioutil.ReadFile(c.KeyPath)
		if err != nil {
			return err
		}
		c.key = key
	}

	return nil
}

// Connect opens the WinRM connection
func (c *Client) Connect() error {
	if err := c.loadCertificates(); err != nil {
		return fmt.Errorf("%s: failed to load certificates: %s", c, err)
	}

	endpoint := &winrm.Endpoint{
		Host:          c.Address,
		Port:          c.Port,
		HTTPS:         c.UseHTTPS,
		Insecure:      c.Insecure,
		TLSServerName: c.TLSServerName,
		Timeout:       60 * time.Second,
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

	if c.UseNTLM {
		params.TransportDecorator = func() winrm.Transporter { return &winrm.ClientNTLM{} }
	}

	if c.UseHTTPS && len(c.cert) > 0 {
		params.TransportDecorator = func() winrm.Transporter { return &winrm.ClientAuthRequest{} }
	}

	client, err := winrm.NewClientWithParameters(endpoint, c.User, c.Password, params)

	if err != nil {
		return err
	}

	c.client = client

	return nil
}

// Disconnect closes the WinRM connection
func (c *Client) Disconnect() {
	c.client = nil
}

// Exec executes a command on the host
func (c *Client) Exec(cmd string, opts ...exec.Option) error {
	o := exec.Build(opts...)
	shell, err := c.client.CreateShell()
	if err != nil {
		return err
	}
	defer shell.Close()

	o.LogCmd(c.String(), cmd)

	command, err := shell.Execute(cmd)
	if err != nil {
		return err
	}

	var wg sync.WaitGroup

	if o.Stdin != "" {
		o.LogStdin(c.String())
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer command.Stdin.Close()
			_, err := command.Stdin.Write([]byte(o.Stdin))
			if err != nil {
			}
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		outputScanner := bufio.NewScanner(command.Stdout)

		for outputScanner.Scan() {
			o.AddOutput(c.String(), outputScanner.Text()+"\n")
		}

		if err := outputScanner.Err(); err != nil {
			o.LogErrorf("%s: %s", c, err.Error())
		}
		command.Stdout.Close()
	}()

	gotErrors := false

	wg.Add(1)
	go func() {
		defer wg.Done()
		outputScanner := bufio.NewScanner(command.Stderr)

		for outputScanner.Scan() {
			gotErrors = true
			o.AddOutput(c.String()+" (stderr)", outputScanner.Text()+"\n")
		}

		if err := outputScanner.Err(); err != nil {
			gotErrors = true
			o.LogErrorf("%s: %s", c, err.Error())
		}
		command.Stdout.Close()
	}()

	command.Wait()

	wg.Wait()

	command.Close()

	if command.ExitCode() > 0 || (!o.AllowWinStderr && gotErrors) {
		return fmt.Errorf("command failed (received output to stderr on windows)")
	}

	return nil
}

// ExecInteractive executes a command on the host and copies stdin/stdout/stderr from local host
func (c *Client) ExecInteractive(cmd string) error {
	if cmd == "" {
		cmd = "cmd"
	}
	_, err := c.client.RunWithInput(cmd, os.Stdout, os.Stderr, os.Stdin)
	return err
}
