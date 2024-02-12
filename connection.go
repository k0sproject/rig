// Package rig provides an easy way to add multi-protocol connectivity and
// multi-os operation support to your application's Host objects
package rig

import (
	"fmt"
	"io"

	"github.com/k0sproject/rig/initsystem"
	"github.com/k0sproject/rig/os"
	"github.com/k0sproject/rig/packagemanager"
	"github.com/k0sproject/rig/remotefs"
)

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
	conn := &Connection{}
	if err := conn.Setup(opts...); err != nil {
		return nil, err
	}
	return conn, nil
}

// Setup the connection with the given options
func (c *Connection) Setup(opts ...Option) error {
	options := NewOptions(opts...)
	deps := options.ConnectionDependencies()
	if err := deps.initClient(); err != nil {
		return fmt.Errorf("init client: %w", err)
	}
	c.Dependencies = deps
	return nil
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
	fs, err := c.getFS()
	if err != nil {
		// TODO: maybe this needs to be setup in the constructor because getting an error here is very inconvenient for the user
		return nil // get a null panic. this does not actually happen since getFS never returns an error, need some rethink
	}
	return fs
}

// Connect to the host.
func (c *Connection) Connect() error {
	if err := c.initClient(); err != nil {
		return fmt.Errorf("init client: %w", err)
	}
	if conn, ok := c.client.(Connector); ok {
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
	if conn, ok := c.client.(Disconnector); ok {
		conn.Disconnect()
	}
}

// ExecInteractive runs a command interactively on the host if supported by the client implementation.
func (c *Connection) ExecInteractive(cmd string, stdin io.Reader, stdout, stderr io.Writer) error {
	if conn, ok := c.client.(InteractiveExecer); ok {
		if err := conn.ExecInteractive(cmd, stdin, stdout, stderr); err != nil {
			return fmt.Errorf("exec interactive: %w", err)
		}
		return nil
	}
	return fmt.Errorf("can't start an interactive session: %w", ErrNotSupported)
}

// InitSystem returns a ServiceManager for the host's init system
func (c *Connection) InitSystem() (initsystem.ServiceManager, error) {
	return c.getInitSystem()
}

// PackageManager returns a PackageManager for the host's package manager
func (c *Connection) PackageManager() (packagemanager.PackageManager, error) {
	return c.getPackageManager()
}

// OS returns the host's operating system
func (c *Connection) OS() (*os.Release, error) {
	return c.getOS()
}
