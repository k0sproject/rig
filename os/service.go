package os

import (
	"fmt"

	"github.com/k0sproject/rig/cmd"
	"github.com/k0sproject/rig/plumbing"
)

// Service provides an interface to detect the operating system version and
// release information using the specified Provider. The result is lazily
// initialized and memoized.
type Service struct {
	lazy *plumbing.LazyService[cmd.SimpleRunner, *Release]
}

// GetOSRelease returns remote host operating system version and release information.
func (p *Service) GetOSRelease() (*Release, error) {
	os, err := p.lazy.Get()
	if err != nil {
		return nil, fmt.Errorf("get os release: %w", err)
	}
	return os, nil
}

// NewOSReleaseService creates a new instance of Service with the provided
// provider and runner.
func NewOSReleaseService(provider OSReleaseProvider, runner cmd.SimpleRunner) *Service {
	return &Service{plumbing.NewLazyService[cmd.SimpleRunner, *Release](provider, runner)}
}
