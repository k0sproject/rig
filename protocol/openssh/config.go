package openssh

import (
	"fmt"
	"net"
	"strconv"

	"github.com/k0sproject/rig/v2/homedir"
	"github.com/k0sproject/rig/v2/protocol"
	"github.com/k0sproject/rig/v2/sshconfig"
)

// Config describes the configuration options for an OpenSSH connection.
type Config struct {
	Address             string                    `yaml:"address" json:"address" validate:"required" jsonschema:"required,description=Address of the remote host"`
	User                *string                   `yaml:"user" json:"user,omitempty" jsonschema:"description=Optional SSH user"`
	Port                *int                      `yaml:"port" json:"port,omitempty" jsonschema:"minimum=1,maximum=65535,description=Optional SSH port"`
	KeyPath             *string                   `yaml:"keyPath,omitempty" json:"keyPath,omitempty" jsonschema:"description=Path to SSH private key"`
	ConfigPath          *string                   `yaml:"configPath,omitempty" json:"configPath,omitempty" jsonschema:"description=Path to SSH config file"`
	Options             sshconfig.OptionArguments `yaml:"options,omitempty" json:"options,omitempty" jsonschema:"description=Additional SSH options as ssh_config key-value pairs"`
	DisableMultiplexing bool                      `yaml:"disableMultiplexing,omitempty" json:"disableMultiplexing,omitempty" jsonschema:"default=false,description=Disable SSH connection multiplexing"`
}

// Connection returns a new OpenSSH connection based on the configuration.
func (c *Config) Connection() (protocol.Connection, error) {
	return NewConnection(*c)
}

// String returns a string representation of the configuration.
func (c *Config) String() string {
	if c.Port == nil {
		return "openssh.Config{" + c.Address + "}"
	}
	return "openssh.Config{" + net.JoinHostPort(c.Address, strconv.Itoa(*c.Port)) + "}"
}

// SetDefaults sets the default values for the configuration.
func (c *Config) SetDefaults() {
	if c.KeyPath != nil {
		if path, err := homedir.Expand(*c.KeyPath); err == nil {
			c.KeyPath = &path
		}
	}
	if c.ConfigPath != nil {
		if path, err := homedir.Expand(*c.ConfigPath); err == nil {
			c.ConfigPath = &path
		}
	}
}

// Validate checks the configuration for any invalid values.
func (c *Config) Validate() error {
	if c.Address == "" {
		return fmt.Errorf("%w: address is required", protocol.ErrValidationFailed)
	}

	if c.Port != nil && (*c.Port <= 0 || *c.Port > 65535) {
		return fmt.Errorf("%w: port must be between 1 and 65535", protocol.ErrValidationFailed)
	}

	return nil
}
