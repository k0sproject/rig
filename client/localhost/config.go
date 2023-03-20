package localhost

import "github.com/k0sproject/rig/client"

type Config struct {
	Enabled bool `yaml:"enabled" validate:"required,eq=true" default:"true"`
}

func (c *Config) NewClient(_ ...client.Option) (*Client, error) {
	return &Client{}, nil
}
