package initsystem

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/k0sproject/rig/v2/cmd"
	ps "github.com/k0sproject/rig/v2/powershell"
)

var errNotSupported = errors.New("not supported on windows")

// WinSCM is a struct that implements the InitSystem interface for Windows Service Control Manager.
type WinSCM struct{}

// String returns the name of the init system.
func (WinSCM) String() string { return "winscm" }

// StartService starts a service.
//
// -WarningAction SilentlyContinue suppresses the benign "Waiting for service
// '…' to start..." progress warning that Start-Service emits when a service
// stays in StartPending for a moment (common on a cold first start). That
// warning goes to the warning stream, which lands on stderr under a subprocess,
// and rig treats any Windows stderr output as a failure. -ErrorAction Stop
// still turns a genuine start failure into a terminating (non-zero) error, so
// real failures are still detected.
func (c WinSCM) StartService(ctx context.Context, h cmd.ContextRunner, s string) error {
	if err := h.ExecContext(ctx, "$ErrorActionPreference='Stop'\nStart-Service "+ps.SingleQuote(s)+" -ErrorAction Stop -WarningAction SilentlyContinue", cmd.PS()); err != nil {
		return fmt.Errorf("failed to start service %s: %w", s, err)
	}
	return nil
}

// StopService stops a service.
//
// -WarningAction SilentlyContinue suppresses the benign "Waiting for service
// '…' to stop..." progress warning (StopPending) for the same reason as
// StartService; -ErrorAction Stop still surfaces genuine failures.
func (c WinSCM) StopService(ctx context.Context, h cmd.ContextRunner, s string) error {
	if err := h.ExecContext(ctx, "$ErrorActionPreference='Stop'\nStop-Service "+ps.SingleQuote(s)+" -ErrorAction Stop -WarningAction SilentlyContinue", cmd.PS()); err != nil {
		return fmt.Errorf("failed to stop service %s: %w", s, err)
	}
	return nil
}

// ServiceScriptPath returns the path to a service configuration file.
func (c WinSCM) ServiceScriptPath(_ context.Context, _ cmd.ContextRunner, _ string) (string, error) {
	return "", errNotSupported
}

// RestartService restarts a service.
//
// -WarningAction SilentlyContinue suppresses the benign "Waiting for service
// '…' to start/stop..." progress warnings for the same reason as StartService;
// -ErrorAction Stop still surfaces genuine failures.
func (c WinSCM) RestartService(ctx context.Context, h cmd.ContextRunner, s string) error {
	if err := h.ExecContext(ctx, "$ErrorActionPreference='Stop'\nRestart-Service "+ps.SingleQuote(s)+" -ErrorAction Stop -WarningAction SilentlyContinue", cmd.PS()); err != nil {
		return fmt.Errorf("failed to restart service %s: %w", s, err)
	}
	return nil
}

// EnableService enables a service by setting its startup type to Automatic.
func (c WinSCM) EnableService(ctx context.Context, h cmd.ContextRunner, s string) error {
	if err := h.ExecContext(ctx, fmt.Sprintf("$ErrorActionPreference='Stop'\nSet-Service -Name %s -StartupType Automatic -ErrorAction Stop", ps.SingleQuote(s)), cmd.PS()); err != nil {
		return fmt.Errorf("failed to enable service %s: %w", s, err)
	}

	return nil
}

// ServiceLogs returns Service Control Manager lifecycle events for the service from the System
// event log (start, stop, crash). This reflects SCM control events only, not the service's own
// application output. Services that write to a file or a separate event log source are not covered.
func (c WinSCM) ServiceLogs(ctx context.Context, h cmd.ContextRunner, s string, lines int) ([]string, error) {
	command := fmt.Sprintf(`Get-EventLog -LogName System -Source "Service Control Manager" -Newest %[1]d | Where-Object {$_.Message -match [regex]::Escape(%s)} | Select-Object -Property TimeGenerated, Message -First %[1]d`, lines, ps.SingleQuote(s))
	out, err := h.ExecOutputContext(ctx, command, cmd.PS())
	if err != nil {
		return nil, fmt.Errorf("failed to get logs for service %s: %w", s, err)
	}
	return strings.Split(out, "\n"), nil
}

// DisableService disables a service by setting its startup type to Disabled.
func (c WinSCM) DisableService(ctx context.Context, h cmd.ContextRunner, s string) error {
	if err := h.ExecContext(ctx, fmt.Sprintf("$ErrorActionPreference='Stop'\nSet-Service -Name %s -StartupType Disabled -ErrorAction Stop", ps.SingleQuote(s)), cmd.PS()); err != nil {
		return fmt.Errorf("failed to disable service %s: %w", s, err)
	}
	return nil
}

// ServiceIsRunning returns true if a service is running.
func (c WinSCM) ServiceIsRunning(ctx context.Context, h cmd.ContextRunner, s string) bool {
	return h.ExecContext(ctx, fmt.Sprintf("$ErrorActionPreference='Stop'\nif ((Get-Service %s -ErrorAction SilentlyContinue).Status -ne 'Running') { exit 1 }", ps.SingleQuote(s)), cmd.PS()) == nil
}

// SetServiceEnvironment sets environment variables for a Windows service via the registry.
func (c WinSCM) SetServiceEnvironment(ctx context.Context, runner cmd.ContextRunner, s string, env map[string]string) error {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	entries := make([]string, len(keys))
	for i, k := range keys {
		entries[i] = ps.SingleQuote(k + "=" + env[k])
	}
	regPath := ps.SingleQuote(`HKLM:\SYSTEM\CurrentControlSet\Services\` + s)
	script := fmt.Sprintf(
		"$ErrorActionPreference='Stop'\nNew-ItemProperty -LiteralPath %s -Name 'Environment' -PropertyType MultiString -Value @(%s) -Force -ErrorAction Stop | Out-Null",
		regPath,
		strings.Join(entries, ","),
	)
	if err := runner.ExecContext(ctx, script, cmd.PS(), cmd.Sensitive()); err != nil {
		return fmt.Errorf("failed to set environment for service %s: %w", s, err)
	}
	return nil
}

// RegisterWinSCM registers the WinSCM in a repository.
func RegisterWinSCM(repo *Registry) {
	repo.Register(func(c cmd.ContextRunner) (ServiceManager, bool) {
		if !c.IsWindows() {
			return nil, false
		}
		return &WinSCM{}, true
	})
}
