package exec

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/k0sproject/rig/pkg/powershell"
)

const RedactMask = "[REDACTED]"

// Option is a functional option for the exec package
type Option func(*Options)

type RedactFunc func(string) string

type DecorateFunc func(string) string

// Options is a collection of exec options
type Options struct {
	in     io.Reader
	out    io.Writer
	errOut io.Writer

	allowWinStderr bool

	logInfo    bool
	logDebug   bool
	logError   bool
	logCommand bool
	logOutput  bool
	logInput   bool

	streamOutput bool
	trimOutput   bool

	wroteErr bool

	redactFuncs   []RedactFunc
	decorateFuncs []DecorateFunc
}

func decodeEncoded(cmd string) string {
	if !strings.Contains(cmd, "powershell") {
		return cmd
	}

	parts := strings.Split(cmd, " ")
	for i, p := range parts {
		if p == "-E" || p == "-EncodedCommand" && len(parts) > i+1 {
			decoded, err := base64.StdEncoding.DecodeString(parts[i+1])
			if err == nil {
				parts[i+1] = strings.ReplaceAll(string(decoded), "\x00", "")
			}
		}
	}
	return strings.Join(parts, " ")
}

func (o *Options) Command(cmd string) string {
	for _, decorator := range o.decorateFuncs {
		cmd = decorator(cmd)
	}
	return cmd
}

func (o *Options) Commandf(format string, args ...any) string {
	return o.Command(fmt.Sprintf(format, args...))
}

func (o *Options) AllowWinStderr() bool {
	return o.allowWinStderr
}

// LogCmd is for logging the command to be executed
func (o *Options) LogCmd(prefix, cmd string) {
	if Confirm {
		mutex.Lock()
		if !ConfirmFunc(fmt.Sprintf("\nHost: %s\nCommand: %s", prefix, o.Redact(decodeEncoded(cmd)))) {
			os.Stderr.WriteString("aborted\n")
			os.Exit(1)
		}
		mutex.Unlock()
	}

	if o.logCommand {
		DebugFunc("%s: executing `%s`", prefix, o.Redact(decodeEncoded(cmd)))
	} else {
		DebugFunc("%s: executing command", prefix)
	}
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

func (o *Options) Stdin() io.Reader {
	if o.in == nil {
		return nil
	}

	size, err := getReaderSize(o.in)
	if err == nil && size > 0 {
		DebugFunc("using %d bytes of data from reader as command input", size)
	} else {
		DebugFunc("using data from reader as command input")
	}

	if o.logInput {
		return io.TeeReader(o.in, redactingWriter{w: logWriter{fn: DebugFunc}, fn: o.Redact})
	}

	return o.in
}

func (o *Options) Stdout() io.Writer {
	var writers []io.Writer
	switch {
	case o.streamOutput:
		writers = append(writers, redactingWriter{w: logWriter{fn: InfoFunc}, fn: o.Redact})
	case o.logOutput:
		writers = append(writers, redactingWriter{w: logWriter{fn: DebugFunc}, fn: o.Redact})
	}
	if o.out != nil {
		writers = append(writers, o.out)
	}
	return io.MultiWriter(writers...)
}

func (o *Options) Stderr() io.Writer {
	var writers []io.Writer
	switch {
	case o.streamOutput:
		writers = append(writers, redactingWriter{w: logWriter{fn: ErrorFunc}, fn: o.Redact})
	case o.logError:
		writers = append(writers, redactingWriter{w: logWriter{fn: DebugFunc}, fn: o.Redact})
	}
	writers = append(writers, &flaggingWriter{b: &o.wroteErr})
	if o.errOut != nil {
		writers = append(writers, o.errOut)
	}

	return io.MultiWriter(writers...)
}

func (o *Options) WroteErr() bool {
	return o.wroteErr
}

// AllowWinStderr exec option allows command to output to stderr without failing
func AllowWinStderr() Option {
	return func(o *Options) {
		o.allowWinStderr = true
	}
}

// Redact is for filtering out sensitive text using a regexp
func (o *Options) Redact(s string) string {
	if DisableRedact || len(o.redactFuncs) == 0 {
		return s
	}
	for _, fn := range o.redactFuncs {
		s = fn(s)
	}
	return s
}

func (o *Options) FormatOutput(s string) string {
	if o.trimOutput {
		return strings.TrimSpace(s)
	}
	return s
}

// Stdin exec option for sending data to the command through stdin
func Stdin(r io.Reader) Option {
	return func(o *Options) {
		o.in = r
	}
}

func StdinString(s string) Option {
	return func(o *Options) {
		o.in = strings.NewReader(s)
	}
}

// Writer exec option for sending command stdout to an io.Writer
func Stdout(w io.Writer) Option {
	return func(o *Options) {
		o.out = w
	}
}

// ErrWriter exec option for sending command stderr to an io.Writer
func Stderr(w io.Writer) Option {
	return func(o *Options) {
		o.errOut = w
	}
}

// StreamOutput exec option for sending the command output to info log
func StreamOutput() Option {
	return func(o *Options) {
		o.streamOutput = true
	}
}

// LogError exec option for enabling or disabling live error logging during exec
func LogError(v bool) Option {
	return func(o *Options) {
		o.logError = v
	}
}

// HideCommand exec option for hiding the command-string and stdin contents from the logs
func HideCommand() Option {
	return func(o *Options) {
		o.logCommand = false
	}
}

// HideOutput exec option for hiding the command output from logs
func HideOutput() Option {
	return func(o *Options) {
		o.logOutput = false
		o.logError = false
	}
}

// Sensitive exec option for disabling all logging of the command
func Sensitive() Option {
	return func(o *Options) {
		o.logDebug = false
		o.logInfo = false
		o.logError = false
		o.logCommand = false
	}
}

// Redact exec option for defining a redact regexp pattern that will be replaced with [REDACTED] in the logs
func Redact(rexp string) Option {
	return func(o *Options) {
		re := regexp.MustCompile(rexp)
		o.redactFuncs = append(o.redactFuncs, func(s string) string {
			return re.ReplaceAllString(s, RedactMask)
		})
	}
}

// RedactString exec option for defining one or more strings to replace with [REDACTED] in the log output
func RedactString(s ...string) Option {
	var newS []string
	for _, str := range s {
		if str != "" {
			newS = append(newS, str)
		}
	}

	return func(o *Options) {
		o.redactFuncs = append(o.redactFuncs, func(s2 string) string {
			newstr := s2
			for _, r := range newS {
				newstr = strings.ReplaceAll(newstr, r, RedactMask)
			}
			return newstr
		})
	}
}

func LogInput(v bool) Option {
	return func(o *Options) {
		o.logInput = v
	}
}

func TrimOutput(v bool) Option {
	return func(o *Options) {
		o.trimOutput = v
	}
}

func PS() Option {
	return func(o *Options) {
		o.decorateFuncs = append(o.decorateFuncs, func(s string) string {
			return powershell.Cmd(s)
		})
	}
}

func PSCompressed() Option {
	return func(o *Options) {
		o.decorateFuncs = append(o.decorateFuncs, func(s string) string {
			return powershell.CompressedCmd(s)
		})
	}
}

// Build returns an instance of Options
func Build(opts ...Option) *Options {
	options := &Options{
		logInfo:      false,
		logCommand:   true,
		logDebug:     true,
		logError:     true,
		logOutput:    true,
		logInput:     false,
		streamOutput: false,
		trimOutput:   true,
	}

	for _, o := range opts {
		o(options)
	}

	return options
}
