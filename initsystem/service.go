package initsystem

import (
	"context"
	"fmt"

	"github.com/k0sproject/rig/v2/cmd"
	"github.com/k0sproject/rig/v2/plumbing"
)

// Service provides a unified interface to interact with different init systems.
// It ensures that a suitable service manager is lazily initialized and made
// available for service managament operations. It supports operations like
// starting and stopping services.
type Service struct {
	lazy *plumbing.LazyService[cmd.ContextRunner, ServiceManager]
}

// GetServiceManager returns a ServiceManager or an error if a service manager
// could not be initialized.
func (p *Service) GetServiceManager() (ServiceManager, error) {
	sm, err := p.lazy.Get()
	if err != nil {
		return nil, fmt.Errorf("get service manager: %w", err)
	}
	return sm, nil
}

// ServiceManager provides easy access to the underlying init system service manager
// instance. It initializes the service manager if it has not been initialized yet.
// If the initialization fails, a NullServiceManager instance is returned which will
// return the initialization error on every operation that is attempted on it.
func (p *Service) ServiceManager() ServiceManager {
	sm, err := p.lazy.Get()
	if err != nil {
		return &NullServiceManager{Err: err}
	}
	return sm
}

// NewInitSystemService creates a new instance of PackageManagerService
// with the provided PackageManagerProvider.
func NewInitSystemService(provider InitSystemProvider, runner cmd.ContextRunner) *Service {
	return &Service{plumbing.NewLazyService[cmd.ContextRunner, ServiceManager](provider, runner)}
}

// NullServiceManager is a service manager that always returns an error on
// every operation.
type NullServiceManager struct {
	Err error
}

func (n *NullServiceManager) err(ctx context.Context) error {
	if ctx.Err() != nil {
		return fmt.Errorf("context error: %w", ctx.Err())
	}
	return n.Err
}

// StartService always returns an error.
func (n *NullServiceManager) StartService(ctx context.Context, _ cmd.ContextRunner, s string) error {
	return fmt.Errorf("start service %s: %w", s, n.err(ctx))
}

// StopService always returns an error.
func (n *NullServiceManager) StopService(ctx context.Context, _ cmd.ContextRunner, s string) error {
	return fmt.Errorf("stop service %s: %w", s, n.err(ctx))
}

// ServiceScriptPath always returns an error.
func (n *NullServiceManager) ServiceScriptPath(ctx context.Context, _ cmd.ContextRunner, s string) (string, error) {
	return "", fmt.Errorf("service script path for %s: %w", s, n.err(ctx))
}

// EnableService always returns an error.
func (n *NullServiceManager) EnableService(ctx context.Context, _ cmd.ContextRunner, s string) error {
	return fmt.Errorf("enable service %s: %w", s, n.err(ctx))
}

// DisableService always returns an error.
func (n *NullServiceManager) DisableService(ctx context.Context, _ cmd.ContextRunner, s string) error {
	return fmt.Errorf("disable service %s: %w", s, n.err(ctx))
}

// ServiceIsRunning always returns false.
func (n *NullServiceManager) ServiceIsRunning(_ context.Context, _ cmd.ContextRunner, _ string) bool {
	return false
}
