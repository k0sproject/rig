package os

import (
	"fmt"
	"strings"

	escape "github.com/alessio/shellescape"
	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/os/initsystem"
)

// Linux is a base module for various linux OS support packages
type Linux struct {
	Host Host

	isys initSystem
}

// initSystem interface defines an init system - the OS's system to manage services (systemd, openrc for example)
type initSystem interface {
	StartService(string) error
	StopService(string) error
	RestartService(string) error
	DisableService(string) error
	EnableService(string) error
	ServiceIsRunning(string) bool
	ServiceScriptPath(string) (string, error)
	DaemonReload() error
}

// Kind returns "linux"
func (c *Linux) Kind() string {
	return "linux"
}

// memoizing accessor to the init system (systemd, openrc)
func (c *Linux) is() initSystem {
	if c.isys == nil {
		initctl, err := c.Host.ExecOutput("basename $(command -v rc-service systemd)")
		if err != nil {
			return nil
		}
		switch initctl {
		case "systemd":
			c.isys = &initsystem.Systemd{Host: c.Host}
		case "rc-service":
			c.isys = &initsystem.OpenRC{Host: c.Host}
		}
	}

	return c.isys
}

// StartService starts a service on the host
func (c *Linux) StartService(s string) error {
	return c.is().StartService(s)
}

// StopService stops a service on the host
func (c *Linux) StopService(s string) error {
	return c.is().StopService(s)
}

// RestartService restarts a service on the host
func (c *Linux) RestartService(s string) error {
	return c.is().RestartService(s)
}

// DisableService disables a service on the host
func (c *Linux) DisableService(s string) error {
	return c.is().DisableService(s)
}

// EnableService enables a service on the host
func (c *Linux) EnableService(s string) error {
	return c.is().EnableService(s)
}

// ServiceIsRunning returns true if the service is running on the host
func (c *Linux) ServiceIsRunning(s string) bool {
	return c.is().ServiceIsRunning(s)
}

// ServiceScriptPath returns the service definition file path on the host
func (c *Linux) ServiceScriptPath(s string) (string, error) {
	return c.is().ServiceScriptPath(s)
}

// DaemonReload performs an init system config reload
func (c *Linux) DaemonReload() error {
	return c.is().DaemonReload()
}

// CheckPrivilege returns an error if the user does not have passwordless sudo enabled
func (c *Linux) CheckPrivilege() error {
	if c.Host.Exec("sudo -n true") != nil {
		return fmt.Errorf("user does not have passwordless sudo access")
	}

	return nil
}

// Pwd returns the current working directory of the session
func (c *Linux) Pwd() string {
	pwd, err := c.Host.ExecOutput("pwd")
	if err != nil {
		return ""
	}
	return pwd
}

// JoinPath joins a path
func (c *Linux) JoinPath(parts ...string) string {
	return strings.Join(parts, "/")
}

// Hostname resolves the short hostname
func (c *Linux) Hostname() string {
	hostname, _ := c.Host.ExecOutput("hostname -s")

	return hostname
}

// LongHostname resolves the FQDN (long) hostname
func (c *Linux) LongHostname() string {
	longHostname, _ := c.Host.ExecOutput("hostname")

	return longHostname
}

// IsContainer returns true if the host is actually a container
func (c *Linux) IsContainer() bool {
	return c.Host.Exec("grep 'container=docker' /proc/1/environ") == nil
}

// FixContainer makes a container work like a real host
func (c *Linux) FixContainer() error {
	if c.IsContainer() {
		return c.Host.Exec("sudo mount --make-rshared /")
	}
	return nil
}

// SELinuxEnabled is true when SELinux is enabled
func (c *Linux) SELinuxEnabled() bool {
	if output, err := c.Host.ExecOutput("sudo getenforce"); err == nil {
		return strings.ToLower(strings.TrimSpace(output)) == "enforcing"
	}

	return false
}

// WriteFile writes file to host with given contents. Do not use for large files.
func (c *Linux) WriteFile(path string, data string, permissions string) error {
	if data == "" {
		return fmt.Errorf("empty content in WriteFile to %s", path)
	}

	if path == "" {
		return fmt.Errorf("empty path in WriteFile")
	}

	tempFile, err := c.Host.ExecOutput("mktemp")
	if err != nil {
		return err
	}
	tempFile = escape.Quote(tempFile)

	err = c.Host.Exec(fmt.Sprintf("cat > %s && (sudo install -D -m %s %s %s || (rm %s; exit 1))", tempFile, permissions, tempFile, path, tempFile), exec.Stdin(data))
	if err != nil {
		return err
	}
	return nil
}

// ReadFile reads a files contents from the host.
func (c *Linux) ReadFile(path string) (string, error) {
	return c.Host.ExecOutput(fmt.Sprintf("sudo cat %s", escape.Quote(path)))
}

// DeleteFile deletes a file from the host.
func (c *Linux) DeleteFile(path string) error {
	return c.Host.Exec(fmt.Sprintf(`sudo rm -f %s`, escape.Quote(path)))
}

// FileExist checks if a file exists on the host
func (c *Linux) FileExist(path string) bool {
	return c.Host.Exec(fmt.Sprintf(`sudo test -e %s`, escape.Quote(path))) == nil
}

// LineIntoFile tries to find a matching line in a file and replace it with a new entry
// TODO refactor this into go because it's too magical.
func (c *Linux) LineIntoFile(path, matcher, newLine string) error {
	if c.FileExist(path) {
		err := c.Host.Exec(fmt.Sprintf(`file=%s; match=%s; line=%s; sudo grep -q "${match}" "$file" && sudo sed -i "/${match}/c ${line}" "$file" || (echo "$line" | sudo tee -a "$file" > /dev/null)`, escape.Quote(path), escape.Quote(matcher), escape.Quote(newLine)))
		if err != nil {
			return err
		}
		return nil
	}
	return c.WriteFile(path, newLine, "0700")
}

// UpdateEnvironment updates the hosts's environment variables
func (c *Linux) UpdateEnvironment(env map[string]string) error {
	for k, v := range env {
		err := c.LineIntoFile("/etc/environment", fmt.Sprintf("^%s=", k), fmt.Sprintf("%s=%s", k, v))
		if err != nil {
			return err
		}
	}

	// Update current session environment from the /etc/environment
	return c.Host.Exec(`while read -r pair; do if [[ $pair == ?* && $pair != \#* ]]; then export "$pair" || exit 2; fi; done < /etc/environment`)
}

// CleanupEnvironment removes environment variable configuration
func (c *Linux) CleanupEnvironment(env map[string]string) error {
	for k := range env {
		err := c.LineIntoFile("/etc/environment", fmt.Sprintf("^%s=", k), "")
		if err != nil {
			return err
		}
	}
	// remove empty lines
	return c.Host.Exec(`sudo sed -i '/^$/d' /etc/environment`)
}

// CommandExist returns true if the command exists
func (c *Linux) CommandExist(cmd string) bool {
	return c.Host.Execf("sudo command -v %s", cmd) == nil
}

// Reboot executes the reboot command
func (c *Linux) Reboot() error {
	return c.Host.Exec("sudo reboot")
}
