package os

import (
	"fmt"
	"strings"

	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/os/initsystem"
	ps "github.com/k0sproject/rig/powershell"
)

type Windows struct {
	Host       Host
	initSystem InitSystem
}

func (c *Windows) InitSystem() InitSystem {
	if c.initSystem == nil {
		c.initSystem = &initsystem.Windows{Host: c.Host}
	}
	return c.initSystem
}

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
	pwd, err := c.Host.ExecWithOutput("echo %cd%")
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
	output, err := c.Host.ExecWithOutput("powershell $env:COMPUTERNAME")
	if err != nil {
		return "localhost"
	}
	return strings.TrimSpace(output)
}

// LongHostname resolves the FQDN (long) hostname
func (c *Windows) ResolveLongHostname() string {
	output, err := c.Host.ExecWithOutput("powershell ([System.Net.Dns]::GetHostByName(($env:COMPUTERNAME))).Hostname")
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

	tempFile, err := c.Host.ExecWithOutput("powershell -Command \"New-TemporaryFile | Write-Host\"")
	if err != nil {
		return err
	}
	defer c.Host.ExecWithOutput(fmt.Sprintf("del \"%s\"", tempFile))

	err = c.Host.Exec(fmt.Sprintf(`powershell -Command "$Input | Out-File -FilePath %s"`, ps.SingleQuote(tempFile)), exec.Stdin(data))
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
	return c.Host.ExecWithOutput(fmt.Sprintf(`type %s`, ps.DoubleQuote(path)))
}

// DeleteFile deletes a file from the host.
func (c *Windows) DeleteFile(path string) error {
	return c.Host.Exec(fmt.Sprintf(`del /f %s`, ps.DoubleQuote(path)))
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
		c.Host.Exec(fmt.Sprintf(`powershell "[Environment]::SetEnvironmentVariable(%s, $null, 'User')"`, ps.SingleQuote(k)))
		c.Host.Exec(fmt.Sprintf(`powershell "[Environment]::SetEnvironmentVariable(%s, $null, 'Machine')"`, ps.SingleQuote(k)))
	}
	return nil
}

func (c *Windows) CommandExist(cmd string) bool {
	return c.Host.Execf("where /q %s", cmd) == nil
}
