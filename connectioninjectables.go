package rig

import (
	"fmt"

	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/initsystem"
	"github.com/k0sproject/rig/packagemanager"
	"github.com/k0sproject/rig/rigfs"
	"github.com/k0sproject/rig/sudo"
)

type ConnectionInjectables struct {
	ClientConfigurer ClientConfigurer `yaml:",inline"`
	exec.Runner      `yaml:"-"`

	client Client

	fsys       rigfs.Fsys
	sudofsys   rigfs.Fsys
	initSys    initsystem.ServiceManager
	packageMan packagemanager.PackageManager

	repositories ConnectionRepositories
}

type ConnectionRepositories struct {
	initsysRepo    *initsystem.Repository
	packagemanRepo *packagemanager.Repository
	sudoRepo       *sudo.Repository
}

func DefaultConnectionRepositories() ConnectionRepositories {
	return ConnectionRepositories{
		initsysRepo:    initsystem.DefaultRepository,
		packagemanRepo: packagemanager.DefaultRepository,
		sudoRepo:       sudo.DefaultRepository,
	}
}

func DefaultConnectionInjectables() *ConnectionInjectables {
	return &ConnectionInjectables{
		repositories: DefaultConnectionRepositories(),
	}
}

func (c *ConnectionInjectables) Clone(opts ...Option) *ConnectionInjectables {
	options := Options{ConnectionInjectables: &ConnectionInjectables{
		ClientConfigurer: c.ClientConfigurer,
		client:           c.client,
		repositories:     c.repositories,
	}}
	options.Apply(opts...)
	return options.ConnectionInjectables
}

func (c *ConnectionInjectables) sudoRunner() exec.Runner {
	decorator, err := c.repositories.sudoRepo.Get(c)
	if err != nil {
		return exec.NewErrorRunner(err)
	} else {
		return exec.NewHostRunner(c.client, decorator)
	}
}

// InitSystem returns a ServiceManager for the host's init system
func (c *ConnectionInjectables) InitSystem() (initsystem.ServiceManager, error) {
	if c.initSys == nil {
		is, err := c.repositories.initsysRepo.Get(c)
		if err != nil {
			return nil, fmt.Errorf("get init system: %w", err)
		}
		c.initSys = is
	}
	return c.initSys, nil
}

// PackageManager returns a PackageManager for the host's package manager
func (c *ConnectionInjectables) PackageManager() (packagemanager.PackageManager, error) {
	if c.packageMan == nil {
		pm, err := c.repositories.packagemanRepo.Get(c)
		if err != nil {
			return nil, fmt.Errorf("get package manager: %w", err)
		}
		c.packageMan = pm
	}
	return c.packageMan, nil
}
