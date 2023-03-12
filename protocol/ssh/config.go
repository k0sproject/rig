package ssh

import "golang.org/x/exp/slog"

// PasswordCallback is a function that is called when a passphrase is needed to decrypt a private key
type PasswordCallback func() (secret string, err error)

type Config struct {
	Address          string           `yaml:"address" validate:"required,hostname|ip"`
	User             *string          `yaml:"user" validate:"required" default:"root"`
	Port             *int             `yaml:"port" default:"22" validate:"gt=0,lte=65535"`
	KeyPath          *string          `yaml:"keyPath" validate:"omitempty"`
	Bastion          *Config          `yaml:"bastion,omitempty"`
	PasswordCallback PasswordCallback `yaml:"-"`

	Logger *slog.Logger `yaml:"-"`
}

func (c *Config) SetDefaults() {
}
