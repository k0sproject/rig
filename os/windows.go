package os

import (
	"fmt"
	"strings"

	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/log"
	ps "github.com/k0sproject/rig/powershell"
)

// Windows is the base packge for windows OS support
type Windows struct {
	Host Host
}

// Kind returns "windows"
func (c *Windows) Kind() string {
	return "windows"
}

const privCheck = `"$currentPrincipal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent()); if (!$currentPrincipal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) { $host.SetShouldExit(1) }"`

// CheckPrivilege returns an error if the user does not have admin access to the host
func (c *Windows) CheckPrivilege() error {
	if c.Host.Exec(ps.Cmd(privCheck)) != nil {
		return fmt.Errorf("user does not have administrator rights on the host")
	}

	return nil
}

// InstallPackage enables an optional windows feature
func (c *Windows) InstallPackage(s ...string) error {
	for _, n := range s {
		err := c.Host.Exec(ps.Cmd(fmt.Sprintf("Enable-WindowsOptionalFeature -Online -FeatureName %s -All", n)))
		if err != nil {
			return err
		}
	}

	return nil
}

// Pwd returns the current working directory
func (c *Windows) Pwd() string {
	pwd, err := c.Host.ExecOutput("echo %cd%")
	if err != nil {
		return ""
	}
	return pwd
}

// JoinPath joins a path
func (c *Windows) JoinPath(parts ...string) string {
	return strings.Join(parts, "\\")
}

// Hostname resolves the short hostname
func (c *Windows) Hostname() string {
	output, err := c.Host.ExecOutput("powershell $env:COMPUTERNAME")
	if err != nil {
		return "localhost"
	}
	return strings.TrimSpace(output)
}

// LongHostname resolves the FQDN (long) hostname
func (c *Windows) LongHostname() string {
	output, err := c.Host.ExecOutput("powershell ([System.Net.Dns]::GetHostByName(($env:COMPUTERNAME))).Hostname")
	if err != nil {
		return "localhost.localdomain"
	}
	return strings.TrimSpace(output)
}

// IsContainer returns true if the host is actually a container (always false on windows for now)
func (c *Windows) IsContainer() bool {
	return false
}

// FixContainer makes a container work like a real host (does nothing on windows for now)
func (c *Windows) FixContainer() error {
	return nil
}

// SELinuxEnabled is true when SELinux is enabled (always false on windows for now)
func (c *Windows) SELinuxEnabled() bool {
	return false
}

// WriteFile writes file to host with given contents. Do not use for large files.
// The permissions argument is ignored on windows.
func (c *Windows) WriteFile(path string, data string, permissions string) error {
	if data == "" {
		return fmt.Errorf("empty content in WriteFile to %s", path)
	}

	if path == "" {
		return fmt.Errorf("empty path in WriteFile")
	}

	tempFile, err := c.Host.ExecOutput("powershell -Command \"New-TemporaryFile | Write-Host\"")
	if err != nil {
		return err
	}
	defer c.deleteTempFile(tempFile)

	err = c.Host.Exec(fmt.Sprintf(`powershell -Command "$Input | Out-File -FilePath %s"`, ps.SingleQuote(tempFile)), exec.Stdin(data), exec.RedactString(data))
	if err != nil {
		return err
	}

	err = c.Host.Exec(fmt.Sprintf(`powershell -Command "Move-Item -Force -Path %s -Destination %s"`, ps.SingleQuote(tempFile), ps.SingleQuote(path)))
	if err != nil {
		return err
	}

	return nil
}

// ReadFile reads a files contents from the host.
func (c *Windows) ReadFile(path string) (string, error) {
	return c.Host.ExecOutput(fmt.Sprintf(`type %s`, ps.DoubleQuote(path)), exec.HideOutput())
}

// DeleteFile deletes a file from the host.
func (c *Windows) DeleteFile(path string) error {
	return c.Host.Exec(fmt.Sprintf(`del /f %s`, ps.DoubleQuote(path)))
}

func (c *Windows) deleteTempFile(path string) {
	if err := c.DeleteFile(path); err != nil {
		log.Debugf("failed to delete temporary file %s: %s", path, err.Error())
	}
}

// FileExist checks if a file exists on the host
func (c *Windows) FileExist(path string) bool {
	return c.Host.Exec(fmt.Sprintf(`powershell -Command "if (!(Test-Path -Path \"%s\")) { exit 1 }"`, path)) == nil
}

// UpdateEnvironment updates the hosts's environment variables
func (c *Windows) UpdateEnvironment(env map[string]string) error {
	for k, v := range env {
		err := c.Host.Exec(fmt.Sprintf(`setx %s %s`, ps.DoubleQuote(k), ps.DoubleQuote(v)))
		if err != nil {
			return err
		}
	}
	return nil
}

// CleanupEnvironment removes environment variable configuration
func (c *Windows) CleanupEnvironment(env map[string]string) error {
	for k := range env {
		err := c.Host.Exec(fmt.Sprintf(`powershell "[Environment]::SetEnvironmentVariable(%s, $null, 'User')"`, ps.SingleQuote(k)))
		if err != nil {
			return err
		}
		err = c.Host.Exec(fmt.Sprintf(`powershell "[Environment]::SetEnvironmentVariable(%s, $null, 'Machine')"`, ps.SingleQuote(k)))
		if err != nil {
			return err
		}
	}
	return nil
}

// CommandExist returns true if the provided command exists
func (c *Windows) CommandExist(cmd string) bool {
	return c.Host.Execf("where /q %s", cmd) == nil
}

// Reboot executes the reboot command
func (c *Windows) Reboot() error {
	return c.Host.Exec("shutdown /r")
}

// StartService starts a a service
func (c *Windows) StartService(s string) error {
	return c.Host.Execf(`sc start "%s"`, s)
}

// StopService stops a a service
func (c *Windows) StopService(s string) error {
	return c.Host.Execf(`sc stop "%s"`, s)
}

// ServiceScriptPath returns the path to a service configuration file
func (c *Windows) ServiceScriptPath(s string) (string, error) {
	return "", fmt.Errorf("not available on windows")
}

// RestartService restarts a a service
func (c *Windows) RestartService(s string) error {
	return c.Host.Execf(ps.Cmd(fmt.Sprintf(`Restart-Service "%s"`, s)))
}

// DaemonReload reloads init system configuration
func (c *Windows) DaemonReload() error {
	return nil
}

// EnableService enables a a service
func (c *Windows) EnableService(s string) error {
	return c.Host.Execf(`sc.exe config "%s" start=disabled`, s)
}

// DisableService disables a a service
func (c *Windows) DisableService(s string) error {
	return c.Host.Execf(`sc.exe config "%s" start=enabled`, s)
}

// ServiceIsRunning returns true if a service is running
func (c *Windows) ServiceIsRunning(s string) bool {
	return c.Host.Execf(`sc.exe query "%s" | findstr "RUNNING"`, s) == nil
}
