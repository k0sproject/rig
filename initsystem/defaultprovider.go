// Package initsystem provides a common interface for interacting with init systems like systemd, openrc, sysvinit, etc.
package initsystem

import (
	"context"
	"errors"
	"sync"

	"github.com/k0sproject/rig/v2/cmd"
	"github.com/k0sproject/rig/v2/plumbing"
)

// ServiceManager defines the methods for interacting with an init system like OpenRC.
type ServiceManager interface {
	StartService(ctx context.Context, h cmd.ContextRunner, s string) error
	StopService(ctx context.Context, h cmd.ContextRunner, s string) error
	ServiceScriptPath(ctx context.Context, h cmd.ContextRunner, s string) (string, error)
	EnableService(ctx context.Context, h cmd.ContextRunner, s string) error
	DisableService(ctx context.Context, h cmd.ContextRunner, s string) error
	ServiceIsRunning(ctx context.Context, h cmd.ContextRunner, s string) bool
}

// ServiceManagerLogReader is a servicemanager that supports reading service logs.
type ServiceManagerLogReader interface {
	ServiceLogs(ctx context.Context, h cmd.ContextRunner, s string, lines int) ([]string, error)
}

// ServiceManagerRestarter is a servicemanager that supports direct restarts (instead of stop+start).
type ServiceManagerRestarter interface {
	RestartService(ctx context.Context, h cmd.ContextRunner, s string) error
}

// ServiceManagerReloader is a servicemanager that needs reloading (like systemd daemon-reload).
type ServiceManagerReloader interface {
	DaemonReload(ctx context.Context, h cmd.ContextRunner) error
}

// ServiceEnvironmentManager is a servicemanager that supports environment files (like systemd .env files).
type ServiceEnvironmentManager interface {
	ServiceEnvironmentPath(ctx context.Context, h cmd.ContextRunner, s string) (string, error)
	ServiceEnvironmentContent(env map[string]string) string
}

var (
	// DefaultProvider is the default repository for init systems.
	DefaultProvider = sync.OnceValue(func() *Provider {
		provider := NewProvider()
		RegisterSystemd(provider)
		RegisterOpenRC(provider)
		RegisterUpstart(provider)
		RegisterSysVinit(provider)
		RegisterWinSCM(provider)
		RegisterRunit(provider)
		RegisterLaunchd(provider)
		return provider
	})

	// ErrNoInitSystem is returned when no supported init system is found.
	ErrNoInitSystem = errors.New("no supported init system found")
)

// InitSystemProvider is a function that returns a ServiceManager given a runner.
type InitSystemProvider interface { //nolint:revive // stutter
	Get(conn cmd.ContextRunner) (ServiceManager, error)
}

// Factory is a type alias for the plumbing.Factory type specialized for initsystem ServiceManagers.
type Factory = plumbing.Factory[cmd.ContextRunner, ServiceManager]

// Provider is a type alias for the plumbing.Provider type specialized for initsystem ServiceManagers.
type Provider = plumbing.Provider[cmd.ContextRunner, ServiceManager]

// NewProvider returns a new Provider.
func NewProvider() *Provider {
	return plumbing.NewProvider[cmd.ContextRunner, ServiceManager](ErrNoInitSystem)
}
