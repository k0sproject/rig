package rig

import (
	"fmt"

	"github.com/k0sproject/rig/v2/protocol"
	"github.com/k0sproject/rig/v2/protocol/localhost"
	"github.com/k0sproject/rig/v2/protocol/openssh"
	"github.com/k0sproject/rig/v2/protocol/ssh"
	"github.com/k0sproject/rig/v2/protocol/winrm"
)

var _ ConnectionConfigurer = (*CompositeConfig)(nil)

// CompositeConfig is a composite configuration of all the protocols supported out of the box by rig.
// It is intended to be embedded into host structs that are unmarshaled from configuration files.
type CompositeConfig struct {
	SSH       *ssh.Config     `yaml:"ssh,omitempty"`
	WinRM     *winrm.Config   `yaml:"winRM,omitempty"`
	OpenSSH   *openssh.Config `yaml:"openSSH,omitempty"`
	Localhost bool            `yaml:"localhost,omitempty"`
}

func (c *CompositeConfig) configuredConfig() (ConnectionConfigurer, error) {
	if c.WinRM != nil {
		return c.WinRM, nil
	}

	if c.SSH != nil {
		return c.SSH, nil
	}

	if c.OpenSSH != nil {
		return c.OpenSSH, nil
	}

	if c.Localhost {
		conn, err := localhost.NewConnection()
		if err != nil {
			return nil, fmt.Errorf("create localhost connection: %w", err)
		}
		return conn, nil
	}

	return nil, fmt.Errorf("%w: no protocol configuration", protocol.ErrValidationFailed)
}

type validatable interface {
	Validate() error
}

// Validate the configuration.
func (c *CompositeConfig) Validate() error {
	configurer, err := c.configuredConfig()
	if err != nil {
		return err
	}
	if v, ok := configurer.(validatable); ok {
		if err := v.Validate(); err != nil {
			return fmt.Errorf("validate %T: %w", configurer, err)
		}
	}
	return nil
}

// Connection returns a connection for the first configured protocol.
func (c *CompositeConfig) Connection() (protocol.Connection, error) {
	cfg, err := c.configuredConfig()
	if err != nil {
		return nil, err
	}
	conn, err := cfg.Connection()
	if err != nil {
		return nil, fmt.Errorf("create connection for %T: %w", cfg, err)
	}
	return conn, nil
}

// String returns the string representation of the first configured protocol configuration.
func (c *CompositeConfig) String() string {
	cfg, err := c.configuredConfig()
	if err != nil {
		return "unknown{}"
	}
	return cfg.String()
}
