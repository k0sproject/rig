package rig

import (
	"errors"
	"fmt"

	"github.com/k0sproject/rig/localhost"
	"github.com/k0sproject/rig/openssh"
	"github.com/k0sproject/rig/ssh"
	"github.com/k0sproject/rig/winrm"
)

var _ ProtocolConfigurer = (*CompositeConfig)(nil)

// CompositeConfig is a composite configuration of all the protocols supported by rig.
// It is intended to be embedded into host structs that are unmarshaled from configuration files.
type CompositeConfig struct {
	WinRM     *winrm.Config     `yaml:"winRM,omitempty"`
	SSH       *ssh.Config       `yaml:"ssh,omitempty"`
	Localhost *localhost.Config `yaml:"localhost,omitempty"`
	OpenSSH   *openssh.Config   `yaml:"openSSH,omitempty"`

	s *string
}

// ErrNoClientConfig is returned when no protocol configuration is found in the CompositeConfig.
var ErrNoClientConfig = errors.New("no protocol configuration found")

// Client returns the first configured protocol configuration found in the CompositeConfig.
func (c *CompositeConfig) Client() (Protocol, error) {
	var err error
	var client Protocol
	if c.WinRM != nil {
		client, err = winrm.NewClient(*c.WinRM)
	}

	if c.Localhost != nil {
		client, err = localhost.NewClient(*c.Localhost)
	}

	if c.SSH != nil {
		client, err = ssh.NewClient(*c.SSH)
	}

	if c.OpenSSH != nil {
		client, err = openssh.NewClient(*c.OpenSSH)
	}

	if client == nil && err == nil {
		return nil, ErrNoClientConfig
	}

	if err != nil {
		return nil, fmt.Errorf("create client: %w", err)
	}

	return client, nil
}

// String returns a string representation of the first configured protocol configuration.
func (c *CompositeConfig) String() string {
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
