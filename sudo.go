package rig

import (
	"fmt"

	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/sudo"
)

func GetSudoDecorator(runner exec.SimpleRunner) (exec.DecorateFunc, error) {
	de, err := sudo.DefaultProvider.Get(runner)
	if err != nil {
		return nil, fmt.Errorf("get sudo decorator: %w", err)
	}
	return de, nil
}

func GetSudoRunner(client Client) (*exec.HostRunner, error) {
	runner := exec.NewHostRunner(client)
	de, err := GetSudoDecorator(runner)
	if err != nil {
		return nil, fmt.Errorf("get sudo runner: %w", err)
	}
	return exec.NewHostRunner(client, de), nil
}
