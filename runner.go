package rig

import (
	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/protocol"
)

// NewRunner returns a new exec.Runner for the given client.
// Currently the error is always nil.
func NewRunner(conn protocol.Connection) (exec.Runner, error) {
	return exec.NewHostRunner(conn), nil
}
