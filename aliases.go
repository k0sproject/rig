package rig

import (
	"fmt"

	"github.com/k0sproject/rig/v2/cmd"
	"github.com/k0sproject/rig/v2/initsystem"
	"github.com/k0sproject/rig/v2/os"
	"github.com/k0sproject/rig/v2/packagemanager"
	"github.com/k0sproject/rig/v2/protocol"
	"github.com/k0sproject/rig/v2/remotefs"
	"github.com/k0sproject/rig/v2/sudo"
)

// Some of the constructors, errors and types from subpackages are aliased here to make it
// easier to consume them without importing more packages.

var (
	// ErrAbort is returned when retrying an action will not yield a different outcome.
	// An alias of protocol.ErrAbort for easier access without importing subpackages.
	ErrAbort = protocol.ErrAbort

	// ErrValidationFailed is returned when a validation check fails.
	// An alias of protocol.ErrValidationFailed for easier access without importing subpackages.
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
func GetRemoteFS(runner cmd.Runner) (remotefs.FS, error) {
	fs, err := remotefs.DefaultProvider().Get(runner)
	if err != nil {
		return nil, fmt.Errorf("get FS: %w", err)
	}
	return fs, nil
}

// GetServiceManager returns a ServiceManager for the current system from the default init system providers.
func GetServiceManager(runner cmd.ContextRunner) (initsystem.ServiceManager, error) {
	mgr, err := initsystem.DefaultProvider().Get(runner)
	if err != nil {
		return nil, fmt.Errorf("get service manager: %w", err)
	}
	return mgr, nil
}

// GetPackageManager returns a package manager from the default provider.
func GetPackageManager(runner cmd.ContextRunner) (packagemanager.PackageManager, error) {
	pm, err := packagemanager.DefaultProvider().Get(runner)
	if err != nil {
		return nil, fmt.Errorf("get package manager: %w", err)
	}
	return pm, nil
}

// GetSudoRunner returns a new runner that uses sudo to execute commands.
func GetSudoRunner(runner cmd.Runner) (cmd.Runner, error) {
	sudoR, err := sudo.DefaultProvider().Get(runner)
	if err != nil {
		return nil, fmt.Errorf("get sudo runner: %w", err)
	}
	return sudoR, nil
}

// GetOSRelease returns the remote host's operating system information from the default provider.
func GetOSRelease(runner cmd.SimpleRunner) (*os.Release, error) {
	os, err := os.DefaultProvider().Get(runner)
	if err != nil {
		return nil, fmt.Errorf("get os release: %w", err)
	}
	return os, nil
}

// NewRunner returns a new cmd.Runner for the connection.
// Currently the error is always nil.
func NewRunner(conn protocol.Connection) (cmd.Runner, error) {
	return cmd.NewExecutor(conn), nil
}
