package rig

import (
	"fmt"

	"github.com/k0sproject/rig/v2/protocol"
	"github.com/k0sproject/rig/v2/protocol/localhost"
	"github.com/k0sproject/rig/v2/protocol/openssh"
	"github.com/k0sproject/rig/v2/protocol/ssh"
	"github.com/k0sproject/rig/v2/protocol/winrm"
)

var _ ConnectionFactory = (*CompositeConfig)(nil)

// LocalhostConfig is a bool-valued type that also accepts the v0.x YAML form
// "localhost:\n  enabled: true" for backward compatibility. To assign a bool
// variable, use an explicit cast: LocalhostConfig(b).
type LocalhostConfig bool

// UnmarshalYAML implements yaml.Unmarshaler, accepting both:
//
//	localhost: true           (current form)
//	localhost:                (v0.x form)
//	  enabled: true
func (l *LocalhostConfig) UnmarshalYAML(unmarshal func(any) error) error {
	*l = false
	var b bool
	if err := unmarshal(&b); err == nil {
		*l = LocalhostConfig(b)
		return nil
	}
	var old struct {
		Enabled bool `yaml:"enabled"`
	}
	if err := unmarshal(&old); err == nil {
		*l = LocalhostConfig(old.Enabled)
		return nil
	} else {
		return fmt.Errorf("%w: localhost must be a bool or {enabled: bool}: %w", protocol.ErrValidationFailed, err)
	}
}

// CompositeConfig is a composite configuration of all the protocols supported out of the box by rig.
// It is intended to be embedded into host structs that are unmarshaled from configuration files.
type CompositeConfig struct {
	SSH       *ssh.Config     `yaml:"ssh,omitempty"`
	WinRM     *winrm.Config   `yaml:"winRM,omitempty"`
	OpenSSH   *openssh.Config `yaml:"openSSH,omitempty"`
	Localhost LocalhostConfig `yaml:"localhost,omitempty"`
}

func (c *CompositeConfig) configuredConfig() (ConnectionFactory, error) {
	var factory ConnectionFactory
	count := 0

	if c.WinRM != nil {
		factory = c.WinRM
		count++
	}

	if c.SSH != nil {
		factory = c.SSH
		count++
	}

	if c.OpenSSH != nil {
		factory = c.OpenSSH
		count++
	}

	if c.Localhost {
		count++
		conn, err := localhost.NewConnection()
		if err != nil {
			return nil, fmt.Errorf("create localhost connection: %w", err)
		}
		factory = conn
	}

	switch count {
	case 0:
		return nil, fmt.Errorf("%w: no protocol configuration", protocol.ErrValidationFailed)
	case 1:
		return factory, nil
	default:
		return nil, fmt.Errorf("%w: multiple protocols configured for a single client", protocol.ErrValidationFailed)
	}
}

type validatable interface {
	Validate() error
}

// Validate the configuration.
func (c *CompositeConfig) Validate() error {
	factory, err := c.configuredConfig()
	if err != nil {
		return err
	}
	if v, ok := factory.(validatable); ok {
		if err := v.Validate(); err != nil {
			return fmt.Errorf("validate %T: %w", factory, err)
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
