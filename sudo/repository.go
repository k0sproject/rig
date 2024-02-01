// Package sudo provides support for various methods of running commands with elevated privileges.
package sudo

import (
	"errors"

	"github.com/k0sproject/rig/exec"
)

var (
	// ErrNoSudo is returned when no supported sudo method is found.
	ErrNoSudo = errors.New("no supported sudo method found")
	// DefaultRepository is the default sudo repository.
	DefaultRepository = NewRepository()
)

func init() {
	RegisterWindowsNoop(DefaultRepository)
	RegisterUID0Noop(DefaultRepository)
	RegisterSudo(DefaultRepository)
	RegisterDoas(DefaultRepository)
}

// Factory is a function that returns a DecorateFunc if the sudo method is supported.
type Factory func(exec.SimpleRunner) exec.DecorateFunc

// Repository is a collection of sudo builders.
type Repository struct {
	builders []Factory
}

// NewRepository returns a new sudo repository.
func NewRepository(factories ...Factory) *Repository {
	return &Repository{builders: factories}
}

// Register a new sudo builder.
func (r *Repository) Register(b Factory) {
	r.builders = append(r.builders, b)
}

// Get the first builder that returns a non-nil DecorateFunc.
func (r *Repository) Get(runner exec.SimpleRunner) (exec.DecorateFunc, error) {
	for _, b := range r.builders {
		if f := b(runner); f != nil {
			return f, nil
		}
	}
	return nil, ErrNoSudo
}
