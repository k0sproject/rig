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

// NotConnectedError is returned when attempting to perform remote operations
// on Host when it is not connected
type NotConnectedError rigError

// Error returns the error message
func (e *NotConnectedError) Error() string { return e.Connection.String() + ": not connected" }

// Connection is a Struct you can embed into your application's "Host" types
// to give them multi-protocol connectivity.
type Connection struct {
	WinRM     *winrm.Client `yaml:"winRM,omitempty"`
	SSH       *ssh.Client   `yaml:"ssh,omitempty"`
	Localhost *local.Client `yaml:"localhost,omitempty"`

	OSVersion OSVersion `yaml:"-"`

	client Client `yaml:"-"`
}

// SetDefaults sets a connection
func (c *Connection) SetDefaults() {
	if c.client == nil {
		c.client = c.configuredClient()
		if c.client == nil {
			c.client = defaultClient()
		}
	}

	defaults.Set(c.client)
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

// Exec runs a command on the host
func (c *Connection) Exec(cmd string, opts ...exec.Option) error {
	if !c.IsConnected() {
		return &NotConnectedError{c}
	}

	return c.client.Exec(cmd, opts...)
}

// ExecOutput runs a command on the host and returns the output as a String
func (c *Connection) ExecOutput(cmd string, opts ...exec.Option) (string, error) {
	if !c.IsConnected() {
		return "", &NotConnectedError{c}
	}

	var output string
	opts = append(opts, exec.Output(&output))
	err := c.Exec(cmd, opts...)
	return strings.TrimSpace(output), err
}

type resolveFunc func(*Connection) (OSVersion, error)

// GetResolver returns an OS version resolver
func (c *Connection) resolver() resolveFunc {
	isWin, err := c.IsWindows()
	if err != nil {
		return func(_ *Connection) (OSVersion, error) {
			return OSVersion{}, err
		}
	}

	if isWin {
		return resolveWindows
	}

	if err := c.Exec("uname | grep -q Darwin"); err == nil {
		return resolveDarwin
	}

	return resolveLinux
}

// Connect to the host and identify the operating system
func (c *Connection) Connect() error {
	if c.client == nil {
		defaults.Set(c)
	}

	if err := c.client.Connect(); err != nil {
		c.client = nil
		return err
	}

	r := c.resolver()

	o, err := r(c)
	if err != nil {
		return err
	}
	c.OSVersion = o

	return nil
}

// Execf is just like `Exec` but you can use Sprintf templating for the command
func (c *Connection) Execf(s string, params ...interface{}) error {
	opts, args := groupParams(params)
	return c.Exec(fmt.Sprintf(s, args...), opts...)
}

// ExecOutputf is like ExecOutput but you can use Sprintf
// templating for the command
func (c *Connection) ExecOutputf(s string, params ...interface{}) (string, error) {
	opts, args := groupParams(params)
	return c.ExecOutput(fmt.Sprintf(s, args...), opts...)
}

// ExecInteractive executes a command on the host and passes control of
// local input to the remote command
func (c *Connection) ExecInteractive(cmd string) error {
	if !c.IsConnected() {
		return &NotConnectedError{c}
	}

	return c.client.ExecInteractive(cmd)
}

// Disconnect from the host
func (c *Connection) Disconnect() {
	if c.client != nil {
		c.client.Disconnect()
	}
	c.client = nil
}

// Upload copies a file from a local path src to the remote host path dst. For
// smaller files you should probably use os.WriteFile
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

func defaultClient() Client {
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
