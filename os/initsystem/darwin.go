package initsystem

import (
	"fmt"
)

type Darwin struct {
	Host Host
}

// StartService starts a a service
func (i *Darwin) StartService(s string) error {
	return i.Host.Execf(`sudo launchctl start %s`, s)
}

// StopService stops a a service
func (i *Darwin) StopService(s string) error {
	return i.Host.Execf(`sudo launchctl stop %s`, s)
}

// ServiceScriptPath returns the path to a service configuration file
func (i *Darwin) ServiceScriptPath(s string) (string, error) {
	return "", fmt.Errorf("not available on mac")
}

// RestartService restarts a a service
func (i *Darwin) RestartService(s string) error {
	return i.Host.Execf(`sudo launchctl kickstart -k %s`, s)
}

// Reload reloads init system configuration
func (i *Darwin) Reload() error {
	return nil
}

// EnableService enables a a service
func (i *Darwin) EnableService(s string) error {
	return i.Host.Execf(`sudo launchctl enable %s`, s)
}

// DisableService disables a a service
func (i *Darwin) DisableService(s string) error {
	return i.Host.Execf(`sudo launchctl disable %s`, s)
}

// ServiceIsRunning returns true if a service is running
func (i *Darwin) ServiceIsRunning(s string) bool {
	return i.Host.Execf(`sudo launchctl list %s | grep -q '"PID"'`, s) == nil
}

func (i *Darwin) RebootCommand() string {
	return "sudo reboot"
}
