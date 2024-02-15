package rig

import (
	"fmt"

	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/remotefs"
)

// GetFS returns a new remote FS instance from the default remote.FS provider.
func GetFS(runner exec.Runner) (remotefs.FS, error) {
	fs, err := remotefs.DefaultProvider.Get(runner)
	if err != nil {
		return nil, fmt.Errorf("get FS: %w", err)
	}
	return fs, nil
}
