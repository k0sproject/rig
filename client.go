// Package rig provides an easy way to add multi-protocol connectivity and
// multi-os operation support to your application's Host objects by
// embedding or directly using the Client or Connection objects.
//
// Rig's core functionality revolves around providing a unified interface
// for interacting with remote systems. This includes managing services,
// file systems, package managers, and getting OS release information,
// abstracting away the intricacies of different operating systems and
// communication protocols.
//
// The protocol implementations aim to provide out-of-the-box default
// behavior similar to what you would expect when using the official
// clients like openssh "ssh" command instead of having to deal with
// implementing ssh config parsing, key managemnt, agent forwarding
// and so on yourself.
//
// To get started, see [Client]
package rig

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/k0sproject/rig/v2/cmd"
	"github.com/k0sproject/rig/v2/log"
	"github.com/k0sproject/rig/v2/os"
	"github.com/k0sproject/rig/v2/packagemanager"
	"github.com/k0sproject/rig/v2/protocol"
	"github.com/k0sproject/rig/v2/remotefs"
	"github.com/k0sproject/rig/v2/retry"
)

// Client is a swiss army knife client that can perform actions and run
// commands on target hosts running on multiple operating systems and
// using different protocols for communication.
//
// It provides a consistent interface to the host's init system,
// package manager, file system, and more, regardless of the protocol
// or the remote operating system. It also provides a consistent
// interface to the host's operating system's basic functions in a
// similar manner as the stdlib's os package does for the local system,
// for example chmod, stat, and so on.
//
// The easiest way to set up a client instance is through a protocol
// config struct, like [protocol/ssh.Config]
// or the unified [CompositeConfig] and then use the [NewClient]
// function to create a new client.
type Client struct {
	options *ClientOptions

	connectionConfigurer ConnectionConfigurer
	connection           protocol.Connection
	once                 sync.Once
	mu                   sync.Mutex
	initErr              error

	cmd.Runner

	log.LoggerInjectable

	*PackageManagerService
	*InitSystemService
	*RemoteFSService
	*OSReleaseService
	*SudoService

	sudoOnce  sync.Once
	sudoClone *Client
}

// ClientWithConfig is a [Client] that is suitable for embedding into
// a host object that is unmarshalled from YAML configuration.
//
// When embedded into a "host" object like this:
//
//	type Host struct {
//	  rig.ClientWithConfig `yaml:",inline"`
//	  // ...
//	}
//
// And having a configuration YAML like this:
//
//	hosts:
//	- ssh:
//	  address: 10.0.0.1
//	  user: root
//
// You can unmarshal the configuration and start using the clients on the host objects:
//
//	if err := host.Connect(context.Background()); err != nil {
//	    log.Fatal(err)
//	}
//	out, err := host.ExecOutput("ls")
//
// The available protocols are defined in the [CompositeConfig] struct.
type ClientWithConfig struct {
	mu               sync.Mutex
	ConnectionConfig CompositeConfig `yaml:",inline"`
	*Client          `yaml:"-"`
}

// Setup allows applying options to the connection to configure subcomponents.
func (c *ClientWithConfig) Setup(opts ...ClientOption) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.Client != nil {
		return nil
	}
	opts = append(opts, WithConnectionConfigurer(&c.ConnectionConfig))
	client, err := NewClient(opts...)
	if err != nil {
		return fmt.Errorf("new client: %w", err)
	}
	c.Client = client
	return nil
}

// Connect to the host. Unlike in [Client.Connect], the Connect method here
// accepts a variadic list of options similar to [NewClient]. This is to
// allow configuring the connection before connecting, since you won't
// be calling [NewClient] to create the [ClientWithConfig] instance when
// unmarshalling from a configuration file.
func (c *ClientWithConfig) Connect(ctx context.Context, opts ...ClientOption) error {
	if err := c.Setup(opts...); err != nil { //nolint:contextcheck // it's the trace logger
		return err
	}
	return c.Client.Connect(ctx)
}

// UnmarshalYAML implements the yaml.Unmarshaler interface, it unmarshals and
// sets up a connection from a YAML configuration.
func (c *ClientWithConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type configuredConnection ClientWithConfig
	conn := (*configuredConnection)(c)
	if err := unmarshal(conn); err != nil {
		return fmt.Errorf("unmarshal client config: %w", err)
	}
	return c.Setup()
}

// NewClient returns a new Connection object with the given options.
//
// You must use either WithConnection to provide a pre-configured connection
// or WithConnectionConfigurer to provide a connection configurer.
//
// An example SSH connection via ssh.Config::
//
//	client, err := rig.NewClient(WithConnectionConfigurer(&ssh.Config{Address: "10.0.0.1"}))
//
// Using the [CompositeConfig] struct:
//
//	client, err := rig.NewClient(WithConnectionConfigurer(&rig.CompositeConfig{SSH: &ssh.Config{...}}))
//
// If you want to use a pre-configured connection, you can use WithConnection:
//
//	conn, err := ssh.NewConnection(ssh.Config{...})
//	client, err := rig.NewClient(WithConnection(conn))
//
// Once you have a client, you can use it to interact with the remote host.
//
//	err := client.Connect(context.Background())
//	if err != nil {
//	    log.Fatal(err)
//	}
//	out, err := client.ExecOutput("ls")
//
// To see all of the available ways to run commands, see [cmd.Executor].
func NewClient(opts ...ClientOption) (*Client, error) {
	options := NewClientOptions(opts...)
	if err := options.Validate(); err != nil {
		return nil, fmt.Errorf("validate client options: %w", err)
	}
	conn := &Client{options: options}
	if err := conn.setup(); err != nil {
		return nil, err
	}
	return conn, nil
}

func (c *Client) setupConnection() error {
	conn, err := c.options.GetConnection()
	if err != nil {
		return fmt.Errorf("get connection: %w", err)
	}
	log.Trace(context.Background(), "connection from configurer", log.HostAttr(conn))
	c.connection = conn
	return nil
}

func (c *Client) setup(opts ...ClientOption) error {
	c.once.Do(func() {
		if len(opts) > 0 {
			c.options.Apply(opts...)
		}
		c.initErr = c.setupConnection()
		if c.initErr != nil {
			return
		}

		log.Trace(context.Background(), "client setup", log.HostAttr(c.connection))
		logger := log.GetLogger(c.connection)
		log.Trace(context.Background(), "logger from connection", "is_nil", logger == nil, "is_null", logger == log.Null)
		log.InjectLogger(logger, c)

		c.Runner = c.options.GetRunner(c.connection)
		log.InjectLogger(logger, c.Runner)

		c.SudoService = c.options.GetSudoService(c.Runner)
		c.InitSystemService = c.options.GetInitSystemService(c.Runner)
		c.RemoteFSService = c.options.GetRemoteFSService(c.Runner)
		c.PackageManagerService = c.options.GetPackageManagerService(c.Runner)
	})
	return c.initErr
}

// Service returns a manager for a named service on the remote host using
// the host's init system if one can be detected. This can be used to
// start, stop, restart, and check the status of services.
//
// You most likely need to use this with Sudo:
//
//	service, err := client.Sudo().Service("nginx")
func (c *Client) Service(name string) (*Service, error) {
	is, err := c.InitSystemService.GetServiceManager()
	if err != nil {
		return nil, fmt.Errorf("get service manager: %w", err)
	}
	return &Service{runner: c.Runner, initsys: is, name: name}, nil
}

// String returns a printable representation of the connection, which will usually look
// something like: `address:port` or `user@address:port`.
func (c *Client) String() string {
	if c.connection == nil {
		if c.connectionConfigurer == nil {
			return "[uninitialized connection]"
		}
		return c.connectionConfigurer.String()
	}

	return c.connection.String()
}

// Clone returns a copy of the connection with the given additional options applied.
func (c *Client) Clone(opts ...ClientOption) *Client {
	options := c.options.Clone()
	options.Apply(opts...)
	clone := &Client{
		options: options,
	}
	_ = clone.setup()
	return clone
}

// Sudo returns a copy of the connection with a Runner that uses sudo.
func (c *Client) Sudo() *Client {
	c.sudoOnce.Do(func() {
		c.sudoClone = c.Clone(
			WithRunner(c.SudoService.SudoRunner()),
			WithConnection(c.connection),
			WithLogger(log.WithAttrs(c.Log(), log.KeySudo, true)),
		)
	})
	return c.sudoClone
}

func (c *Client) connect(ctx context.Context) error {
	if conn, ok := c.connection.(protocol.ConnectorWithContext); ok {
		return conn.Connect(ctx) //nolint:wrapcheck // done below
	}
	if conn, ok := c.connection.(protocol.Connector); ok {
		return conn.Connect() //nolint:wrapcheck // done below
	}
	return nil
}

// Connect to the host. The connection is attempted until the context is done or the
// protocol implementation returns an error indicating that the connection can't be
// established by retrying. If a context without a deadline is used, a 10 second
// timeout is used.
func (c *Client) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connection == nil {
		return fmt.Errorf("%w: connection not properly intialized", protocol.ErrAbort)
	}

	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
	}

	if !c.options.ShouldRetry() {
		if err := c.connect(ctx); err != nil {
			return fmt.Errorf("client connect: %w", err)
		}
		return nil
	}

	err := retry.DoWithContext(ctx, func(ctx context.Context) error {
		return c.connect(ctx)
	}, retry.If(
		func(err error) bool { return !errors.Is(err, protocol.ErrAbort) },
	))
	if err != nil {
		return fmt.Errorf("client connect: %w", err)
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

// ExecInteractive executes a command on the host and passes stdin/stdout/stderr as-is to the session.
func (c *Client) ExecInteractive(cmd string, stdin io.Reader, stdout, stderr io.Writer) error {
	if conn, ok := c.connection.(protocol.InteractiveExecer); ok {
		if err := conn.ExecInteractive(cmd, stdin, stdout, stderr); err != nil {
			return fmt.Errorf("exec interactive: %w", err)
		}
		return nil
	}
	return errInteractiveNotSupported
}

// The service Getters would be available and working via the embedding alrady, but the
// accessors are provided here directly on the Client mainly for discoverability in docs.

// FS returns an fs.FS compatible filesystem interface for accessing files on the host.
//
// If the filesystem can't be accessed, a filesystem that returns an error for all operations is returned
// instead. If you need to handle the error, you can use c.RemoteFSService.GetFS() directly.
func (c *Client) FS() remotefs.FS {
	return c.RemoteFSService.FS()
}

// PackageManager for the host's operating system. This can be used to install or remove packages.
//
// If a known package manager can't be detected, a PackageManager that returns an error for all operations is returned.
// If you need to handle the error, you can use client.PackageManagerService.GetPackageManager() (packagemanager.PackageManager, error) directly.
func (c *Client) PackageManager() packagemanager.PackageManager {
	return c.PackageManagerService.PackageManager()
}

// OS returns the host's operating system version and release information or an error if it can't be determined.
func (c *Client) OS() (*os.Release, error) {
	os, err := c.OSReleaseService.GetOSRelease()
	if err != nil {
		return nil, fmt.Errorf("get os release: %w", err)
	}
	return os, nil
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
