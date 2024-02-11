// Package rig provides an easy way to add multi-protocol connectivity and
// multi-os operation support to your application's Host objects
package rig

import (
	"context"
	"fmt"
	"io"

	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/initsystem"
	"github.com/k0sproject/rig/packagemanager"
	"github.com/k0sproject/rig/remotefs"
)

// Client is the interface for protocol implementations
type Client interface {
	Connect() error
	Disconnect()
	IsWindows() bool
	StartProcess(ctx context.Context, cmd string, stdin io.Reader, stdout io.Writer, stderr io.Writer) (exec.Waiter, error)
	ExecInteractive(cmd string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error
	String() string
	Protocol() string
	IPAddress() string
	IsConnected() bool
}

// Connection is a Struct you can embed into your application's "Host" types
// to give them multi-protocol connectivity.
//
// All of the important fields have YAML tags.
//
// If you have a host like this:
//
//	type Host struct {
//	  rig.Connection `yaml:"connection"`
//	}
//
// and a YAML like this:
//
//	hosts:
//	  - connection:
//	      ssh:
//	        address: 10.0.0.1
//	        port: 8022
//
// you can then simply do this:
//
//	var hosts []*Host
//	if err := yaml.Unmarshal(data, &hosts); err != nil {
//	  panic(err)
//	}
//	for _, h := range hosts {
//	  err := h.Connect()
//	  if err != nil {
//	    panic(err)
//	  }
//	  output, err := h.ExecOutput("echo hello")
//	}
type Connection struct {
	*Dependencies `yaml:",inline"`

	sudo *Connection
}

// NewConnection returns a new Connection object with the given options
func NewConnection(opts ...Option) (*Connection, error) {
	options := NewOptions(opts...)
	deps := options.ConnectionDependencies()
	if err := deps.initClient(); err != nil {
		return nil, fmt.Errorf("init client: %w", err)
	}
	return &Connection{Dependencies: deps}, nil
}

// DefaultClientConfigurer is a function that returns a new ClientConfigurer. You can override this to provide your own
// as a global default.
var DefaultClientConfigurer = func() ClientConfigurer {
	return &ClientConfig{}
}

// UnmarshalYAML is a custom unmarshaler for the Connection struct
func (c *Connection) UnmarshalYAML(unmarshal func(interface{}) error) error {
	deps := DefaultDependencies()
	if deps.clientConfigurer == nil {
		deps.clientConfigurer = DefaultClientConfigurer()
	}
	configurer := deps.clientConfigurer

	if err := unmarshal(configurer); err != nil {
		return err
	}
	c.Dependencies = deps

	return nil
}

// Service returns a Service object for the named service using the host's init system
func (c *Connection) Service(name string) (*Service, error) {
	is, err := c.InitSystem()
	if err != nil {
		return nil, err
	}
	return &Service{runner: c.Sudo(), initsys: is, name: name}, nil
}

// IsConnected returns true if the client is assumed to be connected.
// "Assumed" - as in `Connect()` has been called and no error was returned.
// The underlying client may actually have disconnected and has become
// inoperable, but rig won't know that until you try to execute commands on
// the connection.
func (c *Connection) IsConnected() bool {
	if c.client == nil {
		return false
	}

	return c.client.IsConnected()
}

// String returns a printable representation of the connection, which will look
// like: `[ssh] address:port`
func (c Connection) String() string {
	if c.client == nil {
		if c.clientConfigurer == nil {
			return "[uninitialized connection]"
		}
		return c.clientConfigurer.String()
	}

	return c.client.String()
}

// Clone returns a copy of the connection with the given options.
func (c *Connection) Clone(opts ...Option) *Connection {
	return &Connection{
		Dependencies: c.Dependencies.Clone(opts...),
	}
}

// Sudo returns a copy of the connection with a Runner that uses sudo.
func (c *Connection) Sudo() *Connection {
	if c.sudo == nil {
		c.sudo = c.Clone(WithRunner(c.sudoRunner()))
	}
	return c.sudo
}

// FS returns a fs.FS compatible filesystem interface for accessing files on remote hosts
func (c *Connection) FS() remotefs.FS {
	return c.getFS()
}

// Connect to the host.
func (c *Connection) Connect() error {
	if err := c.initClient(); err != nil {
		return fmt.Errorf("init client: %w", err)
	}
	if err := c.client.Connect(); err != nil {
		return fmt.Errorf("client connect: %w", err)
	}

	return nil
}

// Disconnect from the host.
func (c *Dependencies) Disconnect() {
	if c.client != nil {
		c.client.Disconnect()
	}
}

// InitSystem returns a ServiceManager for the host's init system
func (c *Dependencies) InitSystem() (initsystem.ServiceManager, error) {
	return c.getInitSystem()
}

// PackageManager returns a PackageManager for the host's package manager
func (c *Dependencies) PackageManager() (packagemanager.PackageManager, error) {
	return c.getPackageManager()
}
