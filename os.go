package rig

import (
	"fmt"

	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/os"
)

// GetOS returns the remote host's operating system information from the default provider.
func GetOS(runner exec.SimpleRunner) (*os.Release, error) {
	os, err := os.DefaultProvider.Get(runner)
	if err != nil {
		return nil, fmt.Errorf("get os release: %w", err)
	}
	return os, nil
}
