// Package log provides a simple logging interface for rig
package log

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

var (
	Null = slog.New(Discard)

	trace = sync.OnceValue(func() TraceLogger {
		return Null
	})
)

const (
	KeyHost      = "host"
	KeyExitCode  = "exitCode"
	KeyError     = "error"
	KeyBytes     = "bytes"
	KeyDuration  = "duration"
	KeyCommand   = "command"
	KeyFile      = "file"
	KeySudo      = "sudo"
	KeyProtocol  = "protocol"
	KeyComponent = "component"
)

func HostAttr(conn fmt.Stringer) slog.Attr {
	return slog.String(KeyHost, conn.String())
}

func ErrorAttr(err error) slog.Attr {
	if err == nil {
		return slog.Attr{Key: KeyError, Value: slog.StringValue("")}
	}
	return slog.Attr{Key: KeyError, Value: slog.StringValue(err.Error())}
}

func FileAttr(file string) slog.Attr {
	return slog.String(KeyFile, file)
}

func SetTraceLogger(l TraceLogger) {
	trace = sync.OnceValue(func() TraceLogger { return l })
}

// Trace is for rig's internal trace logging that must be separately enabled by
// providing a [TraceLogger] logger, which is implemented by slog.Logger.
func Trace(ctx context.Context, msg string, keysAndValues ...any) {
	trace().Log(ctx, slog.LevelDebug, msg, keysAndValues...)
}

type TraceLogger interface {
	Log(ctx context.Context, level slog.Level, msg string, keysAndValues ...any)
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

func WithAttrs(logger Logger, attrs ...any) Logger {
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
	InjectLogger(obj any)
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

func (li *LoggerInjectable) LogWithAttrs(attrs ...any) Logger {
	return WithAttrs(li.Log(), attrs...)
}
