package initsystem

import (
	"fmt"
	"path"
	"strconv"
	"strings"
)

// OpenRC is found on some linux systems, often installed on Alpine for example.
type OpenRC struct{}

// StartService starts a a service
func (i OpenRC) StartService(h Host, s string) error {
	return h.Execf("sudo rc-service %s start", s)
}

// StopService stops a a service
func (i OpenRC) StopService(h Host, s string) error {
	return h.Execf("sudo rc-service %s stop", s)
}

// ServiceScriptPath returns the path to a service configuration file
func (i OpenRC) ServiceScriptPath(h Host, s string) (string, error) {
	return h.ExecOutputf("sudo rc-service -r %s 2> /dev/null", s)
}

// RestartService restarts a a service
func (i OpenRC) RestartService(h Host, s string) error {
	return h.Execf("sudo rc-service %s restart", s)
}

// DaemonReload reloads init system configuration
func (i OpenRC) DaemonReload(_ Host) error {
	return nil
}

// EnableService enables a a service
func (i OpenRC) EnableService(h Host, s string) error {
	return h.Execf("sudo rc-update add %s", s)
}

// DisableService disables a a service
func (i OpenRC) DisableService(h Host, s string) error {
	return h.Execf("sudo rc-update del %s", s)
}

// ServiceIsRunning returns true if a service is running
func (i OpenRC) ServiceIsRunning(h Host, s string) bool {
	return h.Execf(`sudo rc-service %s status | grep -q "status: started"`, s) == nil
}

// ServiceEnvironmentPath returns a path to an environment override file path
func (i OpenRC) ServiceEnvironmentPath(h Host, s string) (string, error) {
	return path.Join("/etc/conf.d", s), nil
}

// ServiceEnvironmentContent returns a formatted string for a service environment override file
func (i OpenRC) ServiceEnvironmentContent(env map[string]string) string {
	var b strings.Builder
	for k, v := range env {
		_, _ = fmt.Fprintf(&b, `%s=%s\n`, k, strconv.Quote(v))
	}

	return b.String()
}
