package initsystem

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"github.com/alessio/shellescape"
	"github.com/k0sproject/rig/exec"
)

// Runit is an init system implementation for runit
type Runit struct{}

// StartService starts a runit service
func (i Runit) StartService(ctx context.Context, h exec.ContextRunner, s string) error {
	if err := h.ExecContext(ctx, "sv start %s", shellescape.Quote(s)); err != nil {
		return fmt.Errorf("failed to start service %s: %w", s, err)
	}
	return nil
}

// StopService stops a runit service
func (i Runit) StopService(ctx context.Context, h exec.ContextRunner, s string) error {
	if err := h.ExecContext(ctx, "sv stop %s", shellescape.Quote(s)); err != nil {
		return fmt.Errorf("failed to stop service %s: %w", s, err)
	}
	return nil
}

// RestartService restarts a runit service
func (i Runit) RestartService(ctx context.Context, h exec.ContextRunner, s string) error {
	if err := h.ExecContext(ctx, "sv restart %s", shellescape.Quote(s)); err != nil {
		return fmt.Errorf("failed to restart service %s: %w", s, err)
	}
	return nil
}

// ServiceIsRunning returns true if a runit service is running
func (i Runit) ServiceIsRunning(ctx context.Context, h exec.ContextRunner, s string) bool {
	return h.ExecContext(ctx, "sv status %s | grep -q 'run: '", shellescape.Quote(s)) == nil
}

// ServiceScriptPath returns the path to a runit service script
func (i Runit) ServiceScriptPath(_ context.Context, _ exec.ContextRunner, s string) (string, error) {
	serviceDir := "/etc/service"
	return path.Join(serviceDir, s), nil
}

// EnableService creates a symlink in the runit service directory to enable a service
func (i Runit) EnableService(ctx context.Context, h exec.ContextRunner, s string) error {
	serviceDir := "/etc/service"                      // Adjust as necessary
	runScriptPath := fmt.Sprintf("/etc/sv/%s/run", s) // Adjust the path to run scripts as necessary
	symlinkPath := path.Join(serviceDir, s)
	if err := h.ExecContext(ctx, "ln -s %s %s", shellescape.Quote(runScriptPath), shellescape.Quote(symlinkPath)); err != nil {
		return fmt.Errorf("failed to enable service %s: %w", s, err)
	}
	return nil
}

// DisableService removes the symlink from the runit service directory to disable a service
func (i Runit) DisableService(ctx context.Context, h exec.ContextRunner, s string) error {
	symlinkPath := filepath.Join("/etc/service", s) // Adjust as necessary
	if err := h.ExecContext(ctx, "rm -f %s", shellescape.Quote(symlinkPath)); err != nil {
		return fmt.Errorf("failed to disable service %s: %w", s, err)
	}
	return nil
}

// ServiceLogs returns the logs for a runit service from /var/log/<service>/current. It's not guaranteed
// that the log file exists or that the service logs to this file.
func (i Runit) ServiceLogs(ctx context.Context, h exec.ContextRunner, s string, lines int) ([]string, error) {
	out, err := h.ExecOutputContext(ctx, "tail -n %[1]d %s", lines, shellescape.Quote("/var/log/"+s+"/current"))
	if err != nil {
		return nil, fmt.Errorf("failed to get logs for service %s: %w", s, err)
	}
	return strings.Split(out, "\n"), nil
}

// RegisterRunit register runit in a repository
func RegisterRunit(repo *Provider) {
	repo.Register(func(c exec.ContextRunner) ServiceManager {
		if c.IsWindows() {
			return nil
		}
		// Checking for 'runit' command presence
		if c.ExecContext(context.Background(), "command -v runit > /dev/null 2>&1") != nil {
			return nil
		}
		return Runit{}
	})
}
