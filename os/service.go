package os

import (
	"fmt"

	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/plumbing"
)

// Service provides an interface to detect the operating system version and
// release information using the specified Provider. The result is lazily
// initialized and memoized.
type Service struct {
	lazy *plumbing.LazyService[exec.SimpleRunner, *Release]
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
func NewOSReleaseService(provider OSReleaseProvider, runner exec.SimpleRunner) *Service {
	return &Service{plumbing.NewLazyService[exec.SimpleRunner, *Release](provider, runner)}
}
