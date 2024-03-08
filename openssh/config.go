package openssh

import (
	"fmt"
	"net"
	"strconv"

	"github.com/k0sproject/rig/v2/homedir"
	"github.com/k0sproject/rig/v2/protocol"
)

// Config describes the configuration options for an OpenSSH connection.
type Config struct {
	Address             string          `yaml:"address" validate:"required"`
	User                *string         `yaml:"user"`
	Port                *int            `yaml:"port"`
	KeyPath             *string         `yaml:"keyPath,omitempty"`
	ConfigPath          *string         `yaml:"configPath,omitempty"`
	Options             OptionArguments `yaml:"options,omitempty"`
	DisableMultiplexing bool            `yaml:"disableMultiplexing,omitempty"`
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

// OptionArguments are options for the OpenSSH client. For example StrictHostkeyChecking: false becomes -o StrictHostKeyChecking=no.
type OptionArguments map[string]any

// Copy returns a copy of the options.
func (o OptionArguments) Copy() OptionArguments {
	dup := make(OptionArguments, len(o))
	for k, v := range o {
		dup[k] = v
	}
	return dup
}

// Set sets an option key to value.
func (o OptionArguments) Set(key string, value any) {
	o[key] = value
}

// SetIfUnset sets the option if it's not already set.
func (o OptionArguments) SetIfUnset(key string, value any) {
	if o.IsSet(key) {
		return
	}
	o.Set(key, value)
}

// IsSet returns true if the option is set.
func (o OptionArguments) IsSet(key string) bool {
	_, ok := o[key]
	return ok
}

// ToArgs converts the options to command line arguments.
func (o OptionArguments) ToArgs() []string {
	args := make([]string, 0, len(o)*2)
	for k, v := range o {
		if b, ok := v.(bool); ok {
			if b {
				args = append(args, "-o", k+"=yes")
			} else {
				args = append(args, "-o", k+"=no")
			}
			continue
		}
		args = append(args, "-o", fmt.Sprintf("%s=%v", k, v))
	}
	return args
}
