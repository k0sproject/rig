package rig

import (
	"fmt"

	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/initsystem"
	"github.com/k0sproject/rig/os"
	"github.com/k0sproject/rig/packagemanager"
	"github.com/k0sproject/rig/protocol"
	"github.com/k0sproject/rig/remotefs"
	"github.com/k0sproject/rig/sudo"
)

// Some of the constructors, errors and types from subpackages are aliased here to make it
// easier to consume them without importing more packages.

var (
	// ErrAbort is returned when retrying an action will not yield a different outcome.
	ErrAbort = protocol.ErrAbort

	// ErrValidationFailed is returned when a validation check fails.
	ErrValidationFailed = protocol.ErrValidationFailed
)

// PackageManagerService is a type alias for packagemanager.Service.
type PackageManagerService = packagemanager.Service

// InitSystemService is a type alias for initsystem.Service.
type InitSystemService = initsystem.Service

// RemoteFSService is a type alias for remotefs.Service.
type RemoteFSService = remotefs.Service

// OSReleaseService is a type alias for os.Service.
type OSReleaseService = os.Service

// SudoService is a type alias for sudo.Service.
type SudoService = sudo.Service

// GetRemoteFS returns a new remote FS instance from the default remote.FS provider.
func GetRemoteFS(runner exec.Runner) (remotefs.FS, error) {
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

// GetOSRelease returns the remote host's operating system information from the default provider.
func GetOSRelease(runner exec.SimpleRunner) (*os.Release, error) {
	os, err := os.DefaultProvider().Get(runner)
	if err != nil {
		return nil, fmt.Errorf("get os release: %w", err)
	}
	return os, nil
}

// NewRunner returns a new exec.Runner for the connection.
// Currently the error is always nil.
func NewRunner(conn protocol.Connection) (exec.Runner, error) {
	return exec.NewHostRunner(conn), nil
}
