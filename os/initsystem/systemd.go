package initsystem

// Systemd is found by default on most linux distributions today
type Systemd struct {
	Host host
}

// StartService starts a a service
func (i *Systemd) StartService(s string) error {
	return i.Host.Execf("sudo systemctl start %s", s)
}

// EnableService enables a a service
func (i *Systemd) EnableService(s string) error {
	return i.Host.Execf("sudo systemctl enable %s", s)
}

// DisableService disables a a service
func (i *Systemd) DisableService(s string) error {
	return i.Host.Execf("sudo systemctl disable %s", s)
}

// StopService stops a a service
func (i *Systemd) StopService(s string) error {
	return i.Host.Execf("sudo systemctl stop %s", s)
}

// RestartService restarts a a service
func (i *Systemd) RestartService(s string) error {
	return i.Host.Execf("sudo systemctl restart %s", s)
}

// DaemonReload reloads init system configuration
func (i *Systemd) DaemonReload() error {
	return i.Host.Execf("sudo systemctl daemon-reload")
}

// ServiceIsRunning returns true if a service is running
func (i *Systemd) ServiceIsRunning(s string) bool {
	return i.Host.Execf(`sudo systemctl status %s | grep -q "(running)"`, s) == nil
}

// ServiceScriptPath returns the path to a service configuration file
func (i *Systemd) ServiceScriptPath(s string) (string, error) {
	return i.Host.ExecOutputf(`systemctl show -p FragmentPath %s.service 2> /dev/null | cut -d"=" -f2`, s)
}
