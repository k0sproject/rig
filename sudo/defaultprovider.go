// Package sudo provides support for various methods of running commands with elevated privileges.
package sudo

import (
	"errors"
	"sync"

	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/plumbing"
)

var (
	// ErrNoSudo is returned when no supported sudo method is found.
	ErrNoSudo = errors.New("no supported sudo method found")
	// DefaultProvider is the default sudo repository.
	DefaultProvider = sync.OnceValue(func() *Provider {
		provider := NewProvider()
		RegisterWindowsNoop(provider)
		RegisterUID0Noop(provider)
		RegisterSudo(provider)
		RegisterDoas(provider)
		return provider
	})
)

// SudoProvider returns a new exec.Runner with elevated privileges based on the
// given runner.
type SudoProvider interface { //nolint:revive // stutter
	Get(runner exec.Runner) (exec.Runner, error)
}

// Factory is a factory for sudo runners.
type Factory = plumbing.Factory[exec.Runner, exec.Runner]

// Provider is a repository for sudo runner factories.
type Provider = plumbing.Provider[exec.Runner, exec.Runner]

// NewProvider returns a new sudo repository.
func NewProvider() *Provider {
	return plumbing.NewProvider[exec.Runner, exec.Runner](ErrNoSudo)
}
