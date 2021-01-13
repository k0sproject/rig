package os

// InitSystem interface defines an init system - the OS's system to manage services (systemd, openrc for example)
type InitSystem interface {
	StartService(string) error
	StopService(string) error
	RestartService(string) error
	DisableService(string) error
	EnableService(string) error
	ServiceIsRunning(string) bool
	ServiceScriptPath(string) (string, error)
	Reload() error
}
