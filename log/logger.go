// Package log provides a simple logging interface for rig
package log

import (
	"fmt"
	"io"
	"os"
)

// Logger interface should be implemented by the logging library you wish to use.
type Logger interface {
	Tracef(msg string, args ...any)
	Debugf(msg string, args ...any)
	Infof(msg string, args ...any)
	Warnf(msg string, args ...any)
	Errorf(msg string, args ...any)
}

const (
	keyTrace = "TRACE" // Trace log level title
	keyDebug = "DEBUG" // Debug log level title
	keyInfo  = "INFO " // Info log level title
	keyWarn  = "WARN " // Warn log level title
	keyError = "ERROR" // Error log level title

	LevelTrace = iota // LevelTrace log level
	LevelDebug        // LevelDebug log level
	LevelInfo         // LevelInfo log level
	LevelWarn         // LevelWarn log level
	LevelError        // LevelError log level
)

// LoggerInjectable is a struct that can be embedded in other structs to provide a logger and a log setter.
type LoggerInjectable struct {
	logger Logger
}

// Log interface is implemented by the LoggerInjectable struct.
type Log interface {
	Log() Logger
}

type injectable interface {
	SetLogger(logger Logger)
	Log() Logger
}

// InjectLogger sets the logger for the given object if it implements the LoggerInjectable interface.
func InjectLogger(l Logger, obj any) {
	if o, ok := obj.(injectable); ok {
		o.SetLogger(l)
	}
}

// SetLogger sets the logger for the embedding object.
func (li *LoggerInjectable) SetLogger(logger Logger) {
	li.logger = logger
}

// HasLogger returns true if a logger has been set.
func (li *LoggerInjectable) HasLogger() bool {
	return li.logger != nil
}

// Log returns the logger for the embedding object.
func (li *LoggerInjectable) Log() Logger {
	if li.logger == nil {
		return &NullLog{}
	}
	return li.logger
}

// NewStdLog creates a new StdLog instance.
func NewStdLog(out io.Writer) *StdLog {
	if out == nil {
		out = os.Stderr
	}
	return &StdLog{out: out}
}

// StdLog is a simplistic logger for rig.
type StdLog struct {
	out io.Writer
}

// Tracef prints a debug level log message.
func (l *StdLog) Tracef(t string, args ...any) {
	fmt.Fprintln(l.out, keyTrace, fmt.Sprintf(t, args...))
}

// Debugf prints a debug level log message.
func (l *StdLog) Debugf(t string, args ...any) {
	fmt.Fprintln(l.out, keyDebug, fmt.Sprintf(t, args...))
}

// Infof prints an info level log message.
func (l *StdLog) Infof(t string, args ...any) {
	fmt.Fprintln(l.out, keyInfo, fmt.Sprintf(t, args...))
}

// Warnf prints a warn level log message.
func (l *StdLog) Warnf(t string, args ...any) {
	fmt.Fprintln(l.out, keyWarn, fmt.Sprintf(t, args...))
}

// Errorf prints an error level log message.
func (l *StdLog) Errorf(t string, args ...any) {
	fmt.Fprintln(l.out, keyError, fmt.Sprintf(t, args...))
}

// NewPrefixLog creates a new PrefixLog instance.
func NewPrefixLog(log Logger, prefix string) *PrefixLog {
	if log == nil {
		log = NewStdLog(nil)
	}
	return &PrefixLog{log: log, prefix: prefix}
}

// PrefixLog is a logger that prefixes all log messages with a string.
type PrefixLog struct {
	log    Logger
	prefix string
}

// Tracef prints a debug level log message.
func (l *PrefixLog) Tracef(t string, args ...any) {
	l.log.Tracef(l.prefix+t, args...)
}

// Debugf prints a debug level log message.
func (l *PrefixLog) Debugf(t string, args ...any) {
	l.log.Debugf(l.prefix+t, args...)
}

// Infof prints an info level log message.
func (l *PrefixLog) Infof(t string, args ...any) {
	l.log.Infof(l.prefix+t, args...)
}

// Warnf prints a warn level log message.
func (l *PrefixLog) Warnf(t string, args ...any) {
	l.log.Warnf(l.prefix+t, args...)
}

// Errorf prints an error level log message.
func (l *PrefixLog) Errorf(t string, args ...any) {
	l.log.Errorf(l.prefix+t, args...)
}

// NullLog is a logger that does nothing.
type NullLog struct{}

// Tracef does nothing.
func (l *NullLog) Tracef(_ string, _ ...any) {}

// Debugf does nothing.
func (l *NullLog) Debugf(_ string, _ ...any) {}

// Infof does nothing.
func (l *NullLog) Infof(_ string, _ ...any) {}

// Warnf does nothing.
func (l *NullLog) Warnf(_ string, _ ...any) {}

// Errorf does nothing.
func (l *NullLog) Errorf(_ string, _ ...any) {}
