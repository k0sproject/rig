package initsystem

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/k0sproject/rig/cmd"
	"github.com/k0sproject/rig/sh"
)

// Runit is an init system implementation for runit.
type Runit struct{}

const sv = sh.CommandBuilder("sv")

var svCmd = sv.Args

// StartService starts a runit service.
func (i Runit) StartService(ctx context.Context, h cmd.ContextRunner, s string) error {
	if err := h.ExecContext(ctx, svCmd("start", s).String()); err != nil {
		return fmt.Errorf("failed to start service %s: %w", s, err)
	}
	return nil
}

// StopService stops a runit service.
func (i Runit) StopService(ctx context.Context, h cmd.ContextRunner, s string) error {
	if err := h.ExecContext(ctx, svCmd("stop", s).String()); err != nil {
		return fmt.Errorf("failed to stop service %s: %w", s, err)
	}
	return nil
}

// RestartService restarts a runit service.
func (i Runit) RestartService(ctx context.Context, h cmd.ContextRunner, s string) error {
	if err := h.ExecContext(ctx, svCmd("restart", s).String()); err != nil {
		return fmt.Errorf("failed to restart service %s: %w", s, err)
	}
	return nil
}

// ServiceIsRunning returns true if a runit service is running.
func (i Runit) ServiceIsRunning(ctx context.Context, h cmd.ContextRunner, s string) bool {
	return h.ExecContext(ctx, svCmd("status", s).Pipe("grep", "-q", "run: ").String()) == nil
}

// ServiceScriptPath returns the path to a runit service script.
func (i Runit) ServiceScriptPath(_ context.Context, _ cmd.ContextRunner, s string) (string, error) {
	return "/etc/service/" + s, nil
}

// EnableService creates a symlink in the runit service directory to enable a service.
func (i Runit) EnableService(ctx context.Context, h cmd.ContextRunner, s string) error {
	serviceDir := "/etc/service"                      // Adjust as necessary
	runScriptPath := fmt.Sprintf("/etc/sv/%s/run", s) // Adjust the path to run scripts as necessary
	symlinkPath := path.Join(serviceDir, s)
	if err := h.ExecContext(ctx, sh.Command("ln", "-s", runScriptPath, symlinkPath)); err != nil {
		return fmt.Errorf("failed to enable service %s: %w", s, err)
	}
	return nil
}

// DisableService removes the symlink from the runit service directory to disable a service.
func (i Runit) DisableService(ctx context.Context, h cmd.ContextRunner, s string) error {
	symlinkPath := filepath.Join("/etc/service", s) // Adjust as necessary
	if err := h.ExecContext(ctx, sh.Command("rm", "-f", symlinkPath)); err != nil {
		return fmt.Errorf("failed to disable service %s: %w", s, err)
	}
	return nil
}

// ServiceLogs returns the logs for a runit service from /var/log/<service>/current. It's not guaranteed
// that the log file exists or that the service logs to this file.
func (i Runit) ServiceLogs(ctx context.Context, h cmd.ContextRunner, s string, lines int) ([]string, error) {
	out, err := h.ExecOutputContext(ctx, sh.Command("tail", "-n", strconv.Itoa(lines), "/var/log/"+s+"/current"))
	if err != nil {
		return nil, fmt.Errorf("failed to get logs for service %s: %w", s, err)
	}
	return strings.Split(out, "\n"), nil
}

// RegisterRunit register runit in a repository.
func RegisterRunit(repo *Provider) {
	repo.Register(func(c cmd.ContextRunner) (ServiceManager, bool) {
		if c.IsWindows() {
			return nil, false
		}
		// Checking for 'runit' command presence
		if c.ExecContext(context.Background(), "command -v runit > /dev/null 2>&1") != nil {
			return nil, false
		}
		// Checking for 'sv' command presence
		if c.ExecContext(context.Background(), "command -v sv > /dev/null 2>&1") != nil {
			return nil, false
		}
		return Runit{}, true
	})
}
