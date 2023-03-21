package winrm

import (
	"github.com/k0sproject/rig/client"
	"github.com/k0sproject/rig/client/ssh"
	"github.com/k0sproject/rig/log"
)

type Config struct {
	log.Logging

	Address       string      `yaml:"address" validate:"required,hostname|ip"`
	User          string      `yaml:"user" validate:"omitempty,gt=2" default:"Administrator"`
	Port          int         `yaml:"port" default:"5985" validate:"gt=0,lte=65535"`
	Password      *string     `yaml:"password,omitempty"`
	UseHTTPS      bool        `yaml:"useHTTPS" default:"false"`
	Insecure      bool        `yaml:"insecure" default:"false"`
	UseNTLM       bool        `yaml:"useNTLM" default:"false"`
	CACertPath    *string     `yaml:"caCertPath,omitempty" validate:"omitempty,file"`
	CertPath      *string     `yaml:"certPath,omitempty" validate:"omitempty,file"`
	KeyPath       *string     `yaml:"keyPath,omitempty" validate:"omitempty,file"`
	TLSServerName *string     `yaml:"tlsServerName,omitempty" validate:"omitempty,hostname|ip"`
	Bastion       *ssh.Config `yaml:"bastion,omitempty"`
}

func (c *Config) NewClient(opts ...client.Option) (client.Connection, error) {
	return NewClient(c, opts...)
}
