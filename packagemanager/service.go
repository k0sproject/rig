package packagemanager

import (
	"context"
	"fmt"
	"strings"

	"github.com/k0sproject/rig/v2/cmd"
	"github.com/k0sproject/rig/v2/plumbing"
)

// Provider provides a unified interface to interact with different
// package managers. It ensures that a suitable package manager is lazily initialized
// and made available for package operations. It supports operations like installation,
// removal, and updating of packages via the PackageManager interface.
type Provider struct {
	lazy *plumbing.LazyService[cmd.ContextRunner, PackageManager]
}

// PackageManager returns a PackageManager or an error if the package manager
// could not be initialized.
func (p *Provider) PackageManager() (PackageManager, error) {
	pm, err := p.lazy.Get()
	if err != nil {
		return nil, fmt.Errorf("get package manager: %w", err)
	}
	return pm, nil
}

// NewPackageManagerProvider creates a new instance of Provider
// with the provided ManagerProvider function.
func NewPackageManagerProvider(get ManagerProvider, runner cmd.ContextRunner) *Provider {
	return &Provider{plumbing.NewLazyService(get, runner)}
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
