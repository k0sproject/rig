// Package sudo provides support for various methods of running commands with elevated privileges.
package sudo

import (
	"errors"

	"github.com/k0sproject/rig/exec"
)

var (
	// ErrNoSudo is returned when no supported sudo method is found.
	ErrNoSudo = errors.New("no supported sudo method found")
	// DefaultProvider is the default sudo repository.
	DefaultProvider = NewProvider()
)

func init() {
	RegisterWindowsNoop(DefaultProvider)
	RegisterUID0Noop(DefaultProvider)
	RegisterSudo(DefaultProvider)
	RegisterDoas(DefaultProvider)
}

// Factory is a function that returns a DecorateFunc if the sudo method is supported.
type Factory func(exec.SimpleRunner) exec.DecorateFunc

// SudoProvider is a factory for sudo methods.
type SudoProvider interface {
	Get(runner exec.Runner) (exec.Runner, error)
}

// Provider is a collection of sudo builders.
type Provider struct {
	builders []Factory
}

// NewProvider returns a new sudo repository.
func NewProvider(factories ...Factory) *Provider {
	return &Provider{builders: factories}
}

// Register a new sudo builder.
func (r *Provider) Register(b Factory) {
	r.builders = append(r.builders, b)
}

// Get the first builder that returns a non-nil DecorateFunc.
func (r *Provider) Get(runner exec.Runner) (exec.Runner, error) {
	for _, b := range r.builders {
		if f := b(runner); f != nil {
			return exec.NewHostRunner(runner, f), nil
		}
	}
	return nil, ErrNoSudo
}
