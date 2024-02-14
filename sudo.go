package rig

import (
	"fmt"

	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/sudo"
)

// GetSudoDecorator returns a sudo decorator for the given runner from the default sudo provider.
func GetSudoDecorator(runner exec.SimpleRunner) (exec.DecorateFunc, error) {
	de, err := sudo.DefaultProvider.Get(runner)
	if err != nil {
		return nil, fmt.Errorf("get sudo decorator: %w", err)
	}
	return de, nil
}

// GetSudoRunner returns a new runner that uses sudo to execute commands.
func GetSudoRunner(client Connection) (*exec.HostRunner, error) {
	runner := exec.NewHostRunner(client)
	de, err := GetSudoDecorator(runner)
	if err != nil {
		return nil, fmt.Errorf("get sudo runner: %w", err)
	}
	return exec.NewHostRunner(client, de), nil
}
