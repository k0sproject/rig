package winrm

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"os"
	"sync"
	"time"

	"github.com/k0sproject/rig/exec"
	log "github.com/sirupsen/logrus"

	"github.com/masterzen/winrm"
)

// Connection describes a WinRM connection with its configuration options
type Connection struct {
	Address       string
	User          string
	Port          int
	Password      string
	UseHTTPS      bool
	Insecure      bool
	UseNTLM       bool
	CACertPath    string
	CertPath      string
	KeyPath       string
	TLSServerName string

	name string

	caCert []byte
	key    []byte
	cert   []byte
	client *winrm.Client
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

// IsWindows is here to satisfy the interface, WinRM hosts are expected to always run windows
func (c *Connection) IsWindows() bool {
	return true
}

func (c *Connection) loadCertificates() error {
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
func (c *Connection) Connect() error {
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
func (c *Connection) Disconnect() {
	c.client = nil
}

// Exec executes a command on the host
func (c *Connection) Exec(cmd string, opts ...exec.Option) error {
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
				log.Errorf("failed to send command stdin: %s", err.Error())
			}
			log.Tracef("%s: input loop exited", c)
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
		log.Tracef("%s: stdout loop exited", c)
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
		log.Tracef("%s: stderr loop exited", c)
	}()

	log.Tracef("%s: waiting for command exit", c)

	command.Wait()
	log.Tracef("%s: command exited", c)

	log.Tracef("%s: waiting for syncgroup done", c)
	wg.Wait()
	log.Tracef("%s: syncgroup done", c)

	err = command.Close()
	if err != nil {
		log.Warnf("%s: %s", c, err.Error())
	}

	if command.ExitCode() > 0 || (!o.AllowWinStderr && gotErrors) {
		return fmt.Errorf("command failed (received output to stderr on windows)")
	}

	return nil
}

// ExecInteractive executes a command on the host and copies stdin/stdout/stderr from local host
func (c *Connection) ExecInteractive(cmd string) error {
	if cmd == "" {
		cmd = "cmd"
	}
	_, err := c.client.RunWithInput(cmd, os.Stdout, os.Stderr, os.Stdin)
	return err
}
