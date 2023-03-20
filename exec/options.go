package exec

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/k0sproject/rig/iostream"
	"github.com/k0sproject/rig/log"
)

var ErrSudoNotConfigured = errors.New("sudo not configured")

type RedactFn func(string) string
type SudoFn func(string) (string, error)
type ConfirmFn func(string) bool

var RedactMask = "[REDACTED]"

// Options is a collection of exec options
type Options struct {
	Logger            *log.Logger
	DisallowStderr    bool
	DisableRedact     bool
	disableLogStreams bool
	RedactFuncs       []RedactFn
	Stdin             io.Reader
	Stdout            io.Writer
	Stderr            io.Writer
	Sudo              bool
	SudoFn            SudoFn
	sudoRepo          SudoProviderRepository
	ConfirmFunc       ConfirmFn
	PasswordFunc      PasswordCallback
	wroteToStderr     bool
	defers            []func()
}

func DefaultOptions() *Options {
	return &Options{
		Logger: log.Default(),
	}
}

func (o *Options) clone() *Options {
	return &Options{
		Logger:            o.Logger,
		DisallowStderr:    o.DisallowStderr,
		RedactFuncs:       append([]RedactFn{}, o.RedactFuncs...),
		Stdin:             o.Stdin,
		Stdout:            o.Stdout,
		Stderr:            o.Stderr,
		SudoFn:            o.SudoFn,
		Sudo:              o.Sudo,
		ConfirmFunc:       o.ConfirmFunc,
		PasswordFunc:      o.PasswordFunc,
		disableLogStreams: o.disableLogStreams,
		sudoRepo:          o.sudoRepo,
	}
}

func (o *Options) StderrDataReceived() bool {
	return o.wroteToStderr
}

func (o *Options) With(opts ...Option) *Options {
	newOpts := o.clone()
	for _, opt := range opts {
		opt(newOpts)
	}
	return o
}

func (o *Options) log(ctx context.Context, level log.Level, msg string, attrs ...log.Attr) {
	if o.Logger == nil {
		return
	}
	o.Logger.LogAttrs(ctx, level, msg, attrs...)
}

// Lazy-evaluated redacting as logging is mostly disabled
type redact struct {
	str string
	o   *Options
}

func (r *redact) LogValue() log.Value {
	if r.o.DisableRedact {
		return log.AnyValue(r.str)
	}

	val := r.str

	for _, fn := range r.o.RedactFuncs {
		val = fn(val)
	}

	return log.AnyValue(val)
}

func (o *Options) redactValuer(val string) string {
	if o.DisableRedact {
		return val
	}
	for _, fn := range o.RedactFuncs {
		val = fn(val)
	}
	return val
}

func (o *Options) logCommand(command string) {
	attrs := []log.Attr{log.Any("command", redact{command, o})}
	if o.Stdin != nil {
		attrs = append(attrs, log.Bool("stdin", true))
	}
	if o.Sudo {
		attrs = append(attrs, log.Bool("sudo", true))
	}

	o.log(context.TODO(), log.LevelInfo, "exec", attrs...)
}

func (o *Options) logStdin(row string) {
	o.log(context.TODO(), log.LevelInfo, "stdin", log.Any("data", redact{row, o}))
}

func (o *Options) logStdout(row string) {
	o.log(context.TODO(), log.LevelInfo, "stdout", log.Any("data", redact{row, o}))
}

func (o *Options) logStderr(row string) {
	o.log(context.TODO(), log.LevelError, "stderr", log.Any("data", redact{row, o}))
}

func (o *Options) Defer(fn func()) {
	o.defers = append(o.defers, fn)
}

func (o *Options) Finalize() {
	for _, fn := range o.defers {
		fn()
	}
}

func (o *Options) InputReader() io.Reader {
	if o.Stdin == nil {
		return nil
	}

	if o.disableLogStreams {
		return o.Stdin
	}

	logWriter := iostream.ScanWriter(byte('\n'), o.logStdin)
	o.Defer(func() { logWriter.Close() })

	return io.TeeReader(o.Stdin, logWriter)
}

func (o *Options) ErrorWriter() io.Writer {
	var stderrWriters []io.Writer
	if o.Stderr != nil {
		stderrWriters = append(stderrWriters, o.Stderr)
	}

	if o.DisallowStderr {
		stderrWriters = append(stderrWriters, iostream.NopCallbackWriter(
			func() { o.wroteToStderr = true },
		))
	}

	if !o.disableLogStreams {
		logWriter := iostream.ScanWriter(byte('\n'), o.logStderr)
		stderrWriters = append(stderrWriters, logWriter)
		o.Defer(func() { logWriter.Close() })
	}

	return iostream.MuxWriter(io.MultiWriter(stderrWriters...))
}

func (o *Options) OutputWriter() io.Writer {
	var stdoutWriters []io.Writer
	if o.Stdout != nil {
		stdoutWriters = append(stdoutWriters, o.Stdout)
	}

	if !o.disableLogStreams {
		logWriter := iostream.ScanWriter(byte('\n'), o.logStdout)
		stdoutWriters = append(stdoutWriters, logWriter)
		o.Defer(func() { logWriter.Close() })
	}

	return iostream.MuxWriter(io.MultiWriter(stdoutWriters...))
}

func (o *Options) Command(cmd string) (string, error) {
	if !o.Sudo {
		return cmd, nil
	}

	if o.SudoFn == nil {
		return "", ErrSudoNotConfigured
	}

	newCmd, err := o.SudoFn(cmd)
	if err != nil {
		return "", fmt.Errorf("failed to sudo: %w", err)
	}

	return newCmd, nil
}
