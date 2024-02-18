package os

import (
	"errors"

	"github.com/k0sproject/rig/exec"
)

var (
	// DefaultProvider is the default OS release provider.
	DefaultProvider = NewProvider()
	// ErrNotRecognized is returned when the host OS is not recognized.
	ErrNotRecognized = errors.New("host OS not recognized")
)

func init() {
	DefaultProvider.Register(ResolveLinux)
	DefaultProvider.Register(ResolveWindows)
	DefaultProvider.Register(ResolveDarwin)
}

// Factory is a function that returns release information for a host.
type Factory func(runner exec.SimpleRunner) *Release

// OSProvider is a factory for OS release information.
type OSProvider interface {
	Get(runner exec.SimpleRunner) (*Release, error)
}

// Provider is a collection of factories that can determine the host OS.
type Provider struct {
	factories []Factory
}

// NewProvider creates a new OS release provider.
func NewProvider(factories ...Factory) *Provider {
	return &Provider{factories: factories}
}

// Get returns the OS release information for the host using the registered factories.
func (p *Provider) Get(runner exec.SimpleRunner) (*Release, error) {
	for _, f := range p.factories {
		if os := f(runner); os != nil {
			return os, nil
		}
	}
	return nil, ErrNotRecognized
}

// Register a factory to the provider.
func (p *Provider) Register(f Factory) {
	p.factories = append(p.factories, f)
}
