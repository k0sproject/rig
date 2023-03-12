package exec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
)

type Client interface {
	IsWindows() bool
	Exec(command string, stdin io.Reader, stdout io.Writer, stderr io.Writer) (Process, error)
	String() string
}

type Runner struct {
	client         Client
	defaultOptions *Options
}

func (r *Runner) Sudo() *Runner {
	return &Runner{
		client:         r.client,
		defaultOptions: r.defaultOptions.With(Sudo()),
	}
}

func (r *Runner) IsWindows() bool {
	return r.client.IsWindows()
}

func NewRunner(client Client, opts ...Option) *Runner {
	options := DefaultOptions().With(opts...)
	if client.IsWindows() {
		options = options.With(DisallowStderr())
	}
	options = options.With(
		Logger(options.Logger.WithGroup("runner").With("client", client.String())),
	)
	return &Runner{client: client, defaultOptions: options}
}

type RunnerProcess struct {
	proc Process
	opts *Options
}

func (p *RunnerProcess) Wait() error {
	err := p.proc.Wait()
	p.opts.Finalize()

	if err != nil {
		return fmt.Errorf("exec: %w", err)
	}

	if p.opts.DisallowStderr && p.opts.StderrDataReceived() {
		return fmt.Errorf("exec: command wrote output to stderr")
	}

	return nil
}

func (r *Runner) StartCommand(ctx context.Context, command string, opts ...Option) (Process, error) {
	options := r.defaultOptions.With(opts...)

	if options.Sudo && options.SudoFn == nil {
		if options.SudoRepo == nil {
			return nil, ErrSudoNotConfigured
		}
		sp, err := options.SudoRepo.Find(r)
		if err != nil {
			return nil, errors.Join(ErrSudoNotConfigured, err)
		}
		options.SudoFn = sp.Sudo
	}

	cmd, err := options.Command(command)
	if err != nil {
		return nil, err
	}

	if options.ConfirmFunc != nil {
		if !options.ConfirmFunc(cmd) {
			return nil, fmt.Errorf("exec: command not confirmed")
		}
	}

	options.logCommand(command)

	process, err := r.client.Exec(cmd, options.InputReader(), options.OutputWriter(), options.ErrorWriter())
	if err != nil {
		options.Finalize()
		return nil, fmt.Errorf("start process: %w", err)
	}

	return &RunnerProcess{proc: process, opts: options}, nil
}

func (r *Runner) ExecCtx(ctx context.Context, command string, opts ...Option) error {
	process, err := r.StartCommand(ctx, command, opts...)
	if err != nil {
		return err
	}
	if err := process.Wait(); err != nil {
		return fmt.Errorf("exec: %w", err)
	}

	return nil
}

func (r *Runner) Exec(command string, opts ...Option) error {
	return r.ExecCtx(context.TODO(), command, opts...)
}

func (r *Runner) Execf(format string, argsAndOpts ...any) error {
	opts, args := Extract(argsAndOpts...)
	return r.Exec(fmt.Sprintf(format, args...), opts...)
}

func (r *Runner) ExecOutput(command string, opts ...Option) (string, error) {
	buf := &bytes.Buffer{}
	opts = append(opts, Stdout(buf))
	err := r.Exec(command, opts...)
	return buf.String(), err
}

func (r *Runner) ExecOutputf(format string, argsAndOpts ...any) (string, error) {
	opts, args := Extract(argsAndOpts...)
	return r.ExecOutput(fmt.Sprintf(format, args...), opts...)
}
