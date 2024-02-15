package winrm

import (
	"fmt"
	"net"
	"strconv"

	"github.com/k0sproject/rig/homedir"
	"github.com/k0sproject/rig/protocol"
	"github.com/k0sproject/rig/ssh"
)

// Config describes the configuration options for a WinRM connection.
type Config struct {
	Address       string      `yaml:"address" validate:"required,hostname_rfc1123|ip"`
	User          string      `yaml:"user" validate:"omitempty,gt=2" default:"Administrator"`
	Port          int         `yaml:"port" default:"5985" validate:"gt=0,lte=65535"`
	Password      string      `yaml:"password,omitempty"`
	UseHTTPS      bool        `yaml:"useHTTPS" default:"false"`
	Insecure      bool        `yaml:"insecure" default:"false"`
	UseNTLM       bool        `yaml:"useNTLM" default:"false"`
	CACertPath    string      `yaml:"caCertPath,omitempty" validate:"omitempty,file"`
	CertPath      string      `yaml:"certPath,omitempty" validate:"omitempty,file"`
	KeyPath       string      `yaml:"keyPath,omitempty" validate:"omitempty,file"`
	TLSServerName string      `yaml:"tlsServerName,omitempty" validate:"omitempty,hostname_rfc1123|ip"`
	Bastion       *ssh.Config `yaml:"bastion,omitempty"`
}

// SetDefaults sets various default values.
func (c *Config) SetDefaults() {
	if p, err := homedir.Expand(c.CACertPath); err == nil {
		c.CACertPath = p
	}

	if p, err := homedir.Expand(c.CertPath); err == nil {
		c.CertPath = p
	}

	if p, err := homedir.Expand(c.KeyPath); err == nil {
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
}

// Validate checks the configuration for any invalid values.
func (c *Config) Validate() error {
	if c.Address == "" {
		return fmt.Errorf("%w: address is required", protocol.ErrValidationFailed)
	}

	if c.Port <= 0 || c.Port > 65535 {
		return fmt.Errorf("%w: port must be between 1 and 65535", protocol.ErrValidationFailed)
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
	return NewConnection(*c)
}

// String returns a string representation of the configuration.
func (c *Config) String() string {
	return "winrm.Config{" + net.JoinHostPort(c.Address, strconv.Itoa(c.Port)) + "}"
}
