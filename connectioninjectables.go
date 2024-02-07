package rig

import (
	"fmt"
	"sync"

	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/initsystem"
	"github.com/k0sproject/rig/packagemanager"
	"github.com/k0sproject/rig/rigfs"
	"github.com/k0sproject/rig/sudo"
)

type ConnectionInjectables struct {
	ClientConfigurer ClientConfigurer `yaml:",inline"`
	exec.Runner      `yaml:"-"`
	mu               sync.Mutex

	client     Client
	clientOnce sync.Once

	fsys     rigfs.Fsys
	fsysOnce sync.Once

	initSys     initsystem.ServiceManager
	initSysOnce sync.Once

	packageMan     packagemanager.PackageManager
	packageManOnce sync.Once

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

var ErrConfiguratorNotSet = fmt.Errorf("client configurator not set")

func (c *ConnectionInjectables) initClient() error {
	var err error
	c.clientOnce.Do(func() {
		if c.ClientConfigurer == nil {
			err = ErrConfiguratorNotSet
			return
		}
		c.client, err = c.ClientConfigurer.Client()
		if err != nil {
			err = fmt.Errorf("configure client (%v): %w", c.ClientConfigurer, err)
		}
	})
	return err
}


func (c *ConnectionInjectables) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.initClient(); err != nil {
		return fmt.Errorf("init client: %w", err)
	}
	if err := c.client.Connect(); err != nil {
		return fmt.Errorf("client connect: %w", err)
	}

	c.Runner = exec.NewHostRunner(c.client)

	return nil
}

// Disconnect from the host
func (c *ConnectionInjectables) Disconnect() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.client != nil {
		c.client.Disconnect()
	}
	c.Runner = exec.NewErrorRunner(ErrNotConnected)
}

func (c *ConnectionInjectables) sudoRunner() exec.Runner {
	decorator, err := c.repositories.sudoRepo.Get(c)
	if err != nil {
		return exec.NewErrorRunner(err)
	}
	return exec.NewHostRunner(c.client, decorator)
}

// InitSystem returns a ServiceManager for the host's init system
func (c *ConnectionInjectables) InitSystem() (initsystem.ServiceManager, error) {
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
func (c *ConnectionInjectables) PackageManager() (packagemanager.PackageManager, error) {
	var err error
	c.packageManOnce.Do(func() {
		c.packageMan, err = c.repositories.packagemanRepo.Get(c)
		if err != nil {
			err = fmt.Errorf("get package manager: %w", err)
		}
	})
	return c.packageMan, err
}
