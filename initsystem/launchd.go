package initsystem

import (
	"context"
	"fmt"
	"path"
	"strconv"
	"strings"

	"github.com/k0sproject/rig/v2/cmd"
	"github.com/k0sproject/rig/v2/sh"
)

// Launchd is the init system for macOS (and darwin), the implementation is very basic and doesn't handle services in user space.
type Launchd struct{}

const launchctl = sh.CommandBuilder("launchctl")

var launchctlCmd = launchctl.Args

// StartService starts a launchd service.
func (i Launchd) StartService(ctx context.Context, h cmd.ContextRunner, s string) error {
	if err := h.ExecContext(ctx, launchctlCmd("kickstart", s).String()); err != nil {
		return fmt.Errorf("failed to start service %s: %w", s, err)
	}
	return nil
}

// StopService stops a launchd service.
func (i Launchd) StopService(ctx context.Context, h cmd.ContextRunner, s string) error {
	if err := h.ExecContext(ctx, launchctlCmd("kill", s).String()); err != nil {
		return fmt.Errorf("failed to stop service %s: %w", s, err)
	}
	return nil
}

// ServiceIsRunning checks if a launchd service is running.
func (i Launchd) ServiceIsRunning(ctx context.Context, h cmd.ContextRunner, s string) bool {
	// This might need more sophisticated parsing
	return h.ExecContext(ctx, launchctlCmd("list").Pipe("grep", "-q", s).String()) == nil
}

// ServiceScriptPath returns the path to a launchd service plist file.
func (i Launchd) ServiceScriptPath(_ context.Context, _ cmd.ContextRunner, s string) (string, error) {
	// Assumes plist files are located in /Library/LaunchDaemons - TODO: could be under user
	plistPath := path.Join("/Library/LaunchDaemons", s+".plist")
	return plistPath, nil
}

// EnableService enables a launchd service (not very elegant).
func (i Launchd) EnableService(ctx context.Context, h cmd.ContextRunner, s string) error {
	if err := h.ExecContext(ctx, launchctlCmd("enable", s).String()); err != nil {
		return fmt.Errorf("failed to enable service: %w", err)
	}
	return nil
}

// DisableService disables a launchd service by renaming the plist file (not very elegant).
func (i Launchd) DisableService(ctx context.Context, h cmd.ContextRunner, s string) error {
	if err := h.ExecContext(ctx, launchctlCmd("disable", s).String()); err != nil {
		return fmt.Errorf("failed to disable service: %w", err)
	}
	return nil
}

// ServiceLogs returns the logs for a launchd service.
func (i Launchd) ServiceLogs(ctx context.Context, h cmd.ContextRunner, s string, lines int) ([]string, error) {
	out, err := h.ExecOutputContext(ctx, fmt.Sprintf("log show --predicate 'subsystem contains %s' --debug --info --last 10m --style syslog", strconv.QuoteToASCII(s)))
	if err != nil {
		return nil, fmt.Errorf("failed to get logs for service %s: %w", s, err)
	}
	rows := strings.Split(out, "\n")
	if len(rows) > lines {
		return rows[len(rows)-lines:], nil
	}
	return rows, nil
}

// RegisterLaunchd registers the launchd init system to a init system repository.
func RegisterLaunchd(repo *Provider) {
	repo.Register(func(c cmd.ContextRunner) (ServiceManager, bool) {
		if c.IsWindows() {
			return nil, false
		}
		if err := c.ExecContext(context.Background(), "test -f /System/Library/CoreServices/SystemVersion.plist"); err != nil {
			return nil, false
		}

		return Launchd{}, true
	})
}
