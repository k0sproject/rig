package os

import (
	"errors"
	"sync"

	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/plumbing"
)

var (
	// DefaultProvider is the default OS release provider.
	DefaultProvider = sync.OnceValue(func() *Provider {
		provider := NewProvider()
		provider.Register(ResolveLinux)
		provider.Register(ResolveWindows)
		provider.Register(ResolveDarwin)
		return provider
	})

	// ErrNotRecognized is returned when the host OS is not recognized.
	ErrNotRecognized = errors.New("host OS not recognized")
)

// Factory is a function that returns an OS release based on the provided runner.
type Factory = plumbing.Factory[exec.SimpleRunner, *Release]

// Provider is a type that can determine the host OS given a runner.
type Provider = plumbing.Provider[exec.SimpleRunner, *Release]

// OSReleaseProvider is a factory for OS release information.
type OSReleaseProvider interface {
	Get(runner exec.SimpleRunner) (*Release, error)
}

// NewProvider creates a new OS release provider.
func NewProvider() *Provider {
	return plumbing.NewProvider[exec.SimpleRunner, *Release](ErrNotRecognized)
}
