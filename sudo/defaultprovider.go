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

// SudoProvider returns a new cmd.Runner with elevated privileges based on the
// given runner.
type SudoProvider interface { //nolint:revive // stutter
	Get(runner cmd.Runner) (cmd.Runner, error)
}

// Factory is a factory for sudo runners.
type Factory = plumbing.Factory[cmd.Runner, cmd.Runner]

// Provider is a repository for sudo runner factories.
type Provider = plumbing.Provider[cmd.Runner, cmd.Runner]

// NewProvider returns a new sudo repository.
func NewProvider() *Provider {
	return plumbing.NewProvider[cmd.Runner, cmd.Runner](ErrNoSudo)
}
