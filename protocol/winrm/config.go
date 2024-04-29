package winrm

import (
	"fmt"
	"net"
	"strconv"

	"github.com/k0sproject/rig/v2/homedir"
	"github.com/k0sproject/rig/v2/log"
	"github.com/k0sproject/rig/v2/protocol"
	"github.com/k0sproject/rig/v2/protocol/ssh"
)

// Config describes the configuration options for a WinRM connection.
type Config struct {
	log.LoggerInjectable `yaml:"-"`
	protocol.Endpoint    `yaml:",inline"`
	User                 string      `yaml:"user" validate:"omitempty,gt=2"`
	Password             string      `yaml:"password,omitempty"`
	UseHTTPS             bool        `yaml:"useHTTPS"`
	Insecure             bool        `yaml:"insecure"`
	UseNTLM              bool        `yaml:"useNTLM"`
	CACertPath           string      `yaml:"caCertPath,omitempty" validate:"omitempty,file"`
	CertPath             string      `yaml:"certPath,omitempty" validate:"omitempty,file"`
	KeyPath              string      `yaml:"keyPath,omitempty" validate:"omitempty,file"`
	TLSServerName        string      `yaml:"tlsServerName,omitempty" validate:"omitempty,hostname_rfc1123|ip"`
	Bastion              *ssh.Config `yaml:"bastion,omitempty"`
}

// SetDefaults sets various default values.
func (c *Config) SetDefaults() error {
	if c.User == "" {
		c.User = "Administrator"
	}
	if c.CACertPath != "" {
		p, err := homedir.Expand(c.CACertPath)
		if err != nil {
			return fmt.Errorf("cacertpath: %w", err)
		}
		c.CACertPath = p
	}

	if c.CertPath != "" {
		p, err := homedir.Expand(c.CertPath)
		if err != nil {
			return fmt.Errorf("certpath: %w", err)
		}
		c.CertPath = p
	}

	if c.KeyPath != "" {
		p, err := homedir.Expand(c.KeyPath)
		if err != nil {
			return fmt.Errorf("keypath: %w", err)
		}
		c.KeyPath = p
	}

	switch c.Port {
	case 0:
		switch c.UseHTTPS {
		case true:
			c.Port = 5986
		default:
			c.Port = 5985
		}
	case 5986:
		c.UseHTTPS = true
	}

	return nil
}

// Validate checks the configuration for any invalid values.
func (c *Config) Validate() error {
	if err := c.Endpoint.Validate(); err != nil {
		return fmt.Errorf("endpoint: %w", err)
	}

	if c.Bastion != nil {
		if err := c.Bastion.Validate(); err != nil {
			return fmt.Errorf("bastion: %w", err)
		}
	}

	if c.User == "" && c.Password == "" && c.CertPath == "" && c.KeyPath == "" {
		return fmt.Errorf("%w: no authentication method set (user+pass, certificate)", protocol.ErrValidationFailed)
	}

	return nil
}

// Connection returns a new WinRM Connection based on the configuration.
func (c *Config) Connection() (protocol.Connection, error) {
	conn, err := NewConnection(*c, WithLogger(c.Log()))
	return conn, err
}

// String returns a string representation of the configuration.
func (c *Config) String() string {
	return "winrm.Config{" + net.JoinHostPort(c.Address, strconv.Itoa(c.Port)) + "}"
}
