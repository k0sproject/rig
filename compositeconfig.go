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

type oldLocalhost struct {
	Enabled bool `yaml:"enabled"`
}

// intermediary structure for handling both the old v0.x and the new format
// for localhost.
type compositeConfigIntermediary struct {
	SSH       *ssh.Config     `yaml:"ssh,omitempty"`
	WinRM     *winrm.Config   `yaml:"winRM,omitempty"`
	OpenSSH   *openssh.Config `yaml:"openSSH,omitempty"`
	Localhost any             `yaml:"localhost,omitempty"`
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (c *CompositeConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var intermediary compositeConfigIntermediary
	if err := unmarshal(&intermediary); err != nil {
		return err
	}

	c.SSH = intermediary.SSH
	c.WinRM = intermediary.WinRM
	c.OpenSSH = intermediary.OpenSSH

	if intermediary.Localhost != nil {
		switch v := intermediary.Localhost.(type) {
		case bool:
			c.Localhost = v
		case oldLocalhost:
			c.Localhost = v.Enabled
		default:
			return fmt.Errorf("unmarshal localhost - invalid type %T: %w", v, protocol.ErrValidationFailed)
		}
	}

	return nil
}

func (c *CompositeConfig) configuredConfig() (ConnectionConfigurer, error) {
	var configurer ConnectionConfigurer
	count := 0

	if c.WinRM != nil {
		configurer = c.WinRM
		count++
	}

	if c.SSH != nil {
		configurer = c.SSH
		count++
	}

	if c.OpenSSH != nil {
		configurer = c.OpenSSH
		count++
	}

	if c.Localhost {
		count++
		conn, err := localhost.NewConnection()
		if err != nil {
			return nil, fmt.Errorf("create localhost connection: %w", err)
		}
		configurer = conn
	}

	switch count {
	case 0:
		return nil, fmt.Errorf("%w: no protocol configuration", protocol.ErrValidationFailed)
	case 1:
		return configurer, nil
	default:
		return nil, fmt.Errorf("%w: multiple protocols configured for a single client", protocol.ErrValidationFailed)
	}
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
