package rig

import (
	"context"
	"fmt"

	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/initsystem"
)

type serviceState int

const (
	// ServiceStateStopped is the stopped state.
	ServiceStateStopped serviceState = 0
	// ServiceStateStarted is the started state.
	ServiceStateStarted serviceState = 1
)

// Service is an interface for managing a service on an initsystem.
type Service struct {
	runner  exec.ContextRunner
	name    string
	initsys initsystem.ServiceManager
}

// Name returns the name of the service.
func (m *Service) Name() string {
	return m.name
}

// String returns a string representation of the service.
func (m *Service) String() string {
	return fmt.Sprintf("%s@%s", m.Name(), m.runner.String())
}

// Start the service.
func (m *Service) Start(ctx context.Context) error {
	if err := m.initsys.StartService(ctx, m.runner, m.name); err != nil {
		return fmt.Errorf("failed to start service '%s': %w", m.name, err)
	}
	return m.waitState(ctx, ServiceStateStarted)
}

// Stop the service.
func (m *Service) Stop(ctx context.Context) error {
	if err := m.initsys.StopService(ctx, m.runner, m.name); err != nil {
		return fmt.Errorf("failed to stop service '%s': %w", m.name, err)
	}
	return m.waitState(ctx, ServiceStateStopped)
}

// Restart the service.
func (m *Service) Restart(ctx context.Context) error {
	if restarter, ok := m.initsys.(initsystem.ServiceManagerRestarter); ok {
		if err := restarter.RestartService(ctx, m.runner, m.name); err != nil {
			return fmt.Errorf("failed to restart service '%s': %w", m.name, err)
		}
	}
	if err := m.Stop(ctx); err != nil {
		return fmt.Errorf("failed to stop service '%s' for restart: %w", m.name, err)
	}
	if err := m.Start(ctx); err != nil {
		return fmt.Errorf("failed to restart service '%s: %w", m.name, err)
	}
	return nil
}

func (m *Service) waitState(ctx context.Context, state serviceState) error {
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("service '%s' did not reach the desired state: %w", m.name, ctx.Err())
		default:
			if m.initsys.ServiceIsRunning(ctx, m.runner, m.name) == (state == ServiceStateStarted) {
				return nil
			}
		}
	}
}

// Enable the service.
func (m *Service) Enable(ctx context.Context) error {
	if err := m.initsys.EnableService(ctx, m.runner, m.name); err != nil {
		return fmt.Errorf("failed to enable service: %w", err)
	}
	if reloader, ok := m.initsys.(initsystem.ServiceManagerReloader); ok {
		if err := reloader.DaemonReload(ctx, m.runner); err != nil {
			return fmt.Errorf("failed to reload init system after enabling service '%s': %w", m.name, err)
		}
	}
	return nil
}

// Disable the service.
func (m *Service) Disable(ctx context.Context) error {
	if err := m.initsys.DisableService(ctx, m.runner, m.name); err != nil {
		return fmt.Errorf("failed to disable service '%s': %w", m.name, err)
	}
	if reloader, ok := m.initsys.(initsystem.ServiceManagerReloader); ok {
		if err := reloader.DaemonReload(ctx, m.runner); err != nil {
			return fmt.Errorf("failed to reload init system after disabling service '%s': %w", m.name, err)
		}
	}
	return nil
}

// ServiceScriptPath returns the path to the service script.
func (m *Service) ServiceScriptPath(ctx context.Context) (string, error) {
	path, err := m.initsys.ServiceScriptPath(ctx, m.runner, m.name)
	if err != nil {
		return "", fmt.Errorf("failed to get service script path: %w", err)
	}
	return path, nil
}

// ServiceIsRunning returns true if the service is running.
func (m *Service) ServiceIsRunning(ctx context.Context) bool {
	return m.initsys.ServiceIsRunning(ctx, m.runner, m.name)
}
