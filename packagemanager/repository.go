// Package packagemanager provides a generic interface for package managers.
package packagemanager

import (
	"context"
	"errors"
	"strings"

	"github.com/alessio/shellescape"
	"github.com/k0sproject/rig/exec"
)

// PackageManager is a generic interface for package managers.
type PackageManager interface {
	Install(ctx context.Context, packageNames ...string) error
	Remove(ctx context.Context, packageNames ...string) error
	Update(ctx context.Context) error
}

var (
	// DefaultRepository is the default repository of package managers.
	DefaultRepository = NewRepository()
	// ErrNoPackageManager is returned when no supported package manager is found.
	ErrNoPackageManager = errors.New("no supported package manager found")
)

func init() {
	RegisterApk(DefaultRepository)
	RegisterApt(DefaultRepository)
	RegisterYum(DefaultRepository)
	RegisterDnf(DefaultRepository)
	RegisterPacman(DefaultRepository)
	RegisterZypper(DefaultRepository)
	RegisterWindowsMultiManager(DefaultRepository)
	RegisterHomebrew(DefaultRepository)
	RegisterMacports(DefaultRepository)
}

// Factory is a function that creates a package manager.
type Factory func(c exec.ContextRunner) PackageManager

// Repository is a repository of package managers.
type Repository struct {
	managers []Factory
}

// NewRepository creates a new repository of package managers.
func NewRepository(factories ...Factory) *Repository {
	return &Repository{managers: factories}
}

// Register registers a package manager to the repository.
func (r *Repository) Register(factory Factory) {
	r.managers = append(r.managers, factory)
}

// Get returns a package manager from the repository.
func (r *Repository) Get(c exec.ContextRunner) (PackageManager, error) {
	for _, builder := range r.managers {
		if mgr := builder(c); mgr != nil {
			return mgr, nil
		}
	}
	return nil, ErrNoPackageManager
}

func (r *Repository) getAll(c exec.ContextRunner) []PackageManager {
	var managers []PackageManager
	for _, builder := range r.managers {
		if mgr := builder(c); mgr != nil {
			managers = append(managers, mgr)
		}
	}
	return managers
}

func buildCommand(basecmd, keyword string, packages ...string) string {
	cmd := &strings.Builder{}
	cmd.WriteString(basecmd)
	cmd.WriteRune(' ')
	cmd.WriteString(keyword)
	for _, pkg := range packages {
		cmd.WriteRune(' ')
		cmd.WriteString(shellescape.Quote(pkg))
	}
	return cmd.String()
}