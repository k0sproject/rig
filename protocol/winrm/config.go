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
	Insecure             bool        `yaml:"insecure" json:"insecure,omitempty" default:"false" jsonschema:"default=false,description=Accept invalid TLS certificates when useHTTPS is set; without useHTTPS allow sending the password over plain HTTP"`
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

// validateAuth checks that a complete authentication method is configured and
// that the transport can carry it.
func (c *Config) validateAuth() error {
	if c.User == "" && c.Password == "" && c.CertPath == "" && c.KeyPath == "" {
		return fmt.Errorf("%w: no authentication method set (provide user+password or certPath+keyPath)", protocol.ErrValidationFailed)
	}
	if (c.CertPath == "") != (c.KeyPath == "") {
		return fmt.Errorf("%w: certPath and keyPath must both be set for certificate authentication", protocol.ErrValidationFailed)
	}
	// The client certificate is only presented in a TLS handshake; over plain
	// HTTP it would be silently ignored.
	if c.CertPath != "" && !c.UseHTTPS {
		return fmt.Errorf("%w: certificate authentication requires useHTTPS", protocol.ErrValidationFailed)
	}
	if (c.User == "") != (c.Password == "") && c.CertPath == "" {
		return fmt.Errorf("%w: user and password must both be set for password authentication", protocol.ErrValidationFailed)
	}
	return c.validatePasswordTransport()
}

// validatePasswordTransport refuses a password on plain HTTP unless Insecure
// is set. NewConnection runs it too, because callers are not required to call
// Validate before connecting.
//
// Insecure is read as "do not refuse to expose the credential", widening it
// from "accept invalid TLS certificates": both senses describe a caller who
// has decided the transport does not have to protect what it carries.
//
// NTLM does not earn an exemption: the WinRM client authenticates with it but
// sends the SOAP envelopes unencrypted, so the commands, their output and any
// credential inside them stay readable.
func (c *Config) validatePasswordTransport() error {
	if c.Password != "" && !c.UseHTTPS && !c.Insecure {
		return fmt.Errorf("%w: a password would be sent over plain HTTP, where any observer recovers it; set useHTTPS, or insecure to send it anyway", protocol.ErrValidationFailed)
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
	if err != nil {
		return nil, err
	}
	return conn, nil
}

// String returns a string representation of the configuration.
func (c *Config) String() string {
	return "winrm.Config{" + net.JoinHostPort(c.Address, strconv.Itoa(c.Port)) + "}"
}
