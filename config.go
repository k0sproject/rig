// Package rig provides an easy way to add multi-protocol connectivity and
// multi-os operation support to your application's Host objects
package rig

import (
	"errors"

	"github.com/k0sproject/rig/client"
	"github.com/k0sproject/rig/client/localhost"
	"github.com/k0sproject/rig/client/ssh"
	"github.com/k0sproject/rig/client/winrm"
)

var (
	ErrNoProtocolConfiguration     = errors.New("no connection configuration provided")
	ErrMultipleProtocolsConfigured = errors.New("multiple protocols configured, only one is allowed")
)

// Config is the main configuration object for rig
type Config struct {
	SSHConfig       *ssh.Config       `yaml:"ssh,omitempty"`
	WinRMConfig     *winrm.Config     `yaml:"winRM,omitempty"`
	LocalhostConfig *localhost.Config `yaml:"localhost,omitempty"`
}

// NewClient creates a new client based on which protocol is configured
func (c *Config) NewClient(opts ...client.Option) (client.Connection, error) {
	configurer, err := c.getNonNil()
	if err != nil {
		return nil, err
	}

	conn, err := configurer.NewClient(opts...)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

type clientConfigurer interface {
	NewClient(...client.Option) (client.Connection, error)
}

func (c *Config) clientConfigurers() []clientConfigurer {
	return []clientConfigurer{c.SSHConfig, c.WinRMConfig, c.LocalhostConfig}
}

func (c *Config) getNonNil() (clientConfigurer, error) {
	var count int
	var conf clientConfigurer

	for _, v := range c.clientConfigurers() {
		if v != nil {
			count++
			conf = v
		}
	}

	switch {
	case count == 0:
		return nil, ErrNoProtocolConfiguration
	case count > 1:
		return nil, ErrMultipleProtocolsConfigured
	}

	return conf, nil
}
