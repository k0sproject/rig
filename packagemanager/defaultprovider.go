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

// ManagerProvider is a function that returns a PackageManager given a runner.
type ManagerProvider func(cmd.ContextRunner) (PackageManager, error)

var (
	// DefaultRegistry is the default repository of package managers.
	DefaultRegistry = sync.OnceValue(func() *Registry {
		provider := NewRegistry()
		RegisterDefaults(provider)
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

// RegisterDefaults registers the package managers rig ships with, which is what
// DefaultRegistry holds. Use it to build a registry of your own without having to
// list them, and without missing package managers added in later versions.
//
// The factories are appended, so one of your own that has to take precedence over
// them must be registered before this call, or with Registry.RegisterFirst.
func RegisterDefaults(provider *Registry) {
	RegisterApk(provider)
	RegisterApt(provider)
	RegisterYum(provider)
	RegisterDnf(provider)
	RegisterPacman(provider)
	RegisterZypper(provider)
	RegisterWindowsMultiManager(provider)
	RegisterHomebrew(provider)
	RegisterMacports(provider)
}
