// Package log contains rig's logging related types, constants and functions.
package log

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

var (
	// Null logger is a no-op logger that does nothing.
	Null = slog.New(Discard)

	trace = sync.OnceValue(func() TraceLogger {
		return Null
	})
)

const (
	// Keys for log attributes.

	// KeyHost is the host name or address.
	KeyHost = "host"

	// KeyExitCode is the exit code of a command.
	KeyExitCode = "exitCode"

	// KeyError is an error.
	KeyError = "error"

	// KeyBytes is the number of bytes.
	KeyBytes = "bytes"

	// KeyDuration is the duration of an operation.
	KeyDuration = "duration"

	// KeyCommand is a command-line.
	KeyCommand = "command"

	// KeyFile is a file name.
	KeyFile = "file"

	// KeySudo is a boolean indicating whether a command is run with sudo.
	KeySudo = "sudo"

	// KeyProtocol is a network protocol.
	KeyProtocol = "protocol"

	// KeyComponent is a component name.
	KeyComponent = "component"
)

// SetTraceLogger sets a trace logger. Some of the rig's internal logging is sent to a separate trace logger. It should be
// quite rare to use this function outside of rig development. It, and all the log.Trace calls will likely be removed
// once the code is confirmed to be working as expected.
func SetTraceLogger(l TraceLogger) {
	trace = sync.OnceValue(func() TraceLogger { return l })
}

// GetTraceLogger gets the current value of trace logger.
func GetTraceLogger() TraceLogger {
	return trace()
}

// TraceLogger is a logger for rig's internal trace logging.
type TraceLogger interface {
	Log(ctx context.Context, level slog.Level, msg string, keysAndValues ...any)
}

// HostAttr returns a host log attribute.
func HostAttr(conn fmt.Stringer) slog.Attr {
	return slog.String(KeyHost, conn.String())
}

// ErrorAttr returns an error log attribute.
func ErrorAttr(err error) slog.Attr {
	if err == nil {
		return slog.Attr{Key: KeyError, Value: slog.StringValue("")}
	}
	return slog.Attr{Key: KeyError, Value: slog.StringValue(err.Error())}
}

// FileAttr returns a file/path log attribute.
func FileAttr(file string) slog.Attr {
	return slog.String(KeyFile, file)
}

// Trace is for rig's internal trace logging that must be separately enabled by
// providing a [TraceLogger] logger, which is implemented by slog.Logger.
func Trace(ctx context.Context, msg string, keysAndValues ...any) {
	trace().Log(ctx, slog.LevelInfo, msg, keysAndValues...)
}

// Logger interface is implemented by slog.Logger and some other logging packages
// and can be easily used via a wrapper with any other logging system.
// The functions are not sprintf-style. Keys and values are key-value pairs.
type Logger interface {
	Debug(msg string, keysAndValues ...any)
	Info(msg string, keysAndValues ...any)
	Warn(msg string, keysAndValues ...any)
	Error(msg string, keysAndValues ...any)
}

type withAttrs struct {
	logger Logger
	attrs  []any
}

func (w *withAttrs) kv(kv []any) []any {
	return append(w.attrs, kv...)
}

func (w *withAttrs) Debug(msg string, keysAndValues ...any) {
	w.logger.Debug(msg, w.kv(keysAndValues)...)
}

func (w *withAttrs) Info(msg string, keysAndValues ...any) {
	w.logger.Info(msg, w.kv(keysAndValues)...)
}

func (w *withAttrs) Warn(msg string, keysAndValues ...any) {
	w.logger.Warn(msg, w.kv(keysAndValues)...)
}

func (w *withAttrs) Error(msg string, keysAndValues ...any) {
	w.logger.Error(msg, w.kv(keysAndValues)...)
}

// WithAttrs returns a logger that prepends the given attributes to all log messages.
func WithAttrs(logger Logger, attrs ...any) Logger {
	// TODO inspect the popular loggers if they share some common interface and use it instead.
	return &withAttrs{logger, attrs}
}

// LoggerInjectable is a struct that can be embedded in other structs to provide a logger and a log setter.
type LoggerInjectable struct {
	logger Logger
}

// Log interface is implemented by the LoggerInjectable struct.
type Log interface {
	Log() Logger
}

type injectable interface {
	LogWithAttrs(attrs ...any) Logger
	InjectLoggerTo(obj any, attrs ...any)
	SetLogger(logger Logger)
	Log() Logger
}

// InjectLogger sets the logger for the given object if it implements the injectable interface.
func InjectLogger(l Logger, obj any, attrs ...any) {
	if o, ok := obj.(injectable); ok {
		if len(attrs) > 0 {
			o.SetLogger(WithAttrs(l, attrs...))
		} else {
			o.SetLogger(l)
		}
	} else {
		Trace(context.Background(), "logger is not injectable", slog.String("object", fmt.Sprintf("%T", obj)))
	}
}

// HasLogger returns true if the object implements the Log interface and has a logger set.
func HasLogger(obj any) bool {
	if o, ok := obj.(Log); ok {
		return o.Log() != nil && o.Log() != Null
	}
	return false
}

// GetLogger returns the logger for the given object if it implements the Log interface or a Null logger.
func GetLogger(obj any) Logger {
	if o, ok := obj.(Log); ok {
		return o.Log()
	}
	return Null
}

// InjectLoggerTo sets the logger for the given object if it implements the injectable interface based on the logger of the current object, optionally with extra attributes.
func (li *LoggerInjectable) InjectLoggerTo(obj any, attrs ...any) {
	if li.HasLogger() {
		InjectLogger(li.logger, obj, attrs...)
	}
}

// SetLogger sets the logger for the embedding object.
func (li *LoggerInjectable) SetLogger(logger Logger) {
	li.logger = logger
}

// HasLogger returns true if a logger has been set.
func (li *LoggerInjectable) HasLogger() bool {
	return li.logger != nil && li.logger != Null
}

// Log returns the logger for the embedding object.
func (li *LoggerInjectable) Log() Logger {
	if li.logger == nil {
		return Null
	}
	return li.logger
}

// LogWithAttrs returns an instance of the logger with the given attributes applied.
func (li *LoggerInjectable) LogWithAttrs(attrs ...any) Logger {
	return WithAttrs(li.Log(), attrs...)
}
