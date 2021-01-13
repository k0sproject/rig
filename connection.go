package rig

import (
	"fmt"
	"reflect"
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

	OsInfo *OSVersion `yaml:"-"`

	client Client `yaml:"-"`
}

// SetDefaults sets a connection
func (c *Connection) SetDefaults() {
	if c.client == nil {
		c.client = c.configuredClient()
		if c.client == nil {
			c.client = DefaultClient()
		}
	}

	defaults.Set(c.client)
}

// IsConnected returns true if the client is assumed to be connected (the client library may have become inoperable but rig won't know that)
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

// Connect to the host and identify operating system
func (c *Connection) Connect() error {
	if c.client == nil {
		defaults.Set(c)
	}

	if err := c.client.Connect(); err != nil {
		c.client = nil
		return err
	}

	r, err := GetResolver(c)
	if err != nil {
		return err
	}

	o, err := r.Resolve(c)
	c.OsInfo = &o

	return nil
}

// Execf is like exec but with sprintf templating
func (c *Connection) Execf(s string, params ...interface{}) error {
	opts, args := groupParams(params)
	return c.Exec(fmt.Sprintf(s, args...), opts...)
}

// ExecWithOutputf is like ExecWithOutput but with sprintf templating
func (c *Connection) ExecWithOutputf(s string, params ...interface{}) (string, error) {
	opts, args := groupParams(params)
	return c.ExecWithOutput(fmt.Sprintf(s, args...), opts...)
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

// separates exec.Options from sprintf templating args
func groupParams(params ...interface{}) (opts []exec.Option, args []interface{}) {
	sample := reflect.TypeOf(exec.HideCommand())
	for _, v := range params {
		if reflect.TypeOf(v) == sample {
			opts = append(opts, v.(exec.Option))
		} else {
			args = append(args, v)
		}
	}
	return
}
