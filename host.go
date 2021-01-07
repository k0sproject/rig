package rig

import (
	"strings"

	"github.com/creasty/defaults"
	"github.com/k0sproject/rig/connection/local"
	"github.com/k0sproject/rig/connection/ssh"
	"github.com/k0sproject/rig/connection/winrm"
	"github.com/k0sproject/rig/exec"
)

type rigError struct {
	Host *Host
}

// NotConnectedError is returned when attempting to perform remote operations on Host when it is not connected
type NotConnectedError rigError

// Error returns the error message
func (e *NotConnectedError) Error() string { return e.Host.String() + ": not connected" }

// Host is a host that can be connected to via winrm, ssh or using the "localhost" connection
type Host struct {
	WinRM     *winrm.Connection `yaml:"winRM,omitempty"`
	SSH       *ssh.Connection   `yaml:"ssh,omitempty"`
	Localhost *local.Connection `yaml:"localhost,omitempty"`

	connection Connection `yaml:"-"`
}

// SetDefaults sets a connection
func (h *Host) SetDefaults() error {
	if h.connection == nil {
		h.connection = h.configuredConnection()
		if h.connection == nil {
			h.connection = h.DefaultConnection()
		}
	}

	return defaults.Set(h.connection)
}

func (h *Host) IsConnected() bool {
	if h.connection == nil {
		return false
	}

	return h.connection.IsConnected()
}

// String implements the Stringer interface for logging purposes
func (h *Host) String() string {
	if !h.IsConnected() {
		defaults.Set(h)
	}

	return h.connection.String()
}

// IsWindows returns true on windows hosts
func (h *Host) IsWindows() (bool, error) {
	if !h.IsConnected() {
		return false, &NotConnectedError{}
	}

	return h.connection.IsWindows(), nil
}

// Exec a command on the host
func (h *Host) Exec(cmd string, opts ...exec.Option) error {
	if !h.IsConnected() {
		return &NotConnectedError{}
	}

	return h.connection.Exec(cmd, opts...)
}

// ExecWithOutput executes a command on the host and returns it's output
func (h *Host) ExecWithOutput(cmd string, opts ...exec.Option) (string, error) {
	if !h.IsConnected() {
		return "", &NotConnectedError{}
	}

	var output string
	opts = append(opts, exec.Output(&output))
	err := h.Exec(cmd, opts...)
	return strings.TrimSpace(output), err
}

// Connect to the host
func (h *Host) Connect() error {
	if h.connection == nil {
		defaults.Set(h)
	}

	if err := h.connection.Connect(); err != nil {
		h.connection = nil
		return err
	}

	return nil
}

// Disconnect the host
func (h *Host) Disconnect() {
	if h.connection != nil {
		h.connection.Disconnect()
	}
	h.connection = nil
}

// Upload copies a file to the host. Shortcut to connection.Upload
// Use for larger files instead of configurer.WriteFile when it seems appropriate
func (h *Host) Upload(src, dst string) error {
	if !h.IsConnected() {
		return &NotConnectedError{}
	}

	return h.connection.Upload(src, dst)
}

func (h *Host) configuredConnection() Connection {
	if h.WinRM != nil {
		return h.WinRM
	}

	if h.Localhost != nil {
		return h.Localhost
	}

	if h.SSH != nil {
		return h.SSH
	}

	return nil
}

// DefaultConnection returns a default rig connection (SSH with default settings)
func (h *Host) DefaultConnection() Connection {
	c := &ssh.Connection{}
	defaults.Set(c)
	return c
}
