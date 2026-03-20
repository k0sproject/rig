package sudo

import (
	"fmt"

	"github.com/k0sproject/rig/v2/cmd"
	"github.com/k0sproject/rig/v2/plumbing"
)

// Because the "sudo providers" actually just return a decorator

// Provider provides a unified interface to interact with different
// sudo methods. It ensures that a suitable sudo runner is lazily initialized
// and made available for privileged command execution.
type Provider struct {
	lazy *plumbing.LazyService[cmd.Runner, cmd.Runner]
}

// GetSudoRunner returns an cmd.Runner with a sudo decorator or an error if the
// decorator could not be initialized.
func (p *Provider) GetSudoRunner() (cmd.Runner, error) {
	runner, err := p.lazy.Get()
	if err != nil {
		return nil, fmt.Errorf("get sudo runner: %w", err)
	}
	return runner, nil
}

// SudoRunner returns an cmd.Runner with a sudo decorator. If the runner initialization
// failed, an error runner is returned which will return the initialization error on
// every operation that is attempted on it.
func (p *Provider) SudoRunner() cmd.Runner {
	runner, err := p.lazy.Get()
	if err != nil {
		return cmd.NewErrorExecutor(err)
	}
	return runner
}

// NewSudoProvider creates a new instance of Provider with the provided SudoFactory
// and runner.
func NewSudoProvider(factory SudoFactory, runner cmd.Runner) *Provider {
	return &Provider{plumbing.NewLazyService[cmd.Runner, cmd.Runner](factory, runner)}
}
