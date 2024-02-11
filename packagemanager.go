package rig

import (
	"fmt"

	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/packagemanager"
)

// GetPackageManager returns a package manager from the default provider.
func GetPackageManager(runner exec.ContextRunner) (packagemanager.PackageManager, error) {
	pm, err := packagemanager.DefaultProvider.Get(runner)
	if err != nil {
		return nil, fmt.Errorf("get package manager: %w", err)
	}
	return pm, nil
}
