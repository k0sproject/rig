package remotefs

import (
	"bufio"
	"context"
	"io"

	"github.com/k0sproject/rig/v2/cmd"
	"github.com/k0sproject/rig/v2/protocol"
)

// WithContext returns a copy of fsys whose commands run under ctx, so that a
// caller can bound or cancel operations whose signatures have no room for a
// context, such as Open, Stat and ReadFile from fs.FS or rig's own Remove and
// Sha256. Functions taking fsys, such as Upload, are bounded the same way.
//
// Methods that take a context of their own keep using the one they are given.
// Once ctx is done, the remote commands the returned filesystem and the files
// opened from it run fail, while fsys itself is unaffected. Operations that
// need no remote command, such as seeking or reading entries a directory has
// already listed, are not affected.
//
// fsys is returned unchanged if it is not a *PosixFS or *WinFS, including a
// type that embeds one, whose own methods a copy would lose.
func WithContext(ctx context.Context, fsys FS) FS {
	switch f := fsys.(type) {
	case *PosixFS:
		return f.withContext(ctx)
	case *WinFS:
		return f.withContext(ctx)
	default:
		return fsys
	}
}

func (s *PosixFS) withContext(ctx context.Context) FS {
	return &PosixFS{
		Runner:           bindContext(ctx, s.Runner),
		LoggerInjectable: s.LoggerInjectable,
		statCmd:          s.statCmd,
		chtimesFn:        s.chtimesFn,
	}
}

func (s *WinFS) withContext(ctx context.Context) FS {
	return &WinFS{
		Runner:           bindContext(ctx, s.Runner),
		LoggerInjectable: s.LoggerInjectable,
	}
}

// contextRunner runs the commands it is given without a context under ctx.
type contextRunner struct {
	cmd.Runner
	ctx context.Context //nolint:containedctx // the runner methods it stands in for take no context
}

// bindContext returns runner with its context-less methods bound to ctx. A
// runner that is already bound is rebound rather than wrapped again.
func bindContext(ctx context.Context, runner cmd.Runner) cmd.Runner {
	if bound, ok := runner.(*contextRunner); ok {
		runner = bound.Runner
	}
	return &contextRunner{Runner: runner, ctx: ctx}
}

// runnerContext returns the context runner is bound to, or a background
// context if it is not bound.
func runnerContext(runner cmd.Runner) context.Context {
	if bound, ok := runner.(*contextRunner); ok {
		return bound.ctx
	}
	return context.Background()
}

// fsContext returns the context fsys is bound to by WithContext, or a
// background context.
func fsContext(fsys FS) context.Context {
	switch f := fsys.(type) {
	case *PosixFS:
		return runnerContext(f.Runner)
	case *WinFS:
		return runnerContext(f.Runner)
	default:
		return context.Background()
	}
}

func (r *contextRunner) Exec(command string, opts ...cmd.ExecOption) error {
	return r.ExecContext(r.ctx, command, opts...) //nolint:wrapcheck // transparent pass-through
}

func (r *contextRunner) ExecOutput(command string, opts ...cmd.ExecOption) (string, error) {
	return r.ExecOutputContext(r.ctx, command, opts...) //nolint:wrapcheck // transparent pass-through
}

func (r *contextRunner) ExecReader(command string, opts ...cmd.ExecOption) io.Reader {
	return r.ExecReaderContext(r.ctx, command, opts...)
}

func (r *contextRunner) ExecScanner(command string, opts ...cmd.ExecOption) *bufio.Scanner {
	return bufio.NewScanner(r.ExecReaderContext(r.ctx, command, opts...))
}

func (r *contextRunner) StartBackground(command string, opts ...cmd.ExecOption) (protocol.Waiter, error) {
	return r.Start(r.ctx, command, opts...) //nolint:wrapcheck // transparent pass-through
}
