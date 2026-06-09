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
	log.LoggerInjectable `yaml:"-" json:"-"`
	Address              string      `yaml:"address" json:"address" validate:"required,hostname_rfc1123|ip" jsonschema:"required,description=Address of the remote host"`
	User                 string      `yaml:"user" json:"user,omitempty" validate:"omitempty,gt=2" default:"Administrator" jsonschema:"minLength=3,default=Administrator,description=User to authenticate as"`
	Port                 int         `yaml:"port" json:"port,omitempty" default:"5985" validate:"gt=0,lte=65535" jsonschema:"minimum=1,maximum=65535,description=WinRM port (default 5985; 5986 when useHTTPS is true)"`
	Password             string      `yaml:"password,omitempty" json:"password,omitempty" jsonschema:"minLength=1,description=Password for WinRM authentication"`
	UseHTTPS             bool        `yaml:"useHTTPS" json:"useHTTPS,omitempty" default:"false" jsonschema:"default=false,description=Use HTTPS for WinRM"`
	Insecure             bool        `yaml:"insecure" json:"insecure,omitempty" default:"false" jsonschema:"default=false,description=Accept invalid TLS certificates"`
	UseNTLM              bool        `yaml:"useNTLM" json:"useNTLM,omitempty" default:"false" jsonschema:"default=false,description=Use NTLM authentication"`
	CACertPath           string      `yaml:"caCertPath,omitempty" json:"caCertPath,omitempty" validate:"omitempty,file" jsonschema:"description=Path to CA certificate"`
	CertPath             string      `yaml:"certPath,omitempty" json:"certPath,omitempty" validate:"omitempty,file" jsonschema:"minLength=1,description=Path to client certificate"`
	KeyPath              string      `yaml:"keyPath,omitempty" json:"keyPath,omitempty" validate:"omitempty,file" jsonschema:"minLength=1,description=Path to client key"`
	TLSServerName        string      `yaml:"tlsServerName,omitempty" json:"tlsServerName,omitempty" validate:"omitempty,hostname_rfc1123|ip" jsonschema:"description=TLS server name override"`
	Bastion              *ssh.Config `yaml:"bastion,omitempty" json:"bastion,omitempty" jsonschema:"description=Optional SSH bastion"`
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

// validateAuth checks that a complete authentication method is configured.
func (c *Config) validateAuth() error {
	if c.User == "" && c.Password == "" && c.CertPath == "" && c.KeyPath == "" {
		return fmt.Errorf("%w: no authentication method set (provide user+password or certPath+keyPath)", protocol.ErrValidationFailed)
	}
	if (c.CertPath == "") != (c.KeyPath == "") {
		return fmt.Errorf("%w: certPath and keyPath must both be set for certificate authentication", protocol.ErrValidationFailed)
	}
	if (c.User == "") != (c.Password == "") && c.CertPath == "" {
		return fmt.Errorf("%w: user and password must both be set for password authentication", protocol.ErrValidationFailed)
	}
	return nil
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

	return c.validateAuth()
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
