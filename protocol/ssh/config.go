package ssh

import (
	"fmt"
	"net"
	"strconv"

	"github.com/k0sproject/rig/v2/homedir"
	"github.com/k0sproject/rig/v2/log"
	"github.com/k0sproject/rig/v2/protocol"
	"github.com/k0sproject/rig/v2/sshconfig"
	ssh "golang.org/x/crypto/ssh"
)

// PasswordCallback is a function that is called when a passphrase is needed to decrypt a private key.
type PasswordCallback func() (secret string, err error)

// Config describes an SSH connection's configuration.
type Config struct {
	log.LoggerInjectable `yaml:"-"`
	protocol.Endpoint    `yaml:",inline"`
	User                 string           `yaml:"user" validate:"required" default:"root"`
	KeyPath              *string          `yaml:"keyPath" validate:"omitempty"`
	Bastion              *Config          `yaml:"bastion,omitempty"`
	ConfigPath           string           `yaml:"configPath,omitempty"`
	PasswordCallback     PasswordCallback `yaml:"-"`

	// AuthMethods can be used to pass in a list of crypto/ssh.AuthMethod objects
	// for example to use a private key from memory:
	//   ssh.PublicKeys(privateKey)
	// For convenience, you can use ParseSSHPrivateKey() to parse a private key:
	//   authMethods, err := ssh.ParseSSHPrivateKey(key, rig.DefaultPassphraseCallback)
	AuthMethods []ssh.AuthMethod `yaml:"-"`

	sshconfig.Config `yaml:",inline"`

	options *Options
}

// Connection returns a new Connection object based on the configuration.
func (c *Config) Connection() (protocol.Connection, error) {
	conn, err := NewConnection(*c, c.options.Funcs()...)
	if !log.HasLogger(conn) && log.HasLogger(c) {
		log.InjectLogger(c.Log(), c)
	}
	return conn, err
}

// String returns a string representation of the configuration.
func (c *Config) String() string {
	return "ssh.Config{" + net.JoinHostPort(c.Address, strconv.Itoa(c.Endpoint.Port)) + "}"
}

// SetDefaults sets the default values for the configuration.
func (c *Config) SetDefaults(opts ...Option) error {
	options := NewOptions(opts...)

	if c.KeyPath != nil {
		path, err := homedir.Expand(*c.KeyPath)
		if err != nil {
			return fmt.Errorf("keypath: %w", err)
		}
		c.KeyPath = &path
		c.IdentityFile = []string{path}
	}

	c.Host = c.Address
	if c.Endpoint.Port != 0 {
		c.Config.Port = c.Endpoint.Port
	}

	if c.User != "" {
		c.Config.User = c.User
	}

	var parser ConfigParser
	if options.ConfigParser != nil {
		parser = options.ConfigParser
	} else {
		p, err := ParserCache().Get(c.ConfigPath)
		if err != nil {
			return fmt.Errorf("get ssh config parser: %w", err)
		}
		parser = p
	}

	if err := parser.Apply(c, c.Address); err != nil {
		return fmt.Errorf("apply values from ssh config: %w", err)
	}

	c.Endpoint.Port = c.Config.Port

	if c.Config.User != "" {
		c.User = c.Config.User
	}

	if c.Config.Hostname != "" {
		c.Address = c.Config.Hostname
	} else {
		c.Address = c.Host
	}

	if c.Bastion != nil {
		if err := c.Bastion.SetDefaults(); err != nil {
			return fmt.Errorf("bastion: %w", err)
		}
	}

	return nil
}

// Validate returns an error if the configuration is invalid.
func (c *Config) Validate() error {
	if err := c.Endpoint.Validate(); err != nil {
		return fmt.Errorf("endpoint: %w", err)
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
