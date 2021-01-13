package os

import (
	"fmt"
	"strings"

	ps "github.com/k0sproject/rig/powershell"
)

type Windows struct {
	Host Host
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
