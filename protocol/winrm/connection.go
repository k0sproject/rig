// Package winrm provides a rig protocol implementation for WinRM connections
package winrm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/k0sproject/rig/v2/log"
	"github.com/k0sproject/rig/v2/protocol"
	"github.com/k0sproject/rig/v2/protocol/ssh"
	"github.com/k0sproject/rig/v2/protocol/ssh/hostkey"
	"github.com/masterzen/winrm"
)

var (
	errExitCode       = errors.New("command exited with a non-zero exit code")
	errNotConnected   = errors.New("not connected")
	errInvalidCommand = errors.New("invalid command")
)

// Connection describes a Connection connection with its configuration options.
type Connection struct {
	log.LoggerInjectable `yaml:"-"`
	Config               `yaml:",inline"`

	name string

	caCert []byte
	key    []byte
	cert   []byte

	client *winrm.Client
}

type dialFunc func(network, addr string) (net.Conn, error)

// NewConnection creates a new WinRM connection. Error is currently always nil.
func NewConnection(cfg Config, opts ...Option) (*Connection, error) {
	options := NewOptions(opts...)
	options.InjectLoggerTo(cfg, log.KeyProtocol, "winrm-config")
	if err := cfg.SetDefaults(); err != nil {
		return nil, fmt.Errorf("set winrm config defaults: %w", err)
	}

	c := &Connection{Config: cfg}
	options.InjectLoggerTo(c, log.KeyProtocol, "winrm")

	return c, nil
}

// Protocol returns the protocol name, "WinRM".
func (c *Connection) Protocol() string {
	return "WinRM"
}

// IPAddress returns the connection address.
func (c *Connection) IPAddress() string {
	return c.Address
}

// String returns the connection's printable name.
func (c *Connection) String() string {
	if c.name == "" {
		c.name = net.JoinHostPort(c.Address, strconv.Itoa(c.Port))
	}

	return c.name
}

// IsWindows always returns true on winrm.
func (c *Connection) IsWindows() bool {
	return true
}

func (c *Connection) loadCertificates() error {
	c.caCert = nil
	if c.CACertPath != "" {
		ca, err := os.ReadFile(c.CACertPath)
		if err != nil {
			return fmt.Errorf("load ca-cerrt %s: %w", c.CACertPath, err)
		}
		c.caCert = ca
	}

	c.cert = nil
	if c.CertPath != "" {
		cert, err := os.ReadFile(c.CertPath)
		if err != nil {
			return fmt.Errorf("load cert %s: %w", c.CertPath, err)
		}
		c.cert = cert
	}

	c.key = nil
	if c.KeyPath != "" {
		key, err := os.ReadFile(c.KeyPath)
		if err != nil {
			return fmt.Errorf("load key %s: %w", key, err)
		}
		c.key = key
	}

	return nil
}

func (c *Connection) bastionDialer() (dialFunc, error) {
	bastion, err := c.Bastion.Connection()
	if err != nil {
		return nil, fmt.Errorf("create bastion connection: %w", err)
	}
	bastionSSH, ok := bastion.(*ssh.Connection)
	if !ok {
		return nil, fmt.Errorf("%w: bastion connection is not an SSH connection", protocol.ErrAbort)
	}
	log.Trace(context.Background(), "connecting to bastion", log.KeyHost, c)
	if err := bastionSSH.Connect(); err != nil {
		if errors.Is(err, hostkey.ErrHostKeyMismatch) {
			return nil, fmt.Errorf("%w: bastion connect: %w", protocol.ErrAbort, err)
		}
		return nil, fmt.Errorf("bastion connect: %w", err)
	}
	return bastionSSH.Dial, nil
}

// Connect opens the WinRM connection.
func (c *Connection) Connect() error {
	if err := c.loadCertificates(); err != nil {
		return fmt.Errorf("%w: failed to load certificates: %w", protocol.ErrAbort, err)
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
		dialer, err := c.bastionDialer()
		if err != nil {
			return err
		}
		params.Dial = dialer
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

	c.client = client

	return nil
}

// Disconnect closes the WinRM connection.
func (c *Connection) Disconnect() {
	c.client = nil
}

type command struct {
	sh  *winrm.Shell
	cmd *winrm.Command
	wg  sync.WaitGroup
	log log.Logger
}

// Wait blocks until the command finishes.
func (c *command) Wait() error {
	defer func() {
		if r := recover(); r != nil {
			if strings.Contains(fmt.Sprint(r), "close of closed channel") {
				c.log.Debug("recovered from a panic in command.Wait", "reason", r)
			} else {
				panic(r)
			}
		}
	}()

	defer c.sh.Close()
	defer c.cmd.Close()

	c.wg.Wait()
	log.Trace(context.Background(), "waitgroup finished")
	c.cmd.Wait()
	log.Trace(context.Background(), "command finished", log.KeyExitCode, c.cmd.ExitCode())

	if c.cmd.ExitCode() != 0 {
		return fmt.Errorf("%w: exit code %d", errExitCode, c.cmd.ExitCode())
	}

	return nil
}

// Close terminates the command.
func (c *command) Close() error {
	if err := c.cmd.Close(); err != nil {
		return fmt.Errorf("close command: %w", err)
	}
	return nil
}

// StartProcess executes a command on the remote host and uses the passed in streams for stdin, stdout and stderr. It returns a Waiter with a .Wait() function that
// blocks until the command finishes and returns an error if the exit code is not zero.
func (c *Connection) StartProcess(ctx context.Context, cmd string, stdin io.Reader, stdout, stderr io.Writer) (protocol.Waiter, error) {
	if c.client == nil {
		return nil, errNotConnected
	}
	if len(cmd) > 8191 {
		return nil, fmt.Errorf("%w: %w: command too long (%d/%d)", protocol.ErrAbort, errInvalidCommand, len(cmd), 8191)
	}

	shell, err := c.client.CreateShell()
	if err != nil {
		return nil, fmt.Errorf("create shell: %w", err)
	}
	proc, err := shell.ExecuteWithContext(ctx, cmd)
	if err != nil {
		return nil, fmt.Errorf("execute command: %w", err)
	}
	started := time.Now()
	res := &command{sh: shell, cmd: proc, log: c.Log()}
	if stdin == nil {
		proc.Stdin.Close()
	} else {
		res.wg.Add(1)
		go func() {
			defer res.wg.Done()
			n, err := io.Copy(proc.Stdin, stdin)
			if err != nil {
				log.Trace(ctx, "copying data to command stdin failed", log.KeyError, err)
				return
			}
			log.Trace(ctx, "finished copying data to command stdin", log.KeyBytes, n)
		}()
	}
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	res.wg.Add(2)
	go func() {
		defer res.wg.Done()
		n, err := io.Copy(stdout, proc.Stdout)
		if err != nil {
			log.Trace(ctx, "copying data from command stdout failed", log.KeyDuration, time.Since(started), log.KeyError, err)
			return
		}
		log.Trace(ctx, "finished copying data from stdout", log.KeyBytes, n)
	}()
	go func() {
		defer res.wg.Done()
		n, err := io.Copy(stderr, proc.Stderr)
		if err != nil {
			log.Trace(ctx, "copying data from command stderr failed", log.KeyDuration, time.Since(started), log.KeyError, err)
			return
		}
		log.Trace(ctx, "finished copying data from stderr", log.KeyBytes, n)
	}()
	return res, nil
}

// ExecInteractive executes a command on the host and passes stdin/stdout/stderr as-is to the session.
func (c *Connection) ExecInteractive(cmd string, stdin io.Reader, stdout, stderr io.Writer) error {
	if cmd == "" {
		cmd = "cmd.exe"
	}
	_, err := c.client.RunWithContextWithInput(context.Background(), cmd, stdout, stderr, stdin)
	if err != nil {
		return fmt.Errorf("execute command in interactive mode: %w", err)
	}
	return nil
}
