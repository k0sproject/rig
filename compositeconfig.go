package rig

import (
	"errors"
	"fmt"

	"github.com/k0sproject/rig/localhost"
	"github.com/k0sproject/rig/openssh"
	"github.com/k0sproject/rig/ssh"
	"github.com/k0sproject/rig/winrm"
)

var _ ConnectionConfigurer = (*CompositeConfig)(nil)

// CompositeConfig is a composite configuration of all the protocols supported by rig.
// It is intended to be embedded into host structs that are unmarshaled from configuration files.
type CompositeConfig struct {
	WinRM     *winrm.Config     `yaml:"winRM,omitempty"`
	SSH       *ssh.Config       `yaml:"ssh,omitempty"`
	Localhost *localhost.Config `yaml:"localhost,omitempty"`
	OpenSSH   *openssh.Config   `yaml:"openSSH,omitempty"`

	s *string
}

// ErrNoConnectionConfig is returned when no protocol configuration is found in the CompositeConfig.
var ErrNoConnectionConfig = errors.New("no protocol configuration found")

// Connection returns the first configured protocol configuration found in the CompositeConfig.
func (c *CompositeConfig) Connection() (Connection, error) {
	var err error
	var client Connection
	if c.WinRM != nil {
		client, err = c.WinRM.Connection()
	}

	if c.Localhost != nil {
		client, err = c.Localhost.Connection()
	}

	if c.SSH != nil {
		client, err = c.SSH.Connection()
	}

	if c.OpenSSH != nil {
		client, err = c.OpenSSH.Connection()
	}

	if client == nil && err == nil {
		return nil, ErrNoConnectionConfig
	}

	if err != nil {
		return nil, fmt.Errorf("create client: %w", err)
	}

	return client, nil
}

// String returns a string representation of the first configured protocol configuration.
func (c *CompositeConfig) String() string {
	if c.s == nil {
		conn, err := c.Connection()
		if err != nil {
			return "[invalid]"
		}
		s := conn.String()
		c.s = &s
	}

	return *c.s
}
