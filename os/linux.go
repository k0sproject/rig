package os

import (
	"fmt"
	"strings"
)

type Linux struct {
	Host Host
}

func (c *Linux) Kind() string {
	return "linux"
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
	pwd, err := c.Host.ExecWithOutput("pwd")
	if err != nil {
		return ""
	}
	return pwd
}

// JoinPath joins a path
func (c *Linux) JoinPath(parts ...string) string {
	return strings.Join(parts, "/")
}
