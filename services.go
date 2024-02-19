package rig

import (
	"fmt"

	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/initsystem"
	"github.com/k0sproject/rig/os"
	"github.com/k0sproject/rig/packagemanager"
	"github.com/k0sproject/rig/remotefs"
	"github.com/k0sproject/rig/sudo"
)

// The types here are aliased to make it easier to embed them into your own types
// without having to import the packages individually. Also, as they're all called
// "Service" in their respective packages, you would need to define type aliases
// locally to avoid name collisions.

// PackageManagerService is a type alias for packagemanager.Service
type PackageManagerService = packagemanager.Service

// InitSystemService is a type alias for initsystem.Service
type InitSystemService = initsystem.Service

// RemoteFSService is a type alias for remotefs.Service
type RemoteFSService = remotefs.Service

// OSReleaseService is a type alias for os.Service
type OSReleaseService = os.Service

// SudoService is a type alias for sudo.Service
type SudoService = sudo.Service

// GetFS returns a new remote FS instance from the default remote.FS provider.
func GetFS(runner exec.Runner) (remotefs.FS, error) {
	fs, err := remotefs.DefaultProvider().Get(runner)
	if err != nil {
		return nil, fmt.Errorf("get FS: %w", err)
	}
	return fs, nil
}

// GetServiceManager returns a ServiceManager for the current system from the default init system providers.
func GetServiceManager(runner exec.ContextRunner) (initsystem.ServiceManager, error) {
	mgr, err := initsystem.DefaultProvider().Get(runner)
	if err != nil {
		return nil, fmt.Errorf("get service manager: %w", err)
	}
	return mgr, nil
}

// GetPackageManager returns a package manager from the default provider.
func GetPackageManager(runner exec.ContextRunner) (packagemanager.PackageManager, error) {
	pm, err := packagemanager.DefaultProvider().Get(runner)
	if err != nil {
		return nil, fmt.Errorf("get package manager: %w", err)
	}
	return pm, nil
}

// GetSudoRunner returns a new runner that uses sudo to execute commands.
func GetSudoRunner(runner exec.Runner) (exec.Runner, error) {
	sudoR, err := sudo.DefaultProvider().Get(runner)
	if err != nil {
		return nil, fmt.Errorf("get sudo runner: %w", err)
	}
	return sudoR, nil
}

// GetOS returns the remote host's operating system information from the default provider.
func GetOSRelease(runner exec.SimpleRunner) (*os.Release, error) {
	os, err := os.DefaultProvider().Get(runner)
	if err != nil {
		return nil, fmt.Errorf("get os release: %w", err)
	}
	return os, nil
}
