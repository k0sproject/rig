package ssh

import (
	"fmt"
	"net"
	"strconv"

	"github.com/k0sproject/rig/homedir"
	"github.com/k0sproject/rig/log"
	"github.com/k0sproject/rig/protocol"
	ssh "golang.org/x/crypto/ssh"
)

// PasswordCallback is a function that is called when a passphrase is needed to decrypt a private key.
type PasswordCallback func() (secret string, err error)

// Config describes an SSH connection's configuration.
type Config struct {
	log.LoggerInjectable `yaml:"-"`
	Address              string           `yaml:"address" validate:"required,hostname_rfc1123|ip"`
	User                 string           `yaml:"user" validate:"required" default:"root"`
	Port                 int              `yaml:"port" default:"22" validate:"gt=0,lte=65535"`
	KeyPath              *string          `yaml:"keyPath" validate:"omitempty"`
	Bastion              *Config          `yaml:"bastion,omitempty"`
	PasswordCallback     PasswordCallback `yaml:"-"`

	// AuthMethods can be used to pass in a list of crypto/ssh.AuthMethod objects
	// for example to use a private key from memory:
	//   ssh.PublicKeys(privateKey)
	// For convenience, you can use ParseSSHPrivateKey() to parse a private key:
	//   authMethods, err := ssh.ParseSSHPrivateKey(key, rig.DefaultPassphraseCallback)
	AuthMethods []ssh.AuthMethod `yaml:"-"`
}

// Connection returns a new Connection object based on the configuration.
func (c *Config) Connection() (protocol.Connection, error) {
	conn, err := NewConnection(*c, WithLogger(c.Log()))
	return conn, err
}

// String returns a string representation of the configuration.
func (c *Config) String() string {
	return "ssh.Config{" + net.JoinHostPort(c.Address, strconv.Itoa(c.Port)) + "}"
}

// SetDefaults sets the default values for the configuration.
func (c *Config) SetDefaults() {
	if c.Port == 0 {
		c.Port = 22
	}
	if c.User == "" {
		c.User = "root"
	}
	if c.KeyPath != nil {
		if path, err := homedir.Expand(*c.KeyPath); err == nil {
			c.KeyPath = &path
		}
	}
	if c.Bastion != nil {
		c.Bastion.SetDefaults()
	}
}

// Validate returns an error if the configuration is invalid.
func (c *Config) Validate() error {
	if c.Address == "" {
		return fmt.Errorf("%w: address is required", protocol.ErrValidationFailed)
	}

	if c.Port <= 0 || c.Port > 65535 {
		return fmt.Errorf("%w: port must be between 1 and 65535", protocol.ErrValidationFailed)
	}

	if c.KeyPath != nil {
		path, err := homedir.Expand(*c.KeyPath)
		if err != nil {
			return fmt.Errorf("%w: keyPath: %w", protocol.ErrValidationFailed, err)
		}
		c.KeyPath = &path
	}

	if c.Bastion != nil {
		if err := c.Bastion.Validate(); err != nil {
			return fmt.Errorf("bastion: %w", err)
		}
	}

	return nil
}
