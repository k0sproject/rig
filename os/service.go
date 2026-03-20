package os

import (
	"fmt"

	"github.com/k0sproject/rig/v2/cmd"
	"github.com/k0sproject/rig/v2/plumbing"
)

// Provider provides an interface to detect the operating system version and
// release information using the specified factory. The result is lazily
// initialized and memoized.
type Provider struct {
	lazy *plumbing.LazyService[cmd.SimpleRunner, *Release]
}

// GetOSRelease returns remote host operating system version and release information.
func (p *Provider) GetOSRelease() (*Release, error) {
	os, err := p.lazy.Get()
	if err != nil {
		return nil, fmt.Errorf("get os release: %w", err)
	}
	return os, nil
}

// NewOSReleaseProvider creates a new instance of Provider with the provided
// factory and runner.
func NewOSReleaseProvider(factory OSReleaseFactory, runner cmd.SimpleRunner) *Provider {
	return &Provider{plumbing.NewLazyService[cmd.SimpleRunner, *Release](factory, runner)}
}
