package rig

import (
	"context"
	"fmt"

	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/initsystem"
)

type serviceState int

const (
	ServiceStateStopped serviceState = 0
	ServiceStateStarted serviceState = 1
)

// ServiceManager is an interface for managing services on a system
type Service struct {
	runner  exec.ContextRunner
	name    string
	initsys initsystem.ServiceManager
}

func (m *Service) Name() string {
	return m.name
}

func (m *Service) String() string {
	return fmt.Sprintf("%s@%s", m.Name(), m.runner.String())
}

func (m *Service) Start(ctx context.Context) error {
	if err := m.initsys.StartService(ctx, m.runner, m.name); err != nil {
		return fmt.Errorf("failed to start service '%s': %w", m.name, err)
	}
	return m.waitState(ctx, ServiceStateStarted)
}

func (m *Service) Stop(ctx context.Context) error {
	if err := m.initsys.StopService(ctx, m.runner, m.name); err != nil {
		return fmt.Errorf("failed to stop service '%s': %w", m.name, err)
	}
	return m.waitState(ctx, ServiceStateStopped)
}

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

func (m *Service) ServiceScriptPath(ctx context.Context) (string, error) {
	return m.initsys.ServiceScriptPath(ctx, m.runner, m.name)
}

func (m *Service) ServiceIsRunning(ctx context.Context) bool {
	return m.initsys.ServiceIsRunning(ctx, m.runner, m.name)
}
