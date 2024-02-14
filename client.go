// Package rig provides an easy way to add multi-protocol connectivity and
// multi-os operation support to your application's Host objects
package rig

import (
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/k0sproject/rig/abort"
	"github.com/k0sproject/rig/initsystem"
	"github.com/k0sproject/rig/os"
	"github.com/k0sproject/rig/packagemanager"
	"github.com/k0sproject/rig/remotefs"
)

// Client is a struct you can embed into your application's "Host" types
// to give them multi-protocol connectivity.
type Client struct {
	*Dependencies

	once sync.Once
	sudo *Client
}

// ErrNotInitialized is returned when a Connection is used without being properly initialized
var ErrNotInitialized = errors.New("connection not properly initialized")

// DefaultClient is a Connection that is especially suitable for embedding into something that is unmarshalled from YAML.
type DefaultClient struct {
	ConnectionConfig CompositeConfig `yaml:",inline"`
	*Client          `yaml:"-"`
}

// Setup allows applying options to the connection to configure subcomponents
func (c *DefaultClient) Setup(opts ...Option) error {
	client, err := c.ConnectionConfig.Connection()
	if err != nil {
		return fmt.Errorf("get client: %w", err)
	}
	opts = append(opts, WithConnection(client))
	connection, err := NewConnection(opts...)
	if err != nil {
		return fmt.Errorf("new connection: %w", err)
	}
	c.Client = connection
	return nil
}

// Connect to the host.
func (c *DefaultClient) Connect(opts ...Option) error {
	if c.Client == nil {
		if err := c.Setup(opts...); err != nil {
			return err
		}
	}
	return c.Client.Connect()
}

// UnmarshalYAML unmarshals and setups a DefaultConnection from YAML
func (c *DefaultClient) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type configuredConnection DefaultClient
	conn := (*configuredConnection)(c)
	if err := unmarshal(conn); err != nil {
		return fmt.Errorf("unmarshal client config: %w", err)
	}
	return c.Setup()
}

// NewConnection returns a new Connection object with the given options
func NewConnection(opts ...Option) (*Client, error) {
	conn := &Client{}
	if err := conn.setup(opts...); err != nil {
		return nil, err
	}
	return conn, nil
}

func (c *Client) setup(opts ...Option) error {
	var err error
	c.once.Do(func() {
		options := NewOptions(opts...)
		c.Dependencies = options.ConnectionDependencies()
		err = c.initClient()
	})
	if err != nil {
		return fmt.Errorf("init client: %w", err)
	}
	return nil
}

// Service returns a Service object for the named service using the host's init system
func (c *Client) Service(name string) (*Service, error) {
	is, err := c.InitSystem()
	if err != nil {
		return nil, err
	}
	return &Service{runner: c.Sudo(), initsys: is, name: name}, nil
}

// String returns a printable representation of the connection, which will look
// like: `[ssh] address:port`
func (c *Client) String() string {
	if c.client == nil {
		if c.connectionConfigurer == nil {
			return "[uninitialized connection]"
		}
		return c.connectionConfigurer.String()
	}

	return c.client.String()
}

// Clone returns a copy of the connection with the given options.
func (c *Client) Clone(opts ...Option) *Client {
	return &Client{
		Dependencies: c.Dependencies.Clone(opts...),
	}
}

// Sudo returns a copy of the connection with a Runner that uses sudo.
func (c *Client) Sudo() *Client {
	if c.sudo == nil {
		c.sudo = c.Clone(WithRunner(c.sudoRunner()))
	}
	return c.sudo
}

// FS returns a fs.FS compatible filesystem interface for accessing files on remote hosts
func (c *Client) FS() remotefs.FS {
	fs, err := c.getFS()
	if err != nil {
		// TODO: maybe this needs to be setup in the constructor because getting an error here is very inconvenient for the user
		return nil // get a null panic. this does not actually happen since getFS never returns an error, need some rethink
	}
	return fs
}

// Connect to the host.
func (c *Client) Connect() error {
	if c.client == nil {
		return errors.Join(abort.ErrAbort, ErrNotInitialized)
	}
	if conn, ok := c.client.(connector); ok {
		if err := conn.Connect(); err != nil {
			return fmt.Errorf("client connect: %w", err)
		}
	}

	return nil
}

// Disconnect from the host.
func (c *Dependencies) Disconnect() {
	if c.client == nil {
		return
	}
	if conn, ok := c.client.(disconnector); ok {
		conn.Disconnect()
	}
}

// ExecInteractive runs a command interactively on the host if supported by the client implementation.
func (c *Client) ExecInteractive(cmd string, stdin io.Reader, stdout, stderr io.Writer) error {
	if conn, ok := c.client.(interactiveExecer); ok {
		if err := conn.ExecInteractive(cmd, stdin, stdout, stderr); err != nil {
			return fmt.Errorf("exec interactive: %w", err)
		}
		return nil
	}
	return fmt.Errorf("can't start an interactive session: %w", ErrNotSupported)
}

// InitSystem returns a ServiceManager for the host's init system
func (c *Client) InitSystem() (initsystem.ServiceManager, error) {
	return c.getInitSystem()
}

// PackageManager returns a PackageManager for the host's package manager
func (c *Client) PackageManager() (packagemanager.PackageManager, error) {
	return c.getPackageManager()
}

// OS returns the host's operating system
func (c *Client) OS() (*os.Release, error) {
	return c.getOS()
}

// Protocol returns the protocol used to connect to the host
func (c *Client) Protocol() string {
	if c.client == nil {
		return "unknown"
	}
	return c.client.Protocol()
}

// Address returns the address of the host
func (c *Client) Address() string {
	if c.client == nil {
		return ""
	}
	return c.client.IPAddress()
}
