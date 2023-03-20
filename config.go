// Package rig provides an easy way to add multi-protocol connectivity and
// multi-os operation support to your application's Host objects
package rig

import (
	"errors"

	"github.com/k0sproject/rig/client"
	"github.com/k0sproject/rig/client/localhost"
	"github.com/k0sproject/rig/client/ssh"
	"github.com/k0sproject/rig/client/winrm"
	"github.com/k0sproject/rig/exec"
)

var ErrNoProtocolConfiguration = errors.New("no suitable connection configuration provided")

type Config struct {
	SSH       *ssh.Config       `yaml:"ssh,omitempty"`
	WinRM     *winrm.Config     `yaml:"winRM,omitempty"`
	Localhost *localhost.Config `yaml:"localhost,omitempty"`
}

func (c *Config) NewClient(opts ...client.Option) (exec.Client, error) {
	if c.SSH != nil {
		return c.SSH.NewClient(opts...)
	} else if c.WinRM != nil {
		return c.WinRM.NewClient(opts...)
	} else if c.Localhost != nil && c.Localhost.Enabled {
		return &localhost.Client{}, nil
	} else {
		return nil, ErrNoProtocolConfiguration
	}
}
