package rig

import (
	"strings"

	"github.com/creasty/defaults"
	"github.com/k0sproject/rig/client/local"
	"github.com/k0sproject/rig/client/ssh"
	"github.com/k0sproject/rig/client/winrm"
	"github.com/k0sproject/rig/exec"
)

type rigError struct {
	Connection *Connection
}

// NotConnectedError is returned when attempting to perform remote operations on Host when it is not connected
type NotConnectedError rigError

// Error returns the error message
func (e *NotConnectedError) Error() string { return e.Connection.String() + ": not connected" }

// Connection is a Struct you can embed into a host which then can be connected to via winrm, ssh or using the "localhost" connection
type Connection struct {
	WinRM     *winrm.Client `yaml:"winRM,omitempty"`
	SSH       *ssh.Client   `yaml:"ssh,omitempty"`
	Localhost *local.Client `yaml:"localhost,omitempty"`

	client Client `yaml:"-"`
}

// SetDefaults sets a connection
func (c *Connection) SetDefaults() error {
	if c.client == nil {
		c.client = c.configuredClient()
		if c.client == nil {
			c.client = DefaultClient()
		}
	}

	return defaults.Set(c.client)
}

func (c *Connection) IsConnected() bool {
	if c.client == nil {
		return false
	}

	return c.client.IsConnected()
}

// String implements the Stringer interface for logging purposes
func (c *Connection) String() string {
	if !c.IsConnected() {
		defaults.Set(c)
	}

	return c.client.String()
}

// IsWindows returns true on windows hosts
func (c *Connection) IsWindows() (bool, error) {
	if !c.IsConnected() {
		return false, &NotConnectedError{c}
	}

	return c.client.IsWindows(), nil
}

// Exec a command on the host
func (c *Connection) Exec(cmd string, opts ...exec.Option) error {
	if !c.IsConnected() {
		return &NotConnectedError{c}
	}

	return c.client.Exec(cmd, opts...)
}

// ExecWithOutput executes a command on the host and returns it's output
func (c *Connection) ExecWithOutput(cmd string, opts ...exec.Option) (string, error) {
	if !c.IsConnected() {
		return "", &NotConnectedError{c}
	}

	var output string
	opts = append(opts, exec.Output(&output))
	err := c.Exec(cmd, opts...)
	return strings.TrimSpace(output), err
}

// Connect to the host
func (c *Connection) Connect() error {
	if c.client == nil {
		defaults.Set(c)
	}

	if err := c.client.Connect(); err != nil {
		c.client = nil
		return err
	}

	return nil
}

// Disconnect the host
func (c *Connection) Disconnect() {
	if c.client != nil {
		c.client.Disconnect()
	}
	c.client = nil
}

// Upload copies a file to the host. Shortcut to connection.Upload
// Use for larger files instead of configurer.WriteFile when it seems appropriate
func (c *Connection) Upload(src, dst string) error {
	if !c.IsConnected() {
		return &NotConnectedError{c}
	}

	return c.client.Upload(src, dst)
}

func (c *Connection) configuredClient() Client {
	if c.WinRM != nil {
		return c.WinRM
	}

	if c.Localhost != nil {
		return c.Localhost
	}

	if c.SSH != nil {
		return c.SSH
	}

	return nil
}

// DefaultClient returns a default rig connection client (SSH with default settings)
func DefaultClient() Client {
	c := &ssh.Client{}
	defaults.Set(c)
	return c
}
