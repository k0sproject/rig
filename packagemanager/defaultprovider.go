// Package packagemanager provides a generic interface for package managers.
package packagemanager

import (
	"context"
	"errors"

	"github.com/k0sproject/rig/exec"
)

// PackageManager is a generic interface for package managers.
type PackageManager interface {
	Install(ctx context.Context, packageNames ...string) error
	Remove(ctx context.Context, packageNames ...string) error
	Update(ctx context.Context) error
}

var (
	// DefaultProvider is the default repository of package managers.
	DefaultProvider = NewProvider()
	// ErrNoPackageManager is returned when no supported package manager is found.
	ErrNoPackageManager = errors.New("no supported package manager found")
)

func init() {
	RegisterApk(DefaultProvider)
	RegisterApt(DefaultProvider)
	RegisterYum(DefaultProvider)
	RegisterDnf(DefaultProvider)
	RegisterPacman(DefaultProvider)
	RegisterZypper(DefaultProvider)
	RegisterWindowsMultiManager(DefaultProvider)
	RegisterHomebrew(DefaultProvider)
	RegisterMacports(DefaultProvider)
}

// Factory is a function that creates a package manager.
type Factory func(c exec.ContextRunner) PackageManager

// Provider is a repository of package managers.
type Provider struct {
	managers []Factory
}

// NewProvider creates a new repository of package managers.
func NewProvider(factories ...Factory) *Provider {
	return &Provider{managers: factories}
}

// Register registers a package manager to the repository.
func (r *Provider) Register(factory Factory) {
	r.managers = append(r.managers, factory)
}

// Get returns a package manager from the repository.
func (r *Provider) Get(c exec.ContextRunner) (PackageManager, error) {
	for _, builder := range r.managers {
		if mgr := builder(c); mgr != nil {
			return mgr, nil
		}
	}
	return nil, ErrNoPackageManager
}

func (r *Provider) getAll(c exec.ContextRunner) []PackageManager {
	var managers []PackageManager
	for _, builder := range r.managers {
		if mgr := builder(c); mgr != nil {
			managers = append(managers, mgr)
		}
	}
	return managers
}
