// Package rig provides an easy way to add multi-protocol connectivity and
// multi-os operation support to your application's Host objects
package rig

import (
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/k0sproject/rig/abort"
	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/initsystem"
	"github.com/k0sproject/rig/log"
	"github.com/k0sproject/rig/os"
	"github.com/k0sproject/rig/packagemanager"
	"github.com/k0sproject/rig/protocol"
	"github.com/k0sproject/rig/remotefs"
)

// Client is a struct you can embed into your application's "Host" types
// to give them multi-protocol connectivity or use directly. The Client
// provides a consistent interface to the host's init system, package
// manager, filesystem, and more, regardless of the protocol used to
// connect to the host. The Client also provides a consistent interface
// to the host's operating system's basic functions in a similar fasion
// as the stdlib's os package does for the local system, regardless
// of the protocol used to connect and the remote operating system.
// The client also contains multiple methods for running commands on the
// remote host, see exec.Runner for more.
type Client struct {
	options *Options

	connectionConfigurer ConnectionConfigurer
	connection           protocol.Connection
	once                 sync.Once
	mu                   sync.Mutex
	initErr              error

	exec.Runner `yaml:"-"`

	log.LoggerInjectable `yaml:"-"`

	*PackageManagerService `yaml:"-"`
	*InitSystemService     `yaml:"-"`
	*RemoteFSService       `yaml:"-"`
	*OSReleaseService      `yaml:"-"`
	*SudoService           `yaml:"-"`

	sudoOnce  sync.Once
	sudoClone *Client
}

// ErrNotInitialized is returned when a Connection is used without being properly initialized.
var ErrNotInitialized = errors.New("connection not properly initialized")

// DefaultClient is a Connection that is especially suitable for embedding into something that is unmarshalled from YAML.
type DefaultClient struct {
	ConnectionConfig CompositeConfig `yaml:",inline"`
	*Client          `yaml:"-"`
}

// Setup allows applying options to the connection to configure subcomponents.
func (c *DefaultClient) Setup(opts ...Option) error {
	client, err := c.ConnectionConfig.Connection()
	if err != nil {
		return fmt.Errorf("get client: %w", err)
	}
	opts = append(opts, WithConnection(client))
	connection, err := NewClient(opts...)
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

// UnmarshalYAML unmarshals and setups a DefaultConnection from YAML.
func (c *DefaultClient) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type configuredConnection DefaultClient
	conn := (*configuredConnection)(c)
	if err := unmarshal(conn); err != nil {
		return fmt.Errorf("unmarshal client config: %w", err)
	}
	return c.Setup()
}

// NewClient returns a new Connection object with the given options.
func NewClient(opts ...Option) (*Client, error) {
	conn := &Client{options: NewOptions(opts...)}
	if err := conn.setup(opts...); err != nil {
		return nil, err
	}
	return conn, nil
}

func (c *Client) setupConnection() error {
	conn, err := c.options.GetConnection()
	if err != nil {
		return fmt.Errorf("get connection: %w", err)
	}
	c.connection = conn
	return nil
}

func (c *Client) setup(opts ...Option) error {
	c.once.Do(func() {
		c.options.Apply(opts...)
		c.initErr = c.setupConnection()
		if c.initErr != nil {
			return
		}
		c.Runner = c.options.GetRunner(c.connection)
		c.SudoService = c.options.GetSudoService(c)
		c.InitSystemService = c.options.GetInitSystemService(c)
		c.RemoteFSService = c.options.GetRemoteFSService(c)
		c.PackageManagerService = c.options.GetPackageManagerService(c)
	})
	return c.initErr
}

// Service returns a Service object for the named service using the host's init system.
func (c *Client) Service(name string) (*Service, error) {
	is, err := c.InitSystemService.GetServiceManager()
	if err != nil {
		return nil, err
	}
	runner, err := c.SudoService.GetSudoRunner()
	if err != nil {
		return nil, err
	}
	return &Service{runner: runner, initsys: is, name: name}, nil
}

// String returns a printable representation of the connection, which will look
// like: `[ssh] address:port`
func (c *Client) String() string {
	if c.connection == nil {
		if c.connectionConfigurer == nil {
			return "[uninitialized connection]"
		}
		return c.connectionConfigurer.String()
	}

	return c.connection.String()
}

// Clone returns a copy of the connection with the given options.
func (c *Client) Clone(opts ...Option) *Client {
	options := c.options.Clone()
	options.Apply(opts...)
	return &Client{
		options: options,
	}
}

// Sudo returns a copy of the connection with a Runner that uses sudo.
func (c *Client) Sudo() *Client {
	c.sudoOnce.Do(func() {
		c.sudoClone = c.Clone(
			WithRunner(c.SudoService.SudoRunner()),
			WithConnection(c.connection),
		)
	})
	return c.sudoClone
}

// Connect to the host.
func (c *Client) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connection == nil {
		return errors.Join(abort.ErrAbort, ErrNotInitialized)
	}
	if conn, ok := c.connection.(protocol.Connector); ok {
		if err := conn.Connect(); err != nil {
			return fmt.Errorf("client connect: %w", err)
		}
	}

	return nil
}

// Disconnect from the host.
func (c *Client) Disconnect() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connection == nil {
		return
	}
	if conn, ok := c.connection.(protocol.Disconnector); ok {
		conn.Disconnect()
	}
}

var errInteractiveNotSupported = errors.New("the connection does not provide interactive exec support")

// ExecInteractive runs a command interactively on the host if supported by the client implementation.
func (c *Client) ExecInteractive(cmd string, stdin io.Reader, stdout, stderr io.Writer) error {
	if conn, ok := c.connection.(protocol.InteractiveExecer); ok {
		if err := conn.ExecInteractive(cmd, stdin, stdout, stderr); err != nil {
			return fmt.Errorf("exec interactive: %w", err)
		}
		return nil
	}
	return errInteractiveNotSupported
}

// FS returns a fs.FS compatible filesystem interface for accessing files on the host.
// If the filesystem can't be accessed, a filesystem that returns an error for all operations is returned.
// If you need to handle the error, you can use client.RemoteFSService.GetFS() (FS, error) directly.
func (c *Client) FS() remotefs.FS {
	return c.RemoteFSService.FS()
}

// InitSystem returns a ServiceManager for the host's init system.
// If the init system can't be determined, a ServiceManager that returns an error for all operations is returned.
// If you need to handle the error, you can use client.InitSystemService.GetServiceManager() (initsystem.ServiceManager, error) directly.
func (c *Client) InitSystem() initsystem.ServiceManager {
	return c.InitSystemService.ServiceManager()
}

// PackageManager returns a PackageManager for the host's package manager
// If the package manager can't be determined, a PackageManager that returns an error for all operations is returned.
// If you need to handle the error, you can use client.PackageManagerService.GetPackageManager() (packagemanager.PackageManager, error) directly.
func (c *Client) PackageManager() packagemanager.PackageManager {
	return c.PackageManagerService.PackageManager()
}

// OS returns the host's operating system version and release information or an error if it can't be determined.
func (c *Client) OS() (*os.Release, error) {
	return c.OSReleaseService.GetOSRelease()
}

// Protocol returns the protocol used to connect to the host.
func (c *Client) Protocol() string {
	if c.connection == nil {
		return "uninitialized"
	}
	return c.connection.Protocol()
}

// Address returns the address of the host.
func (c *Client) Address() string {
	if c.connection != nil {
		return c.connection.IPAddress()
	}
	return ""
}
