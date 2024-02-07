package rig

import "errors"

var _ ClientConfigurer = (*ClientConfig)(nil)

// ClientConfig is the full configuration for a client with all the protocols supported by this package.
// You can create a subset of this to only support some of them or use one of the protocols as a standalone
// ClientConfigurer.
type ClientConfig struct {
	WinRM     *WinRM     `yaml:"winRM,omitempty"`
	SSH       *SSH       `yaml:"ssh,omitempty"`
	Localhost *Localhost `yaml:"localhost,omitempty"`
	OpenSSH   *OpenSSH   `yaml:"openSSH,omitempty"`

	s *string
}

// ErrNoClientConfig is returned when no protocol configuration is found in the ClientConfig.
var ErrNoClientConfig = errors.New("no protocol configuration found")

// Client returns the first configured protocol configuration found in the ClientConfig.
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

// String returns a string representation of the first configured protocol configuration found in the ClientConfig.
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
