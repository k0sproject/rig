package rig

import "github.com/k0sproject/rig/exec"

// NewRunner returns a new exec.Runner for the given client.
// Currently the error is always nil.
func NewRunner(client Client) (*exec.HostRunner, error) {
	return exec.NewHostRunner(client), nil
}
