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
	// ErrNonRetryable is returned when retrying an action will not yield a different outcome.
	// An alias of protocol.ErrNonRetryable for easier access without importing subpackages.
	ErrNonRetryable = protocol.ErrNonRetryable

	// ErrValidationFailed is returned when a validation check fails.
	// An alias of protocol.ErrValidationFailed for easier access without importing subpackages.
	ErrValidationFailed = protocol.ErrValidationFailed
)

// PackageManagerProvider is a type alias for packagemanager.Provider.
type PackageManagerProvider = packagemanager.Provider

// InitSystemProvider is a type alias for initsystem.Provider.
type InitSystemProvider = initsystem.Provider

// RemoteFSProvider is a type alias for remotefs.Provider.
type RemoteFSProvider = remotefs.Provider

// OSReleaseProvider is a type alias for os.Provider.
type OSReleaseProvider = os.Provider

// SudoProvider is a type alias for sudo.Provider.
type SudoProvider = sudo.Provider

// GetRemoteFS returns a new remote FS instance from the default remote.FS provider.
func GetRemoteFS(runner cmd.Runner) (remotefs.FS, error) {
	fs, err := remotefs.DefaultRegistry().Get(runner)
	if err != nil {
		return nil, fmt.Errorf("get FS: %w", err)
	}
	return fs, nil
}

// GetServiceManager returns a ServiceManager for the current system from the default init system providers.
func GetServiceManager(runner cmd.ContextRunner) (initsystem.ServiceManager, error) {
	mgr, err := initsystem.DefaultRegistry().Get(runner)
	if err != nil {
		return nil, fmt.Errorf("get service manager: %w", err)
	}
	return mgr, nil
}

// GetPackageManager returns a package manager from the default provider.
func GetPackageManager(runner cmd.ContextRunner) (packagemanager.PackageManager, error) {
	pm, err := packagemanager.DefaultRegistry().Get(runner)
	if err != nil {
		return nil, fmt.Errorf("get package manager: %w", err)
	}
	return pm, nil
}

// GetSudoRunner returns a new runner that uses sudo to execute commands.
func GetSudoRunner(runner cmd.Runner) (cmd.Runner, error) {
	sudoR, err := sudo.DefaultRegistry().Get(runner)
	if err != nil {
		return nil, fmt.Errorf("get sudo runner: %w", err)
	}
	return sudoR, nil
}

// GetOSRelease returns the remote host's operating system information from the default provider.
func GetOSRelease(runner cmd.SimpleRunner) (*os.Release, error) {
	os, err := os.DefaultRegistry().Get(runner)
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
