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
	isys initSystem
}

// initSystem interface defines an init system - the OS's system to manage services (systemd, openrc for example)
type initSystem interface {
	StartService(initsystem.Host, string) error
	StopService(initsystem.Host, string) error
	RestartService(initsystem.Host, string) error
	DisableService(initsystem.Host, string) error
	EnableService(initsystem.Host, string) error
	ServiceIsRunning(initsystem.Host, string) bool
	ServiceScriptPath(initsystem.Host, string) (string, error)
	DaemonReload(initsystem.Host) error
}

// Kind returns "linux"
func (c Linux) Kind() string {
	return "linux"
}

// memoizing accessor to the init system (systemd, openrc)
func (c Linux) is(h Host) initSystem {
	if c.isys == nil {
		if h.Exec("command -v systemctl > /dev/null 2&>1", exec.Sudo(h)) == nil {
			c.isys = &initsystem.Systemd{}
		} else if h.Exec("command -v rc-service > /dev/null 2&>1", exec.Sudo(h)) == nil {
			c.isys = &initsystem.OpenRC{}
		}
	}

	return c.isys
}

// StartService starts a service on the host
func (c Linux) StartService(h Host, s string) error {
	return c.is(h).StartService(h, s)
}

// StopService stops a service on the host
func (c Linux) StopService(h Host, s string) error {
	return c.is(h).StopService(h, s)
}

// RestartService restarts a service on the host
func (c Linux) RestartService(h Host, s string) error {
	return c.is(h).RestartService(h, s)
}

// DisableService disables a service on the host
func (c Linux) DisableService(h Host, s string) error {
	return c.is(h).DisableService(h, s)
}

// EnableService enables a service on the host
func (c Linux) EnableService(h Host, s string) error {
	return c.is(h).EnableService(h, s)
}

// ServiceIsRunning returns true if the service is running on the host
func (c Linux) ServiceIsRunning(h Host, s string) bool {
	return c.is(h).ServiceIsRunning(h, s)
}

// ServiceScriptPath returns the service definition file path on the host
func (c Linux) ServiceScriptPath(h Host, s string) (string, error) {
	return c.is(h).ServiceScriptPath(h, s)
}

// DaemonReload performs an init system config reload
func (c Linux) DaemonReload(h Host) error {
	return c.is(h).DaemonReload(h)
}

// Pwd returns the current working directory of the session
func (c Linux) Pwd(h Host) string {
	pwd, err := h.ExecOutput("pwd 2> /dev/null")
	if err != nil {
		return ""
	}
	return pwd
}

// JoinPath joins a path
func (c Linux) JoinPath(parts ...string) string {
	return strings.Join(parts, "/")
}

// Hostname resolves the short hostname
func (c Linux) Hostname(h Host) string {
	n, _ := h.ExecOutput("hostname -s 2> /dev/null")

	return n
}

// LongHostname resolves the FQDN (long) hostname
func (c Linux) LongHostname(h Host) string {
	n, _ := h.ExecOutput("hostname 2> /dev/null")

	return n
}

// IsContainer returns true if the host is actually a container
func (c Linux) IsContainer(h Host) bool {
	return h.Exec("grep 'container=docker' /proc/1/environ 2> /dev/null") == nil
}

// FixContainer makes a container work like a real host
func (c Linux) FixContainer(h Host) error {
	return h.Exec("mount --make-rshared / 2> /dev/null", exec.Sudo(h))
}

// SELinuxEnabled is true when SELinux is enabled
func (c Linux) SELinuxEnabled(h Host) bool {
	return h.Exec("getenforce | grep -iq enforcing 2> /dev/null", exec.Sudo(h)) == nil
}

// WriteFile writes file to host with given contents. Do not use for large files.
func (c Linux) WriteFile(h Host, path string, data string, permissions string) error {
	if data == "" {
		return fmt.Errorf("empty content in WriteFile to %s", path)
	}

	if path == "" {
		return fmt.Errorf("empty path in WriteFile")
	}

	tempFile, err := h.ExecOutput("mktemp 2> /dev/null")
	if err != nil {
		return err
	}
	tempFile = escape.Quote(tempFile)

	installCmd, err := h.Sudo(fmt.Sprintf("install -D -m %s %s %s", permissions, tempFile, path))
	if err != nil {
		return err
	}

	return h.Execf(`cat > %s && (%s || (rm %s; exit 1))`, tempFile, installCmd, tempFile, exec.Stdin(data), exec.RedactString(data))
}

// ReadFile reads a files contents from the host.
func (c Linux) ReadFile(h Host, path string) (string, error) {
	return h.ExecOutputf("cat %s 2> /dev/null", escape.Quote(path), exec.HideOutput(), exec.Sudo(h))
}

// DeleteFile deletes a file from the host.
func (c Linux) DeleteFile(h Host, path string) error {
	return h.Execf(`rm -f %s 2> /dev/null`, escape.Quote(path), exec.Sudo(h))
}

// FileExist checks if a file exists on the host
func (c Linux) FileExist(h Host, path string) bool {
	return h.Execf(`test -e %s 2> /dev/null`, escape.Quote(path), exec.Sudo(h)) == nil
}

// LineIntoFile tries to find a matching line in a file and replace it with a new entry
// TODO refactor this into go because it's too magical.
func (c Linux) LineIntoFile(h Host, path, matcher, newLine string) error {
	if c.FileExist(h, path) {
		err := h.Exec(fmt.Sprintf(`/bin/bash -c -- 'file=%s; match=%s; line=%s; grep -q "${match}" "$file" && sed -i "/${match}/c ${line}" "$file" || (echo "$line" | tee -a "$file" > /dev/null)'`, escape.Quote(path), escape.Quote(matcher), escape.Quote(newLine)))
		if err != nil {
			return err
		}
		return nil
	}
	return c.WriteFile(h, path, newLine, "0700")
}

// UpdateEnvironment updates the hosts's environment variables
func (c Linux) UpdateEnvironment(h Host, env map[string]string) error {
	for k, v := range env {
		err := c.LineIntoFile(h, "/etc/environment", fmt.Sprintf("^%s=", k), fmt.Sprintf("%s=%s", k, v))
		if err != nil {
			return err
		}
	}

	// Update current session environment from the /etc/environment
	return h.Exec(`while read -r pair; do if [[ $pair == ?* && $pair != \#* ]]; then export "$pair" || exit 2; fi; done < /etc/environment`)
}

// CleanupEnvironment removes environment variable configuration
func (c Linux) CleanupEnvironment(h Host, env map[string]string) error {
	for k := range env {
		err := c.LineIntoFile(h, "/etc/environment", fmt.Sprintf("^%s=", k), "")
		if err != nil {
			return err
		}
	}
	// remove empty lines
	return h.Exec(`sed -i '/^$/d' /etc/environment`, exec.Sudo(h))
}

// CommandExist returns true if the command exists
func (c Linux) CommandExist(h Host, cmd string) bool {
	return h.Execf(`command -v "%s" 2> /dev/null`, cmd, exec.Sudo(h)) == nil
}

// Reboot executes the reboot command
func (c Linux) Reboot(h Host) error {
	cmd, err := h.Sudo("shutdown --reboot 0 2> /dev/null")
	if err != nil {
		return err
	}
	return h.Execf("%s && exit", cmd)
}
