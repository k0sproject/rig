// Package rig provides an easy way to add multi-protocol connectivity and
// multi-os operation support to your application's Host objects
package rig

import (
	"context"
	"fmt"
	"io"

	"github.com/creasty/defaults"
	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/initsystem"
	"github.com/k0sproject/rig/log"
	"github.com/k0sproject/rig/packagemanager"
	"github.com/k0sproject/rig/rigfs"
	"github.com/k0sproject/rig/sudo"
)

type client interface {
	Connect() error
	Disconnect()
	IsWindows() bool
	StartProcess(ctx context.Context, cmd string, stdin io.Reader, stdout io.Writer, stderr io.Writer) (exec.Waiter, error)
	ExecInteractive(cmd string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error
	String() string
	Protocol() string
	IPAddress() string
	IsConnected() bool
}

// Connection is a Struct you can embed into your application's "Host" types
// to give them multi-protocol connectivity.
//
// All of the important fields have YAML tags.
//
// If you have a host like this:
//
//	type Host struct {
//	  rig.Connection `yaml:"connection"`
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
type Connection struct {
	exec.Runner `yaml:"-"`
	WinRM       *WinRM     `yaml:"winRM,omitempty"`
	SSH         *SSH       `yaml:"ssh,omitempty"`
	Localhost   *Localhost `yaml:"localhost,omitempty"`
	OpenSSH     *OpenSSH   `yaml:"openSSH,omitempty"`

	OS *OSRelease `yaml:"-"`

	client     client
	sudoRunner exec.Runner
	fsys       rigfs.Fsys
	sudofsys   rigfs.Fsys
	initSys    initsystem.ServiceManager
	packageMan packagemanager.PackageManager

	initsysRepo    *initsystem.Repository
	packagemanRepo *packagemanager.Repository
	sudoRepo       *sudo.Repository
}

// SetDefaults sets a connection
func (c *Connection) SetDefaults() {
	if c.client == nil {
		c.client = c.configuredClient()
		if c.client == nil {
			c.client = defaultClient()
		}
		_ = defaults.Set(c.client)
	}

	if defaults.CanUpdate(c.initsysRepo) {
		c.initsysRepo = initsystem.DefaultRepository
	}
	if defaults.CanUpdate(c.packagemanRepo) {
		c.packagemanRepo = packagemanager.DefaultRepository
	}
	if defaults.CanUpdate(c.sudoRepo) {
		c.sudoRepo = sudo.DefaultRepository
	}
}

// Protocol returns the connection protocol name
func (c *Connection) Protocol() string {
	if c.client != nil {
		return c.client.Protocol()
	}

	if client := c.configuredClient(); client != nil {
		return client.Protocol()
	}

	return ""
}

// Address returns the connection address
func (c *Connection) Address() string {
	if c.client != nil {
		return c.client.IPAddress()
	}

	if client := c.configuredClient(); client != nil {
		return client.IPAddress()
	}

	return ""
}

// InitSystem returns a ServiceManager for the host's init system
func (c *Connection) InitSystem() (initsystem.ServiceManager, error) {
	if c.initSys == nil {
		is, err := c.initsysRepo.Get(c)
		if err != nil {
			return nil, fmt.Errorf("get init system: %w", err)
		}
		c.initSys = is
	}
	return c.initSys, nil
}

// PackageManager returns a PackageManager for the host's package manager
func (c *Connection) PackageManager() (packagemanager.PackageManager, error) {
	if c.packageMan == nil {
		pm, err := c.packagemanRepo.Get(c)
		if err != nil {
			return nil, fmt.Errorf("get package manager: %w", err)
		}
		c.packageMan = pm
	}
	return c.packageMan, nil
}

// Service returns a Service object for the named service using the host's init system
func (c *Connection) Service(name string) (*Service, error) {
	is, err := c.InitSystem()
	if err != nil {
		return nil, err
	}
	return &Service{runner: c.Sudo(), initsys: is, name: name}, nil
}

// IsConnected returns true if the client is assumed to be connected.
// "Assumed" - as in `Connect()` has been called and no error was returned.
// The underlying client may actually have disconnected and has become
// inoperable, but rig won't know that until you try to execute commands on
// the connection.
func (c *Connection) IsConnected() bool {
	if c.client == nil {
		return false
	}

	return c.client.IsConnected()
}

func (c *Connection) checkConnected() error {
	if !c.IsConnected() {
		return ErrNotConnected
	}

	return nil
}

// String returns a printable representation of the connection, which will look
// like: `[ssh] address:port`
func (c Connection) String() string {
	if c.client == nil {
		return fmt.Sprintf("[%s] %s", c.Protocol(), c.Address())
	}

	return c.client.String()
}

// Fsys returns a fs.FS compatible filesystem interface for accessing files on remote hosts
func (c *Connection) Fsys() rigfs.Fsys {
	if c.fsys == nil {
		c.fsys = rigfs.NewFsys(c)
	}

	return c.fsys
}

// SudoFsys returns a fs.FS compatible filesystem interface for accessing files on remote hosts with sudo permissions
func (c *Connection) SudoFsys() rigfs.Fsys {
	if c.sudofsys == nil {
		c.sudofsys = rigfs.NewFsys(c.Sudo())
	}

	return c.sudofsys
}

// IsWindows returns true on windows hosts
func (c *Connection) IsWindows() bool {
	if c.OS != nil {
		return c.OS.ID == "windows"
	}
	if !c.IsConnected() {
		if client := c.configuredClient(); client != nil {
			return client.IsWindows()
		}
	}
	return c.client.IsWindows()
}

// Connect to the host and identify the operating system and sudo capability
func (c *Connection) Connect() error {
	if c.client == nil {
		if err := defaults.Set(c); err != nil {
			return fmt.Errorf("%w: set defaults: %w", ErrValidationFailed, err)
		}
	}

	if err := c.client.Connect(); err != nil {
		c.client = nil
		log.Debugf("%s: failed to connect: %v", c, err)
		return fmt.Errorf("%w: client connect: %w", ErrNotConnected, err)
	}

	c.Runner = exec.NewHostRunner(c.client)

	return nil
}

// OSRelease returns the host's OSRelease information
func (c *Connection) OSRelease() (*OSRelease, error) {
	if c.OS == nil {
		o, err := GetOSRelease(c.Runner)
		if err != nil {
			return nil, err
		}
		c.OS = &o
	}

	return c.OS, nil
}

// Sudo returns an exec.Runner that runs commands using sudo. If a sudo method can not be detected, a runner that always returns errors is returned.
func (c *Connection) Sudo() exec.Runner {
	if c.sudoRunner == nil {
		fn, err := c.detectSudo()
		if err != nil {
			c.sudoRunner = exec.NewErrorRunner(err)
		} else {
			c.sudoRunner = exec.NewHostRunner(c.client, fn)
		}
	}
	return c.sudoRunner
}

func (c *Connection) detectSudo() (exec.DecorateFunc, error) {
	decorator, err := c.sudoRepo.Get(c)
	if err != nil {
		return nil, fmt.Errorf("%w: get sudo: %w", ErrSudoRequired, err)
	}
	return decorator, nil
}

// ExecInteractive executes a command on the host and passes control of
// local input to the remote command
func (c Connection) ExecInteractive(cmd string, stdin io.Reader, stdout, stderr io.Writer) error {
	if err := c.checkConnected(); err != nil {
		return err
	}

	if err := c.client.ExecInteractive(cmd, stdin, stdout, stderr); err != nil {
		return fmt.Errorf("%w: client exec interactive: %w", ErrCommandFailed, err)
	}

	return nil
}

// Disconnect from the host
func (c *Connection) Disconnect() {
	if c.client != nil {
		c.client.Disconnect()
	}
	c.client = nil
}

func (c *Connection) configuredClient() client {
	if c.WinRM != nil {
		return c.WinRM
	}

	if c.Localhost != nil {
		return c.Localhost
	}

	if c.SSH != nil {
		return c.SSH
	}

	if c.OpenSSH != nil {
		return c.OpenSSH
	}

	return nil
}

func defaultClient() client {
	return &Localhost{Enabled: true}
}
