package packagemanager

import (
	"context"
	"fmt"
	"strings"

	"github.com/k0sproject/rig/cmd"
	"github.com/k0sproject/rig/plumbing"
)

// Service provides a unified interface to interact with different
// package managers. It ensures that a suitable package manager is lazily initialized
// and made available for package operations. It supports operations like installation,
// removal, and updating of packages via the PackageManager interface.
type Service struct {
	lazy *plumbing.LazyService[cmd.ContextRunner, PackageManager]
}

// GetPackageManager returns a PackageManager or an error if the package manager
// could not be initialized.
func (p *Service) GetPackageManager() (PackageManager, error) {
	pm, err := p.lazy.Get()
	if err != nil {
		return nil, fmt.Errorf("get package manager: %w", err)
	}
	return pm, nil
}

// PackageManager provides easy access to the underlying package manager instance.
// It initializes the package manager if it has not been initialized yet. If the
// initialization fails, a NullPackageManager instance is returned which will
// return the initialization error on every operation that is attempted on it.
func (p *Service) PackageManager() PackageManager {
	pm, err := p.lazy.Get()
	if err != nil {
		return &NullPackageManager{Err: err}
	}
	return pm
}

// NewPackageManagerService creates a new instance of PackageManagerService
// with the provided PackageManagerProvider.
func NewPackageManagerService(provider PackageManagerProvider, runner cmd.ContextRunner) *Service {
	return &Service{plumbing.NewLazyService[cmd.ContextRunner, PackageManager](provider, runner)}
}

// NullPackageManager is a package manager that always returns an error on
// every operation.
type NullPackageManager struct {
	Err error
}

func (n *NullPackageManager) err(ctx context.Context) error {
	if ctx.Err() != nil {
		return fmt.Errorf("context error: %w", ctx.Err())
	}
	return n.Err
}

// Install returns an error on every call.
func (n *NullPackageManager) Install(ctx context.Context, packageNames ...string) error {
	return fmt.Errorf("install packages (%s): %w", strings.Join(packageNames, ","), n.err(ctx))
}

// Remove returns an error on every call.
func (n *NullPackageManager) Remove(ctx context.Context, packageNames ...string) error {
	return fmt.Errorf("remove packages (%s): %w", strings.Join(packageNames, ","), n.err(ctx))
}

// Update returns an error on every call.
func (n *NullPackageManager) Update(ctx context.Context) error {
	return fmt.Errorf("update packages list: %w", n.err(ctx))
}
