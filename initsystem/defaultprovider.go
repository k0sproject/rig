// Package initsystem provides a common interface for interacting with init systems like systemd, openrc, sysvinit, etc.
package initsystem

import (
	"context"
	"errors"

	"github.com/k0sproject/rig/exec"
)

// ServiceManager defines the methods for interacting with an init system like OpenRC.
type ServiceManager interface {
	StartService(ctx context.Context, h exec.ContextRunner, s string) error
	StopService(ctx context.Context, h exec.ContextRunner, s string) error
	ServiceScriptPath(ctx context.Context, h exec.ContextRunner, s string) (string, error)
	EnableService(ctx context.Context, h exec.ContextRunner, s string) error
	DisableService(ctx context.Context, h exec.ContextRunner, s string) error
	ServiceIsRunning(ctx context.Context, h exec.ContextRunner, s string) bool
}

// ServiceManagerLogReader is a servicemanager that supports reading service logs.
type ServiceManagerLogReader interface {
	ServiceLogs(ctx context.Context, h exec.ContextRunner, s string, lines int) ([]string, error)
}

// ServiceManagerRestarter is a servicemanager that supports direct restarts (instead of stop+start).
type ServiceManagerRestarter interface {
	RestartService(ctx context.Context, h exec.ContextRunner, s string) error
}

// ServiceManagerReloader is a servicemanager that needs reloading (like systemd daemon-reload).
type ServiceManagerReloader interface {
	DaemonReload(ctx context.Context, h exec.ContextRunner) error
}

// ServiceEnvironmentManager is a servicemanager that supports environment files (like systemd .env files).
type ServiceEnvironmentManager interface {
	ServiceEnvironmentPath(ctx context.Context, h exec.ContextRunner, s string) (string, error)
	ServiceEnvironmentContent(env map[string]string) string
}

// ServiceManagerFactory is a function that returns a ServiceManager.
type ServiceManagerFactory func(c exec.ContextRunner) ServiceManager

var (
	// DefaultProvider is the default repository for init systems.
	DefaultProvider = NewProvider()
	// ErrNoInitSystem is returned when no supported init system is found.
	ErrNoInitSystem = errors.New("no supported init system found")
)

func init() {
	RegisterSystemd(DefaultProvider)
	RegisterOpenRC(DefaultProvider)
	RegisterUpstart(DefaultProvider)
	RegisterSysVinit(DefaultProvider)
	RegisterWinSCM(DefaultProvider)
	RegisterRunit(DefaultProvider)
	RegisterLaunchd(DefaultProvider)
}

// Provider is a collection of ServiceManagerFactories.
type Provider struct {
	systems []ServiceManagerFactory
}

// Register adds a ServiceManagerFactory to the repository.
func (r *Provider) Register(factory ServiceManagerFactory) {
	r.systems = append(r.systems, factory)
}

// Get returns the first ServiceManager that matches the current system.
func (r *Provider) Get(c exec.ContextRunner) (ServiceManager, error) {
	for _, factory := range r.systems {
		system := factory(c)
		if system != nil {
			return system, nil
		}
	}
	return nil, ErrNoInitSystem
}

// NewProvider returns a new Provider.
func NewProvider(factories ...ServiceManagerFactory) *Provider {
	return &Provider{systems: factories}
}
