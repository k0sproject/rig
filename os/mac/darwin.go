package darwin

import (
	"fmt"
	"strings"

	"github.com/k0sproject/rig"
	"github.com/k0sproject/rig/os"
	"github.com/k0sproject/rig/os/registry"
)

// Darwin provides OS support for macOS Darwin
type Darwin struct {
	os.Linux
}

// Kind returns "darwin"
func (c *Darwin) Kind() string {
	return "darwin"
}

// StartService starts a a service
func (c *Darwin) StartService(s string) error {
	return c.Host.Execf(`sudo launchctl start %s`, s)
}

// StopService stops a a service
func (c *Darwin) StopService(s string) error {
	return c.Host.Execf(`sudo launchctl stop %s`, s)
}

// ServiceScriptPath returns the path to a service configuration file
func (c *Darwin) ServiceScriptPath(s string) (string, error) {
	return "", fmt.Errorf("not available on mac")
}

// RestartService restarts a a service
func (c *Darwin) RestartService(s string) error {
	return c.Host.Execf(`sudo launchctl kickstart -k %s`, s)
}

// DaemonReload reloads init system configuration
func (c *Darwin) DaemonReload() error {
	return nil
}

// EnableService enables a a service
func (c *Darwin) EnableService(s string) error {
	return c.Host.Execf(`sudo launchctl enable %s`, s)
}

// DisableService disables a a service
func (c *Darwin) DisableService(s string) error {
	return c.Host.Execf(`sudo launchctl disable %s`, s)
}

// ServiceIsRunning returns true if a service is running
func (c *Darwin) ServiceIsRunning(s string) bool {
	return c.Host.Execf(`sudo launchctl list %s | grep -q '"PID"'`, s) == nil
}

// InstallPackage installs a package using brew
func (c *Darwin) InstallPackage(s ...string) error {
	return c.Host.Execf("brew install %s", strings.Join(s, " "))
}

func init() {
	registry.RegisterOSModule(
		func(os rig.OSVersion) bool {
			return os.ID == "darwin"
		},
		func(h os.Host) interface{} {
			return &Darwin{
				Linux: os.Linux{
					Host: h,
				},
			}
		},
	)
}
