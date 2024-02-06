package rig

import "fmt"

var _ ClientConfigurer = (*ClientConfig)(nil)

type ClientConfig struct {
	WinRM     *WinRM     `yaml:"winRM,omitempty"`
	SSH       *SSH       `yaml:"ssh,omitempty"`
	Localhost *Localhost `yaml:"localhost,omitempty"`
	OpenSSH   *OpenSSH   `yaml:"openSSH,omitempty"`

	s *string
}

var ErrNoClientConfig = fmt.Errorf("no protocol configuration found")

func (c *ClientConfig) Client() (Client, error) {
	if c.WinRM != nil {
		return c.WinRM, nil
	}

	if c.Localhost != nil {
		return c.Localhost, nil
	}

	if c.SSH != nil {
		return c.SSH, nil
	}

	if c.OpenSSH != nil {
		return c.OpenSSH, nil
	}

	return nil, ErrNoClientConfig
}

func (c *ClientConfig) String() string {
	if c.s == nil {
		client, err := c.Client()
		if err != nil {
			return "[invalid]"
		}
		s := client.String()
		c.s = &s
	}

	return *c.s
}
