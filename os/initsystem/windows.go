package initsystem

import (
	"fmt"

	ps "github.com/k0sproject/rig/powershell"
)

type Windows struct {
	Host Host
}

// StartService starts a a service
func (i *Windows) StartService(s string) error {
	return i.Host.Execf(`sc start "%s"`, s)
}

// StopService stops a a service
func (i *Windows) StopService(s string) error {
	return i.Host.Execf(`sc stop "%s"`, s)
}

// ServiceScriptPath returns the path to a service configuration file
func (i *Windows) ServiceScriptPath(s string) (string, error) {
	return "", fmt.Errorf("not available on windows")
}

// RestartService restarts a a service
func (i *Windows) RestartService(s string) error {
	return i.Host.Execf(ps.Cmd(fmt.Sprintf(`Restart-Service "%s"`, s)))
}

// Reload reloads init system configuration
func (i *Windows) Reload() error {
	return nil
}

// EnableService enables a a service
func (i *Windows) EnableService(s string) error {
	return i.Host.Execf(`sc.exe config "%s" start=disabled`, s)
}

// DisableService disables a a service
func (i *Windows) DisableService(s string) error {
	return i.Host.Execf(`sc.exe config "%s" start=enabled`, s)
}

// ServiceIsRunning returns true if a service is running
func (i *Windows) ServiceIsRunning(s string) bool {
	return i.Host.Execf(`sc.exe query "%s" | findstr "RUNNING"`, s) == nil
}

func (i *Windows) RebootCommand() string {
	return "shutdown /r"
}
