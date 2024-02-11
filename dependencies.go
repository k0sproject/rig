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

// Dependencies is a collection of injectable dependencies for a connection
type Dependencies struct {
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

	providers SubsystemProviders
}

type initsystemProvider interface {
	Get(runner exec.ContextRunner) (manager initsystem.ServiceManager, err error)
}

type packagemanagerProvider interface {
	Get(runner exec.ContextRunner) (manager packagemanager.PackageManager, err error)
}

type sudoProvider interface {
	Get(runner exec.SimpleRunner) (decorator exec.DecorateFunc, err error)
}

type fsProvider interface {
	Get(runner exec.Runner) (fsys rigfs.Fsys)
}

// SubsystemProviders is a collection of repositories for connection injectables
type SubsystemProviders struct {
	initsys        initsystemProvider
	packagemanager packagemanagerProvider
	sudo           sudoProvider
	fsys           fsProvider
	loggerFactory  LoggerFactory
}

// DefaultProviders returns a set of default repositories for connection injectables
func DefaultProviders() SubsystemProviders {
	return SubsystemProviders{
		initsys:        initsystem.DefaultRepository,
		packagemanager: packagemanager.DefaultRepository,
		sudo:           sudo.DefaultRepository,
		fsys:           rigfs.DefaultRepository,
		loggerFactory:  DefaultLoggerFactory,
	}
}

// DefaultDependencies returns a set of default injectables for a connection
func DefaultDependencies() *Dependencies {
	return &Dependencies{
		clientConfigurer: &ClientConfig{},
		providers:        DefaultProviders(),
	}
}

// Clone returns a copy of the ConnectionInjectables with the given options applied
func (c *Dependencies) Clone(opts ...Option) *Dependencies {
	options := Options{connectionDependencies: &Dependencies{
		clientConfigurer: c.clientConfigurer,
		client:           c.client,
		providers:        c.providers,
	}}
	options.Apply(opts...)
	return options.ConnectionDependencies()
}

var (
	// ErrConfiguratorNotSet is returned when a client configurator is not set when trying to connect
	ErrConfiguratorNotSet = errors.New("client configurator not set")

	// ErrClientNotSet is returned when a client is not set when trying to connect
	ErrClientNotSet = errors.New("client not set")
)

func (c *Dependencies) initClient() error {
	var err error
	c.clientOnce.Do(func() {
		if c.client != nil {
			c.clientConfigurer = nil
		}
		if c.client == nil && c.clientConfigurer == nil {
			err = errors.Join(ErrClientNotSet, ErrConfiguratorNotSet)
			return
		}
		if c.clientConfigurer != nil {
			c.injectLogger(c.clientConfigurer)
			c.client, err = c.clientConfigurer.Client()
			if err != nil {
				err = fmt.Errorf("configure client (%v): %w", c.clientConfigurer, err)
				return
			}
		}
		if c.client == nil {
			err = ErrClientNotSet
			return
		}
		if !c.HasLogger() {
			c.SetLogger(c.providers.loggerFactory(c.client))
		}
		c.injectLogger(c.client)
		if c.Runner == nil {
			c.Runner = exec.NewHostRunner(c.client)
		}
	})
	return err
}

func (c *Dependencies) injectLogger(obj any) {
	log.InjectLogger(c.Log(), obj)
}

func (c *Dependencies) sudoRunner() exec.Runner {
	decorator, err := c.providers.sudo.Get(c)
	if err != nil {
		return exec.NewErrorRunner(err)
	}
	runner := exec.NewHostRunner(c.client, decorator)
	c.injectLogger(runner)
	return runner
}

// InitSystem returns a ServiceManager for the host's init system
func (c *Dependencies) getInitSystem() (initsystem.ServiceManager, error) {
	var err error
	c.initSysOnce.Do(func() {
		c.initSys, err = c.providers.initsys.Get(c)
		if err != nil {
			err = fmt.Errorf("get init system: %w", err)
		}
		c.injectLogger(c.initSys)
	})
	return c.initSys, err
}

// PackageManager returns a PackageManager for the host's package manager
func (c *Dependencies) getPackageManager() (packagemanager.PackageManager, error) {
	var err error
	c.packageManOnce.Do(func() {
		c.packageMan, err = c.providers.packagemanager.Get(c)
		if err != nil {
			err = fmt.Errorf("get package manager: %w", err)
		}
		c.injectLogger(c.packageMan)
	})
	return c.packageMan, err
}

func (c *Dependencies) getFsys() rigfs.Fsys {
	c.fsysOnce.Do(func() {
		c.fsys = c.providers.fsys.Get(c)
		c.injectLogger(c.fsys)
	})
	return c.fsys
}
