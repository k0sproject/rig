// Package rig provides an easy way to add multi-protocol connectivity and
// multi-os operation support to your application's Host objects
package rig

import (
	"github.com/k0sproject/rig/pkg/localhost"
	"github.com/k0sproject/rig/pkg/ssh"
	"github.com/k0sproject/rig/pkg/winrm"
)

// var _ rigos.Host = (*Config)(nil)

type sudofn func(string) string

// Config is a Struct you can embed into your application's "Host" types
// to give them multi-protocol connectivity.
//
// All of the important fields have YAML tags.
//
// If you have a host like this:
//
//	type Host struct {
//	  rig.Config `yaml:"connection"`
//	}
//
// and a YAML like this:
//
//	hosts:
//	  - connection:
//	      ssh:
//	        address: 10.0.0.1
//	        port: 8022
//
// you can then simply do this:
//
//	var hosts []*Host
//	if err := yaml.Unmarshal(data, &hosts); err != nil {
//	  panic(err)
//	}
//	for _, h := range hosts {
//	  err := h.Connect()
//	  if err != nil {
//	    panic(err)
//	  }
//	  output, err := h.ExecOutput("echo hello")
//	}
type Config struct {
	WinRM     *winrm.Config     `yaml:"winRM,omitempty"`
	SSH       *ssh.Config       `yaml:"ssh,omitempty"`
	Localhost *localhost.Config `yaml:"localhost,omitempty"`
}
