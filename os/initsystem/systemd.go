package initsystem

import (
	"fmt"
	"path"
	"strconv"
	"strings"

	"github.com/k0sproject/rig/exec"
)

// Systemd is found by default on most linux distributions today
type Systemd struct{}

// StartService starts a a service
func (i Systemd) StartService(h Host, s string) error {
	if err := h.Execf("systemctl start %s 2> /dev/null", s, exec.Sudo(h)); err != nil {
		return fmt.Errorf("failed to start service %s: %w", s, err)
	}
	return nil
}

// EnableService enables a a service
func (i Systemd) EnableService(h Host, s string) error {
	if err := h.Execf("systemctl enable %s 2> /dev/null", s, exec.Sudo(h)); err != nil {
		return fmt.Errorf("failed to enable service %s: %w", s, err)
	}
	return nil
}

// DisableService disables a a service
func (i Systemd) DisableService(h Host, s string) error {
	if err := h.Execf("systemctl disable %s 2> /dev/null", s, exec.Sudo(h)); err != nil {
		return fmt.Errorf("failed to disable service %s: %w", s, err)
	}
	return nil
}

// StopService stops a a service
func (i Systemd) StopService(h Host, s string) error {
	if err := h.Execf("systemctl stop %s 2> /dev/null", s, exec.Sudo(h)); err != nil {
		return fmt.Errorf("failed to stop service %s: %w", s, err)
	}
	return nil
}

// RestartService restarts a a service
func (i Systemd) RestartService(h Host, s string) error {
	if err := h.Execf("systemctl restart %s 2> /dev/null", s, exec.Sudo(h)); err != nil {
		return fmt.Errorf("failed to restart service %s: %w", s, err)
	}
	return nil
}

// DaemonReload reloads init system configuration
func (i Systemd) DaemonReload(h Host) error {
	if err := h.Execf("systemctl daemon-reload 2> /dev/null", exec.Sudo(h)); err != nil {
		return fmt.Errorf("failed to daemon-reload: %w", err)
	}
	return nil
}

// ServiceIsRunning returns true if a service is running
func (i Systemd) ServiceIsRunning(h Host, s string) bool {
	return h.Execf(`systemctl status %s 2> /dev/null | grep -q "(running)"`, s, exec.Sudo(h)) == nil
}

// ServiceScriptPath returns the path to a service configuration file
func (i Systemd) ServiceScriptPath(h Host, s string) (string, error) {
	out, err := h.ExecOutputf(`systemctl show -p FragmentPath %s.service 2> /dev/null | cut -d"=" -f2`, s, exec.Sudo(h))
	if err != nil {
		return "", fmt.Errorf("failed to get service %s script path: %w", s, err)
	}
	return strings.TrimSpace(out), nil
}

// ServiceEnvironmentPath returns a path to an environment override file path
func (i Systemd) ServiceEnvironmentPath(h Host, s string) (string, error) {
	sp, err := i.ServiceScriptPath(h, s)
	if err != nil {
		return "", err
	}
	dn := path.Dir(sp)
	return path.Join(dn, fmt.Sprintf("%s.service.d", s), "env.conf"), nil
}

// ServiceEnvironmentContent returns a formatted string for a service environment override file
func (i Systemd) ServiceEnvironmentContent(env map[string]string) string {
	var b strings.Builder
	fmt.Fprintln(&b, "[Service]")
	for k, v := range env {
		env := fmt.Sprintf("%s=%s", k, v)
		_, _ = fmt.Fprintf(&b, "Environment=%s\n", strconv.Quote(env))
	}

	return b.String()
}
