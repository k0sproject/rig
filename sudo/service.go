package sudo

import (
	"fmt"

	"github.com/k0sproject/rig/cmd"
	"github.com/k0sproject/rig/plumbing"
)

// Because the "sudo providers" actually just return a decorator

// Service provides a unified interface to interact with different
// package managers. It ensures that a suitable package manager is lazily initialized
// and made available for package operations. It supports operations like installation,
// removal, and updating of packages via the PackageManager interface.
type Service struct {
	lazy *plumbing.LazyService[cmd.Runner, cmd.Runner]
}

// GetSudoRunner returns an cmd.Runner with a sudo decorator or an error if the
// decorator could not be initialized.
func (p *Service) GetSudoRunner() (cmd.Runner, error) {
	runner, err := p.lazy.Get()
	if err != nil {
		return nil, fmt.Errorf("get sudo runner: %w", err)
	}
	return runner, nil
}

// SudoRunner returns an cmd.Runner with a sudo decorator. If the runner initialization
// failed, an error runner is returned which will return the initialization error on
// every operation that is attempted on it.
func (p *Service) SudoRunner() cmd.Runner {
	runner, err := p.lazy.Get()
	if err != nil {
		return cmd.NewErrorExecutor(err)
	}
	return runner
}

// NewSudoService creates a new instance of SudoService with the provided SudoProvider
// and runner.
func NewSudoService(provider SudoProvider, runner cmd.Runner) *Service {
	return &Service{plumbing.NewLazyService[cmd.Runner, cmd.Runner](provider, runner)}
}
