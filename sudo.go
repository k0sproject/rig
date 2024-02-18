package rig

import (
	"fmt"

	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/protocol"
	"github.com/k0sproject/rig/sudo"
)

// GetSudoRunner returns a new runner that uses sudo to execute commands.
func GetSudoRunner(conn protocol.Connection) (exec.Runner, error) {
	runner := exec.NewHostRunner(conn)
	sudoR, err := sudo.DefaultProvider.Get(runner)
	if err != nil {
		return nil, fmt.Errorf("get sudo runner: %w", err)
	}
	return sudoR, nil
}
