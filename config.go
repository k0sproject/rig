// Package rig provides an easy way to add multi-protocol connectivity and
// multi-os operation support to your application's Host objects
package rig

import (
	"errors"
	"fmt"

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
	var configurer clientConfigurer
	var count int
	if c.SSHConfig != nil {
		configurer = c.SSHConfig
		count++
	}
	if c.WinRMConfig != nil {
		configurer = c.WinRMConfig
		count++
	}
	if c.LocalhostConfig != nil {
		configurer = c.LocalhostConfig
		count++
	}

	switch {
	case count == 0:
		return nil, ErrNoProtocolConfiguration
	case count > 1:
		return nil, ErrMultipleProtocolsConfigured
	}

	conn, err := configurer.NewClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("combined config new client: %w", err)
	}
	return conn, nil
}

type clientConfigurer interface {
	NewClient(...client.Option) (client.Connection, error)
}
