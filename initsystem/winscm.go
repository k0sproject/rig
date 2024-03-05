package initsystem

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/k0sproject/rig/cmd"
	ps "github.com/k0sproject/rig/powershell"
)

var errNotSupported = errors.New("not supported on windows")

// WinSCM is a struct that implements the InitSystem interface for Windows Service Control Manager.
type WinSCM struct{}

// StartService starts a service.
func (c WinSCM) StartService(ctx context.Context, h cmd.ContextRunner, s string) error {
	if err := h.ExecContext(ctx, "sc.exe start "+ps.DoubleQuote(s)); err != nil {
		return fmt.Errorf("failed to start service %s: %w", s, err)
	}
	return nil
}

// StopService stops a service.
func (c WinSCM) StopService(ctx context.Context, h cmd.ContextRunner, s string) error {
	if err := h.ExecContext(ctx, "sc.exe stop "+ps.DoubleQuote(s)); err != nil {
		return fmt.Errorf("failed to stop service %s: %w", s, err)
	}
	return nil
}

// ServiceScriptPath returns the path to a service configuration file.
func (c WinSCM) ServiceScriptPath(_ context.Context, _ cmd.ContextRunner, _ string) (string, error) {
	return "", errNotSupported
}

// RestartService restarts a service.
func (c WinSCM) RestartService(ctx context.Context, h cmd.ContextRunner, s string) error {
	if err := h.ExecContext(ctx, "Restart-Service "+ps.DoubleQuote(s), cmd.PS()); err != nil {
		return fmt.Errorf("failed to restart service %s: %w", s, err)
	}
	return nil
}

// EnableService enables a service.
func (c WinSCM) EnableService(ctx context.Context, h cmd.ContextRunner, s string) error {
	if err := h.ExecContext(ctx, fmt.Sprintf("sc.exe config %s start=enabled", ps.DoubleQuote(s))); err != nil {
		return fmt.Errorf("failed to enable service %s: %w", s, err)
	}

	return nil
}

// ServiceLogs returns the logs for a service.
func (c WinSCM) ServiceLogs(ctx context.Context, h cmd.ContextRunner, s string, lines int) ([]string, error) {
	command := fmt.Sprintf(`Get-EventLog -LogName System -Source "Service Control Manager" -Newest %[1]d | Where-Object {$_.Message -like "*%s*"} | Select-Object -Property TimeGenerated, Message -First %[1]d`, lines, s)
	out, err := h.ExecOutputContext(ctx, command, cmd.PS())
	if err != nil {
		return nil, fmt.Errorf("failed to get logs for service %s: %w", s, err)
	}
	return strings.Split(out, "\n"), nil
}

// DisableService disables a service.
func (c WinSCM) DisableService(ctx context.Context, h cmd.ContextRunner, s string) error {
	if err := h.ExecContext(ctx, fmt.Sprintf("sc.exe config %s start=disabled", ps.DoubleQuote(s))); err != nil {
		return fmt.Errorf("failed to disable service %s: %w", s, err)
	}
	return nil
}

// ServiceIsRunning returns true if a service is running.
func (c WinSCM) ServiceIsRunning(ctx context.Context, h cmd.ContextRunner, s string) bool {
	return h.ExecContext(ctx, fmt.Sprintf(`sc.exe query %s | findstr "RUNNING"`, ps.DoubleQuote(s))) == nil
}

// RegisterWinSCM registers the WinSCM in a repository.
func RegisterWinSCM(repo *Provider) {
	repo.Register(func(c cmd.ContextRunner) (ServiceManager, bool) {
		if !c.IsWindows() {
			return nil, false
		}
		return &WinSCM{}, true
	})
}
