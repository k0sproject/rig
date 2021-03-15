package initsystem

// Systemd is found by default on most linux distributions today
type Systemd struct{}

// StartService starts a a service
func (i Systemd) StartService(h Host, s string) error {
	return h.Execf("sudo -i systemctl start %s 2> /dev/null", s)
}

// EnableService enables a a service
func (i Systemd) EnableService(h Host, s string) error {
	return h.Execf("sudo -i systemctl enable %s 2> /dev/null", s)
}

// DisableService disables a a service
func (i Systemd) DisableService(h Host, s string) error {
	return h.Execf("sudo -i systemctl disable %s 2> /dev/null", s)
}

// StopService stops a a service
func (i Systemd) StopService(h Host, s string) error {
	return h.Execf("sudo -i systemctl stop %s 2> /dev/null", s)
}

// RestartService restarts a a service
func (i Systemd) RestartService(h Host, s string) error {
	return h.Execf("sudo -i systemctl restart %s 2> /dev/null", s)
}

// DaemonReload reloads init system configuration
func (i Systemd) DaemonReload(h Host) error {
	return h.Execf("sudo -i systemctl daemon-reload 2> /dev/null")
}

// ServiceIsRunning returns true if a service is running
func (i Systemd) ServiceIsRunning(h Host, s string) bool {
	return h.Execf(`sudo -i systemctl status %s 2> /dev/null | grep -q "(running)"`, s) == nil
}

// ServiceScriptPath returns the path to a service configuration file
func (i Systemd) ServiceScriptPath(h Host, s string) (string, error) {
	return h.ExecOutputf(`sudo -i systemctl show -p FragmentPath %s.service 2> /dev/null | cut -d"=" -f2`, s)
}
