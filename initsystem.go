package rig

import (
	"fmt"

	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/initsystem"
)

// GetServiceManager returns a ServiceManager for the current system from the default init system providers.
func GetServiceManager(runner exec.ContextRunner) (initsystem.ServiceManager, error) {
	mgr, err := initsystem.DefaultProvider.Get(runner)
	if err != nil {
		return nil, fmt.Errorf("get service manager: %w", err)
	}
	return mgr, nil
}
