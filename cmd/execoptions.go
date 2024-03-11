package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/k0sproject/rig/v2/iostream"
	"github.com/k0sproject/rig/v2/log"
	"github.com/k0sproject/rig/v2/powershell"
	"github.com/k0sproject/rig/v2/redact"
)

// DefaultRedactMask is the string that will be used to replace redacted text in the logs.
const DefaultRedactMask = "[REDACTED]"

// ExecOption is a functional option for the exec package.
type ExecOption func(*ExecOptions)

// ExecOptions is a collection of exec options.
type ExecOptions struct {
	log.LoggerInjectable

	in     io.Reader
	out    io.Writer
	errOut io.Writer

	errBuf *bytes.Buffer

	allowWinStderr bool

	logInfo    bool
	logDebug   bool
	logError   bool
	logCommand bool
	logOutput  bool
	logInput   bool

	streamOutput bool
	trimOutput   bool

	redactStrings []string
	decorateFuncs []DecorateFunc
	redactMask    string
}

// Command returns the command string with all decorators applied.
func (o *ExecOptions) Command(cmd string) string {
	for _, decorator := range o.decorateFuncs {
		cmd = decorator(cmd)
	}
	return cmd
}

// AllowWinStderr returns the allowWinStderr option.
func (o *ExecOptions) AllowWinStderr() bool {
	return o.allowWinStderr
}

// LogCommand returns true if the command should be logged, false if not.
func (o *ExecOptions) LogCommand() bool {
	return o.logCommand
}

var (
	errCharDevice    = errors.New("reader is a character device")
	errUnknownReader = errors.New("unknown type of reader")
)

func getReaderSize(reader io.Reader) (int64, error) {
	switch v := reader.(type) {
	case *bytes.Buffer:
		return int64(v.Len()), nil
	case *os.File:
		stat, err := v.Stat()
		if err != nil {
			return 0, fmt.Errorf("failed to stat reader: %w", err)
		}

		if stat.Mode()&os.ModeCharDevice != 0 {
			return 0, errCharDevice
		}

		return stat.Size(), nil
	default:
		return 0, errUnknownReader
	}
}

// RedactReader returns a reader that will redact sensitive content from the input.
func (o *ExecOptions) RedactReader(r io.Reader) io.Reader {
	return redact.Reader(r, o.redactMask, o.redactStrings...)
}

// RedactWriter returns a writer that will redact sensitive content from the output.
func (o *ExecOptions) RedactWriter(w io.Writer) io.Writer {
	return redact.Writer(w, o.redactMask, o.redactStrings...)
}

// Redacter returns a [redact.Rredacter] prepared with the redact mask and string matchers set via the options.
func (o *ExecOptions) Redacter() redact.Redacter {
	return redact.StringRedacter(o.redactMask, o.redactStrings...)
}

// Redact returns a redacted version of the given string.
func (o *ExecOptions) Redact(s string) string {
	return o.Redacter().Redact(s)
}

// Stdin returns the Stdin reader. If input logging is enabled, it will be a TeeReader that writes to the log.
func (o *ExecOptions) Stdin() io.Reader {
	if o.in == nil {
		return nil
	}

	size, err := getReaderSize(o.in)
	if err == nil && size > 0 {
		log.Trace(context.Background(), "using data from reader as command input", log.KeyBytes, size)
	} else {
		log.Trace(context.Background(), "using data from reader as command input")
	}

	if o.logInput {
		return io.TeeReader(o.in, redact.Writer(logWriter{fn: o.Log().Debug}, DefaultRedactMask, o.redactStrings...))
	}

	return o.in
}

func (o *ExecOptions) logWriter(stream string, logFn func(msg string, keysAndValues ...any)) io.WriteCloser {
	return redact.Writer(iostream.NewScanWriter(func(s string) { logFn(s, "stream", stream) }), o.redactMask, o.redactStrings...)
}

// Stdout returns the Stdout writer. If output logging is enabled, it will be a MultiWriter that writes to the log.
func (o *ExecOptions) Stdout() io.Writer {
	var writers []io.Writer
	switch {
	case o.streamOutput:
		writers = append(writers, o.logWriter("stdout", o.Log().Info))
	case o.logOutput:
		writers = append(writers, o.logWriter("stdout", o.Log().Debug))
	}
	if o.out != nil {
		writers = append(writers, o.out)
	}
	return io.MultiWriter(writers...)
}

// Stderr returns the Stderr writer. If error logging is enabled, it will be a MultiWriter that writes to the log.
func (o *ExecOptions) Stderr() io.Writer {
	var writers []io.Writer
	switch {
	case o.streamOutput:
		writers = append(writers, o.logWriter("stderr", o.Log().Error))
	case o.logError:
		writers = append(writers, o.logWriter("stderr", o.Log().Debug))
	}
	writers = append(writers, o.errBuf)
	if o.errOut != nil {
		writers = append(writers, o.errOut)
	}

	return io.MultiWriter(writers...)
}

// ErrString returns the contents of stderr after exec.
func (o *ExecOptions) ErrString() string {
	return o.errBuf.String()
}

// AllowWinStderr exec option allows command to output to stderr without failing.
func AllowWinStderr() ExecOption {
	return func(o *ExecOptions) {
		o.allowWinStderr = true
	}
}

// RedactString filters out sensitive content from strings.
func (o *ExecOptions) RedactString(s string) string {
	if DisableRedact || len(o.redactStrings) == 0 {
		return s
	}
	for _, rs := range o.redactStrings {
		s = strings.ReplaceAll(s, rs, o.redactMask)
	}
	return s
}

// FormatOutput is for trimming whitespace from the command output if TrimOutput is enabled.
func (o *ExecOptions) FormatOutput(s string) string {
	if o.trimOutput {
		return strings.TrimSpace(s)
	}
	return s
}

// Stdin exec option for sending data to the command through stdin.
func Stdin(r io.Reader) ExecOption {
	return func(o *ExecOptions) {
		o.in = r
	}
}

// StdinString exec option for sending string data to the command through stdin.
func StdinString(s string) ExecOption {
	return func(o *ExecOptions) {
		o.in = strings.NewReader(s)
	}
}

// Stdout exec option for sending command stdout to an io.Writer.
func Stdout(w io.Writer) ExecOption {
	return func(o *ExecOptions) {
		o.out = w
	}
}

// Stderr exec option for sending command stderr to an io.Writer.
func Stderr(w io.Writer) ExecOption {
	return func(o *ExecOptions) {
		o.errOut = w
	}
}

// StreamOutput exec option for sending the command output to info log.
func StreamOutput() ExecOption {
	return func(o *ExecOptions) {
		o.streamOutput = true
	}
}

// LogError exec option for enabling or disabling live error logging during exec.
func LogError(v bool) ExecOption {
	return func(o *ExecOptions) {
		o.logError = v
	}
}

// HideCommand exec option for hiding the command-string and stdin contents from the logs.
func HideCommand() ExecOption {
	return func(o *ExecOptions) {
		o.logCommand = false
	}
}

// HideOutput exec option for hiding the command output from logs.
func HideOutput() ExecOption {
	return func(o *ExecOptions) {
		o.logOutput = false
		o.logError = false
	}
}

// Sensitive exec option for disabling all logging of the command.
func Sensitive() ExecOption {
	return func(o *ExecOptions) {
		o.logDebug = false
		o.logInfo = false
		o.logError = false
		o.logCommand = false
	}
}

// Redact exec option for defining a redact regexp pattern that will be replaced with [REDACTED] in the logs.
func Redact(match string) ExecOption {
	return func(o *ExecOptions) {
		o.redactStrings = append(o.redactStrings, match)
	}
}

// LogInput exec option for enabling or disabling live input logging during exec.
func LogInput(v bool) ExecOption {
	return func(o *ExecOptions) {
		o.logInput = v
	}
}

// TrimOutput exec option for controlling if the output of the command will be trimmed of whitespace.
func TrimOutput(v bool) ExecOption {
	return func(o *ExecOptions) {
		o.trimOutput = v
	}
}

// PS exec option for using powershell to execute the command on windows.
func PS() ExecOption {
	return func(o *ExecOptions) {
		o.decorateFuncs = append(o.decorateFuncs, powershell.Cmd)
	}
}

// PSCompressed is like PS but for long command scriptlets. The script will be gzipped
// and base64 encoded and includes a small decompression script at the beginning of the command.
// This can allow running longer scripts than the 8191 characters that powershell.exe allows.
func PSCompressed() ExecOption {
	return func(o *ExecOptions) {
		o.decorateFuncs = append(o.decorateFuncs, powershell.CompressedCmd)
	}
}

// Decorate exec option for applying a custom decorator to the command string.
func Decorate(decorator DecorateFunc) ExecOption {
	return func(o *ExecOptions) {
		o.decorateFuncs = append(o.decorateFuncs, decorator)
	}
}

// Logger exec option for setting a custom logger.
func Logger(l log.Logger) ExecOption {
	return func(o *ExecOptions) {
		o.SetLogger(l)
	}
}

// Build returns an instance of Options.
func Build(opts ...ExecOption) *ExecOptions {
	options := &ExecOptions{
		logInfo:      false,
		logCommand:   true,
		logDebug:     true,
		logError:     true,
		logOutput:    true,
		logInput:     false,
		streamOutput: false,
		trimOutput:   true,
		redactMask:   DefaultRedactMask,
		errBuf:       bytes.NewBuffer(nil),
	}

	options.Apply(opts...)

	return options
}

// Apply the supplied options to the Options.
func (o *ExecOptions) Apply(opts ...ExecOption) {
	for _, opt := range opts {
		opt(o)
	}
}

// a writer that calls a logging function for each line written.
type logWriter struct {
	fn func(string, ...any)
}

// Write writes the given bytes to the log function.
func (l logWriter) Write(p []byte) (int, error) {
	s := string(p)
	l.fn(s)
	return len(p), nil
}
