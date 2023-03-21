package localhost

import (
	"errors"

	"github.com/k0sproject/rig/client"
)

var ErrLocalhostConfiguredButNotEnabled = errors.New("localhost protocol configured but not enabled")

type Config struct {
	Enabled bool `yaml:"enabled" validate:"required,eq=true" default:"true"`
}

func (c *Config) NewClient(_ ...client.Option) (client.Connection, error) {
	if !c.Enabled {
		return nil, ErrLocalhostConfiguredButNotEnabled
	}
	return &Client{}, nil
}
