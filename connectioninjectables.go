package rig

import (
	"errors"
	"fmt"
	"sync"

	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/initsystem"
	"github.com/k0sproject/rig/log"
	"github.com/k0sproject/rig/packagemanager"
	"github.com/k0sproject/rig/rigfs"
	"github.com/k0sproject/rig/sudo"
)

// ClientConfigurer is an interface that can be used to configure a client. The Connect() function calls the Client() function
// to get a client to use for connecting.
type ClientConfigurer interface {
	String() string
	Client() (Client, error)
}

// LoggerFactory is a function that creates a logger
type LoggerFactory func(client Client) log.Logger

var nullLogger = &log.NullLog{}

// DefaultLoggerFactory returns a logger factory that returns a null logger
func DefaultLoggerFactory(_ Client) log.Logger {
	return nullLogger
}

// ConnectionInjectables is a collection of injectable dependencies for a connection
type ConnectionInjectables struct {
	clientConfigurer     ClientConfigurer `yaml:",inline"`
	exec.Runner          `yaml:"-"`
	log.LoggerInjectable `yaml:"-"`

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

type initsystemRepository interface {
	Get(runner exec.ContextRunner) (manager initsystem.ServiceManager, err error)
}

type packagemanagerRepository interface {
	Get(runner exec.ContextRunner) (manager packagemanager.PackageManager, err error)
}

type sudoRepository interface {
	Get(runner exec.SimpleRunner) (decorator exec.DecorateFunc, err error)
}

type rigfsRepository interface {
	Get(runner exec.Runner) (fsys rigfs.Fsys)
}

// ConnectionRepositories is a collection of repositories for connection injectables
type ConnectionRepositories struct {
	initsysRepo    initsystemRepository
	packagemanRepo packagemanagerRepository
	sudoRepo       sudoRepository
	fsysRepo       rigfsRepository
	loggerFactory  LoggerFactory
}

// DefaultConnectionRepositories returns a set of default repositories for connection injectables
func DefaultConnectionRepositories() ConnectionRepositories {
	return ConnectionRepositories{
		initsysRepo:    initsystem.DefaultRepository,
		packagemanRepo: packagemanager.DefaultRepository,
		sudoRepo:       sudo.DefaultRepository,
		fsysRepo:       rigfs.DefaultRepository,
		loggerFactory:  DefaultLoggerFactory,
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
		c.injectLogger(c.clientConfigurer)
		c.client, err = c.clientConfigurer.Client()
		if err != nil {
			err = fmt.Errorf("configure client (%v): %w", c.clientConfigurer, err)
			return
		}
		if !c.HasLogger() {
			c.SetLogger(c.repositories.loggerFactory(c.client))
		}
		c.injectLogger(c.client)
		if c.Runner == nil {
			c.Runner = exec.NewHostRunner(c.client)
		}
	})
	return err
}

func (c *ConnectionInjectables) injectLogger(obj any) {
	log.InjectLogger(c.Log(), obj)
}

func (c *ConnectionInjectables) sudoRunner() exec.Runner {
	decorator, err := c.repositories.sudoRepo.Get(c)
	if err != nil {
		return exec.NewErrorRunner(err)
	}
	runner := exec.NewHostRunner(c.client, decorator)
	c.injectLogger(runner)
	return runner
}

// InitSystem returns a ServiceManager for the host's init system
func (c *ConnectionInjectables) getInitSystem() (initsystem.ServiceManager, error) {
	var err error
	c.initSysOnce.Do(func() {
		c.initSys, err = c.repositories.initsysRepo.Get(c)
		if err != nil {
			err = fmt.Errorf("get init system: %w", err)
		}
		c.injectLogger(c.initSys)
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
		c.injectLogger(c.packageMan)
	})
	return c.packageMan, err
}

func (c *ConnectionInjectables) getFsys() rigfs.Fsys {
	c.fsysOnce.Do(func() {
		c.fsys = c.repositories.fsysRepo.Get(c)
		c.injectLogger(c.fsys)
	})
	return c.fsys
}
