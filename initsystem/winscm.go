package initsystem

import (
	"context"
	"errors"
	"fmt"

	"github.com/k0sproject/rig/exec"
	ps "github.com/k0sproject/rig/powershell"
)

var errNotSupported = errors.New("not supported on windows")

// WinSCM is a struct that implements the InitSystem interface for Windows Service Control Manager
type WinSCM struct{}

// StartService starts a service
func (c WinSCM) StartService(ctx context.Context, h exec.ContextRunner, s string) error {
	if err := h.ExecContext(ctx, `sc start %s`, ps.DoubleQuote(s)); err != nil {
		return fmt.Errorf("failed to start service %s: %w", s, err)
	}
	return nil
}

// StopService stops a service
func (c WinSCM) StopService(ctx context.Context, h exec.ContextRunner, s string) error {
	if err := h.ExecContext(ctx, `sc stop %s`, ps.DoubleQuote(s)); err != nil {
		return fmt.Errorf("failed to stop service %s: %w", s, err)
	}
	return nil
}

// ServiceScriptPath returns the path to a service configuration file
func (c WinSCM) ServiceScriptPath(_ context.Context, _ exec.ContextRunner, _ string) (string, error) {
	return "", errNotSupported
}

// RestartService restarts a service
func (c WinSCM) RestartService(ctx context.Context, h exec.ContextRunner, s string) error {
	if err := h.ExecContext(ctx, ps.Cmd(fmt.Sprintf(`Restart-Service %s`, ps.DoubleQuote(s)))); err != nil {
		return fmt.Errorf("failed to restart service %s: %w", s, err)
	}
	return nil
}

// EnableService enables a service
func (c WinSCM) EnableService(ctx context.Context, h exec.ContextRunner, s string) error {
	if err := h.ExecContext(ctx, `sc.exe config %s start=enabled`, ps.DoubleQuote(s)); err != nil {
		return fmt.Errorf("failed to enable service %s: %w", s, err)
	}

	return nil
}

// DisableService disables a service
func (c WinSCM) DisableService(ctx context.Context, h exec.ContextRunner, s string) error {
	if err := h.ExecContext(ctx, `sc.exe config %s start=disabled`, ps.DoubleQuote(s)); err != nil {
		return fmt.Errorf("failed to disable service %s: %w", s, err)
	}
	return nil
}

// ServiceIsRunning returns true if a service is running
func (c WinSCM) ServiceIsRunning(ctx context.Context, h exec.ContextRunner, s string) bool {
	return h.ExecContext(ctx, `sc.exe query %s | findstr "RUNNING"`, ps.DoubleQuote(s)) == nil
}

// RegisterWinSCM registers the WinSCM in a repository
func RegisterWinSCM(repo *Repository) {
	repo.Register(func(c exec.ContextRunner) ServiceManager {
		if c.IsWindows() {
			return &WinSCM{}
		}
		return nil
	})
}
