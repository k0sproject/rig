package winrm

import (
	"net"
	"strconv"

	"github.com/k0sproject/rig/homedir"
	"github.com/k0sproject/rig/protocol"
	"github.com/k0sproject/rig/ssh"
)

// Config describes the configuration options for a WinRM connection
type Config struct {
	Address       string          `yaml:"address" validate:"required,hostname_rfc1123|ip"`
	User          string          `yaml:"user" validate:"omitempty,gt=2" default:"Administrator"`
	Port          int             `yaml:"port" default:"5985" validate:"gt=0,lte=65535"`
	Password      string          `yaml:"password,omitempty"`
	UseHTTPS      bool            `yaml:"useHTTPS" default:"false"`
	Insecure      bool            `yaml:"insecure" default:"false"`
	UseNTLM       bool            `yaml:"useNTLM" default:"false"`
	CACertPath    string          `yaml:"caCertPath,omitempty" validate:"omitempty,file"`
	CertPath      string          `yaml:"certPath,omitempty" validate:"omitempty,file"`
	KeyPath       string          `yaml:"keyPath,omitempty" validate:"omitempty,file"`
	TLSServerName string          `yaml:"tlsServerName,omitempty" validate:"omitempty,hostname_rfc1123|ip"`
	Bastion       *ssh.Connection `yaml:"bastion,omitempty"` // TODO: this needs to be done some other way. and it's just a dial function. need to figure out the unmarshaling.
}

// SetDefaults sets various default values
func (c *Config) SetDefaults() {
	if p, err := homedir.ExpandFile(c.CACertPath); err == nil {
		c.CACertPath = p
	}

	if p, err := homedir.ExpandFile(c.CertPath); err == nil {
		c.CertPath = p
	}

	if p, err := homedir.ExpandFile(c.KeyPath); err == nil {
		c.KeyPath = p
	}

	if c.Port == 5985 && c.UseHTTPS {
		c.Port = 5986
	}
}

// Connection returns a new WinRM Connection based on the configuration
func (c *Config) Connection() (protocol.Connection, error) {
	return NewConnection(*c)
}

// String returns a string representation of the configuration
func (c *Config) String() string {
	return "winrm.Config{" + net.JoinHostPort(c.Address, strconv.Itoa(c.Port)) + "}"
}
