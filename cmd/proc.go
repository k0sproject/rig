package cmd

import (
	"context"
	"io"

	"github.com/k0sproject/rig/v2/protocol"
)

// Proc is a command bound to a runner. Set Stdin, Stdout, and Stderr as needed,
// then call Start or Run. Modeled after os/exec.Cmd.
type Proc struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	runner  ContextRunner
	command string
}

func (p *Proc) ioOpts(extra []ExecOption) []ExecOption {
	opts := make([]ExecOption, 0, 3+len(extra))
	if p.Stdin != nil {
		opts = append(opts, Stdin(p.Stdin))
	}
	if p.Stdout != nil {
		opts = append(opts, Stdout(p.Stdout))
	}
	if p.Stderr != nil {
		opts = append(opts, Stderr(p.Stderr))
	}
	return append(opts, extra...)
}

// Start starts the command and returns a Waiter.
func (p *Proc) Start(ctx context.Context, opts ...ExecOption) (protocol.Waiter, error) {
	return p.runner.Start(ctx, p.command, p.ioOpts(opts)...) //nolint:wrapcheck // transparent delegation
}

// Run starts the command and waits for it to complete.
func (p *Proc) Run(ctx context.Context, opts ...ExecOption) error {
	return p.runner.ExecContext(ctx, p.command, p.ioOpts(opts)...) //nolint:wrapcheck // transparent delegation
}
