// Package sudo provides support for various methods of running commands with elevated privileges.
package sudo

import (
	"errors"
	"sync"

	"github.com/k0sproject/rig/v2/cmd"
	"github.com/k0sproject/rig/v2/plumbing"
)

var (
	// ErrNoSudo is returned when no supported sudo method is found.
	ErrNoSudo = errors.New("no supported sudo method found")
	// DefaultRegistry is the default sudo repository.
	DefaultRegistry = sync.OnceValue(func() *Registry {
		provider := NewRegistry()
		RegisterWindowsNoop(provider)
		RegisterUID0Noop(provider)
		RegisterSudo(provider)
		RegisterDoas(provider)
		return provider
	})
)

// RunnerProvider is a function that returns a cmd.Runner with elevated privileges
// given a runner.
type RunnerProvider func(cmd.Runner) (cmd.Runner, error)

// Factory is a factory for sudo runners.
type Factory = plumbing.Factory[cmd.Runner, cmd.Runner]

// Registry is a repository for sudo runner factories.
type Registry = plumbing.Provider[cmd.Runner, cmd.Runner]

// NewRegistry returns a new sudo repository.
func NewRegistry() *Registry {
	return plumbing.NewProvider[cmd.Runner, cmd.Runner](ErrNoSudo)
}
