package ssh

import (
	"github.com/k0sproject/rig/client"
	"github.com/k0sproject/rig/log"
)

// PasswordCallback is a function that is called when a passphrase is needed to decrypt a private key
type PasswordCallback func() (secret string, err error)

type Config struct {
	log.Logging

	Address          string           `yaml:"address" validate:"required,hostname|ip"`
	User             *string          `yaml:"user" validate:"required" default:"root"`
	Port             *int             `yaml:"port" default:"22" validate:"gt=0,lte=65535"`
	KeyPath          *string          `yaml:"keyPath" validate:"omitempty"`
	Bastion          *Config          `yaml:"bastion,omitempty"`
	PasswordCallback PasswordCallback `yaml:"-"`
}

func (c *Config) NewClient(opts ...client.Option) (client.Connection, error) {
	return NewClient(c, opts...)
}
