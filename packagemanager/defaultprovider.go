// Package packagemanager provides a generic interface for package managers.
package packagemanager

import (
	"context"
	"errors"
	"sync"

	"github.com/k0sproject/rig/v2/cmd"
	"github.com/k0sproject/rig/v2/plumbing"
)

// PackageManager is a generic interface for package managers.
type PackageManager interface {
	Install(ctx context.Context, packageNames ...string) error
	Remove(ctx context.Context, packageNames ...string) error
	Update(ctx context.Context) error
}

// PackageManagerProvider returns a package manager implementation from a provider when given a runner.
type PackageManagerProvider interface { //nolint:revive // TODO stutter
	Get(runner cmd.ContextRunner) (PackageManager, error)
}

var (
	// DefaultRegistry is the default repository of package managers.
	DefaultRegistry = sync.OnceValue(func() *Registry {
		provider := NewRegistry()
		RegisterApk(provider)
		RegisterApt(provider)
		RegisterYum(provider)
		RegisterDnf(provider)
		RegisterPacman(provider)
		RegisterZypper(provider)
		RegisterWindowsMultiManager(provider)
		RegisterHomebrew(provider)
		RegisterMacports(provider)
		return provider
	})
	// ErrNoPackageManager is returned when no supported package manager is found.
	ErrNoPackageManager = errors.New("no supported package manager found")
)

// Factory is an alias for plumbing.Factory specialized for PackageManager.
type Factory = plumbing.Factory[cmd.ContextRunner, PackageManager]

// Registry is an alias for plumbing.Provider specialized for PackageManager.
type Registry = plumbing.Provider[cmd.ContextRunner, PackageManager]

// NewRegistry creates a new instance of the specialized Registry.
func NewRegistry() *Registry {
	return plumbing.NewProvider[cmd.ContextRunner, PackageManager](ErrNoPackageManager)
}
