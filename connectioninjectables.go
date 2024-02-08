package rig

import (
	"errors"
	"fmt"
	"sync"

	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/initsystem"
	"github.com/k0sproject/rig/packagemanager"
	"github.com/k0sproject/rig/rigfs"
	"github.com/k0sproject/rig/sudo"
)

// ConnectionInjectables is a collection of injectable dependencies for a connection
type ConnectionInjectables struct {
	clientConfigurer ClientConfigurer `yaml:",inline"`
	exec.Runner      `yaml:"-"`

	client     Client
	clientOnce sync.Once

	fsys rigfs.Fsys

	initSys     initsystem.ServiceManager
	initSysOnce sync.Once

	packageMan     packagemanager.PackageManager
	packageManOnce sync.Once

	repositories ConnectionRepositories
}

// ConnectionRepositories is a collection of repositories for connection injectables
type ConnectionRepositories struct {
	initsysRepo    *initsystem.Repository
	packagemanRepo *packagemanager.Repository
	sudoRepo       *sudo.Repository
}

// DefaultConnectionRepositories returns a set of default repositories for connection injectables
func DefaultConnectionRepositories() ConnectionRepositories {
	return ConnectionRepositories{
		initsysRepo:    initsystem.DefaultRepository,
		packagemanRepo: packagemanager.DefaultRepository,
		sudoRepo:       sudo.DefaultRepository,
	}
}

// DefaultConnectionInjectables returns a set of default injectables for a connection
func DefaultConnectionInjectables() *ConnectionInjectables {
	return &ConnectionInjectables{
		repositories: DefaultConnectionRepositories(),
	}
}

// Clone returns a copy of the ConnectionInjectables with the given options applied
func (c *ConnectionInjectables) Clone(opts ...Option) *ConnectionInjectables {
	options := Options{ConnectionInjectables: &ConnectionInjectables{
		clientConfigurer: c.clientConfigurer,
		client:           c.client,
		repositories:     c.repositories,
	}}
	options.Apply(opts...)
	return options.ConnectionInjectables
}

// ErrConfiguratorNotSet is returned when a client configurator is not set when trying to connect
var ErrConfiguratorNotSet = errors.New("client configurator not set")

func (c *ConnectionInjectables) initClient() error {
	var err error
	c.clientOnce.Do(func() {
		if c.clientConfigurer == nil {
			err = ErrConfiguratorNotSet
			return
		}
		c.client, err = c.clientConfigurer.Client()
		if err != nil {
			err = fmt.Errorf("configure client (%v): %w", c.clientConfigurer, err)
		}
	})
	return err
}

func (c *ConnectionInjectables) sudoRunner() exec.Runner {
	decorator, err := c.repositories.sudoRepo.Get(c)
	if err != nil {
		return exec.NewErrorRunner(err)
	}
	return exec.NewHostRunner(c.client, decorator)
}

// InitSystem returns a ServiceManager for the host's init system
func (c *ConnectionInjectables) getInitSystem() (initsystem.ServiceManager, error) {
	var err error
	c.initSysOnce.Do(func() {
		c.initSys, err = c.repositories.initsysRepo.Get(c)
		if err != nil {
			err = fmt.Errorf("get init system: %w", err)
		}
	})
	return c.initSys, err
}

// PackageManager returns a PackageManager for the host's package manager
func (c *ConnectionInjectables) getPackageManager() (packagemanager.PackageManager, error) {
	var err error
	c.packageManOnce.Do(func() {
		c.packageMan, err = c.repositories.packagemanRepo.Get(c)
		if err != nil {
			err = fmt.Errorf("get package manager: %w", err)
		}
	})
	return c.packageMan, err
}
