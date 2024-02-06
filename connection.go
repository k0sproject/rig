// Package rig provides an easy way to add multi-protocol connectivity and
// multi-os operation support to your application's Host objects
package rig

import (
	"context"
	"fmt"
	"io"

	"github.com/creasty/defaults"
	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/log"
	"github.com/k0sproject/rig/rigfs"
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
	*ConnectionInjectables `yaml:",inline"`

	sudo *Connection
}

// DefaultConnectionInjectables can be overridden to provide a different set of protocols than the default
var DefaultClientConfigurer = func() ClientConfigurer {
	return &ClientConfig{}
}

func (c *Connection) UnmarshalYAML(unmarshal func(interface{}) error) error {
	c.ConnectionInjectables = DefaultConnectionInjectables()

	if c.ClientConfigurer == nil {
		c.ClientConfigurer = DefaultClientConfigurer()
	}

	type connection Connection
	if err := unmarshal((*connection)(c)); err != nil {
		return err
	}

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
		if c.ClientConfigurer == nil {
			return "[uninitialized connection]"
		}
		return c.ClientConfigurer.String()
	}

	return c.client.String()
}

func (c *Connection) Clone(opts ...Option) *Connection {
	return &Connection{
		ConnectionInjectables: c.ConnectionInjectables.Clone(opts...),
	}
}

func (c *Connection) Sudo() *Connection {
	if c.sudo == nil {
		c.sudo = c.Clone(WithRunner(c.sudoRunner()))
	}
	return c.sudo
}

// Fsys returns a fs.FS compatible filesystem interface for accessing files on remote hosts
func (c *Connection) Fsys() rigfs.Fsys {
	if c.fsys == nil {
		c.fsys = rigfs.NewFsys(c)
	}

	return c.fsys
}

// Connect to the host and identify the operating system and sudo capability
func (c *Connection) Connect() error {
	if c.client == nil {
		if err := defaults.Set(c); err != nil {
			return fmt.Errorf("%w: set defaults: %w", ErrValidationFailed, err)
		}
	}

	if err := c.client.Connect(); err != nil {
		c.client = nil
		log.Debugf("%s: failed to connect: %v", c, err)
		return fmt.Errorf("%w: client connect: %w", ErrNotConnected, err)
	}

	c.Runner = exec.NewHostRunner(c.client)

	return nil
}

// Disconnect from the host
func (c *Connection) Disconnect() {
	if c.client != nil {
		c.client.Disconnect()
	}
	c.client = nil
}
