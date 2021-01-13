package initsystem

type OpenRC struct {
	Host Host
}

// StartService starts a a service
func (i *OpenRC) StartService(s string) error {
	return i.Host.Execf("sudo rc-service %s start", s)
}

// StopService stops a a service
func (i *OpenRC) StopService(s string) error {
	return i.Host.Execf("sudo rc-service %s stop", s)
}

// ScriptPath returns the path to a service configuration file
func (i *OpenRC) ServiceScriptPath(s string) (string, error) {
	return i.Host.ExecWithOutputf("sudo rc-service -r %s 2> /dev/null", s)
}

// RestartService restarts a a service
func (i *OpenRC) RestartService(s string) error {
	return i.Host.Execf("sudo rc-service %s restart", s)
}

// Reload reloads init system configuration
func (i *OpenRC) Reload() error {
	return nil
}

// EnableService enables a a service
func (i *OpenRC) EnableService(s string) error {
	return i.Host.Execf("sudo rc-update add %s", s)
}

// DisableService disables a a service
func (i *OpenRC) DisableService(s string) error {
	return i.Host.Execf("sudo rc-update del %s", s)
}

// ServiceIsRunning returns true if a service is running
func (i *OpenRC) ServiceIsRunning(s string) bool {
	return i.Host.Execf(`sudo rc-service %s status | grep -q "status: started"`, s) == nil
}

func (i *OpenRC) RebootCommand() string {
	return "sudo shutdown -r now"
}
