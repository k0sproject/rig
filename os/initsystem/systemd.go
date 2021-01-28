package initsystem

// Systemd is found by default on most linux distributions today
type Systemd struct{}

// StartService starts a a service
func (i Systemd) StartService(h Host, s string) error {
	return h.Execf("sudo systemctl start %s", s)
}

// EnableService enables a a service
func (i Systemd) EnableService(h Host, s string) error {
	return h.Execf("sudo systemctl enable %s", s)
}

// DisableService disables a a service
func (i Systemd) DisableService(h Host, s string) error {
	return h.Execf("sudo systemctl disable %s", s)
}

// StopService stops a a service
func (i Systemd) StopService(h Host, s string) error {
	return h.Execf("sudo systemctl stop %s", s)
}

// RestartService restarts a a service
func (i Systemd) RestartService(h Host, s string) error {
	return h.Execf("sudo systemctl restart %s", s)
}

// DaemonReload reloads init system configuration
func (i Systemd) DaemonReload(h Host) error {
	return h.Execf("sudo systemctl daemon-reload")
}

// ServiceIsRunning returns true if a service is running
func (i Systemd) ServiceIsRunning(h Host, s string) bool {
	return h.Execf(`sudo systemctl status %s | grep -q "(running)"`, s) == nil
}

// ServiceScriptPath returns the path to a service configuration file
func (i Systemd) ServiceScriptPath(h Host, s string) (string, error) {
	return h.ExecOutputf(`systemctl show -p FragmentPath %s.service 2> /dev/null | cut -d"=" -f2`, s)
}
