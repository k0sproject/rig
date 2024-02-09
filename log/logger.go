// Package log provides a simple logging interface for rig
package log

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"sync"
)

// Logger interface should be implemented by the logging library you wish to use
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

// LoggerInjectable is a struct that can be embedded in other structs to provide a logger and a log setter
type LoggerInjectable struct {
	logger Logger
}

type injectable interface {
	SetLogger(logger Logger)
	Log() Logger
}

// InjectLogger sets the logger for the given object if it implements the LoggerInjectable interface
func InjectLogger(l Logger, obj any) {
	if o, ok := obj.(injectable); ok {
		o.SetLogger(l)
	}
}

// SetLogger sets the logger for the embedding object
func (li *LoggerInjectable) SetLogger(logger Logger) {
	li.logger = logger
}

// HasLogger returns true if a logger has been set
func (li *LoggerInjectable) HasLogger() bool {
	return li.logger != nil
}

// Log returns the logger for the embedding object
func (li *LoggerInjectable) Log() Logger {
	if li.logger == nil {
		return &NullLog{}
	}
	return li.logger
}

// NewStdLog creates a new StdLog instance
func NewStdLog(out io.Writer) *StdLog {
	if out == nil {
		out = os.Stderr
	}
	return &StdLog{out: out}
}

// StdLog is a simplistic logger for rig
type StdLog struct {
	out io.Writer
}

// Tracef prints a debug level log message
func (l *StdLog) Tracef(t string, args ...any) {
	fmt.Fprintln(l.out, keyTrace, fmt.Sprintf(t, args...))
}

// Debugf prints a debug level log message
func (l *StdLog) Debugf(t string, args ...any) {
	fmt.Fprintln(l.out, keyDebug, fmt.Sprintf(t, args...))
}

// Infof prints an info level log message
func (l *StdLog) Infof(t string, args ...any) {
	fmt.Fprintln(l.out, keyInfo, fmt.Sprintf(t, args...))
}

// Warnf prints a warn level log message
func (l *StdLog) Warnf(t string, args ...any) {
	fmt.Fprintln(l.out, keyWarn, fmt.Sprintf(t, args...))
}

// Errorf prints an error level log message
func (l *StdLog) Errorf(t string, args ...any) {
	fmt.Fprintln(l.out, keyError, fmt.Sprintf(t, args...))
}

// NewPrefixLog creates a new PrefixLog instance
func NewPrefixLog(log Logger, prefix string) *PrefixLog {
	if log == nil {
		log = NewStdLog(nil)
	}
	return &PrefixLog{log: log, prefix: prefix}
}

// PrefixLog is a logger that prefixes all log messages with a string
type PrefixLog struct {
	log    Logger
	prefix string
}

// Tracef prints a debug level log message
func (l *PrefixLog) Tracef(t string, args ...any) {
	l.log.Tracef(l.prefix+t, args...)
}

// Debugf prints a debug level log message
func (l *PrefixLog) Debugf(t string, args ...any) {
	l.log.Debugf(l.prefix+t, args...)
}

// Infof prints an info level log message
func (l *PrefixLog) Infof(t string, args ...any) {
	l.log.Infof(l.prefix+t, args...)
}

// Warnf prints a warn level log message
func (l *PrefixLog) Warnf(t string, args ...any) {
	l.log.Warnf(l.prefix+t, args...)
}

// Errorf prints an error level log message
func (l *PrefixLog) Errorf(t string, args ...any) {
	l.log.Errorf(l.prefix+t, args...)
}

// NullLog is a logger that does nothing
type NullLog struct{}

func (l *NullLog) Tracef(t string, args ...any) {} // Tracef does nothing
func (l *NullLog) Debugf(t string, args ...any) {} // Debugf does nothing
func (l *NullLog) Infof(t string, args ...any)  {} // Infof does nothing
func (l *NullLog) Warnf(t string, args ...any)  {} // Warnf does nothing
func (l *NullLog) Errorf(t string, args ...any) {} // Errorf does nothing

// MockLogMessage is a mock log message
type MockLogMessage struct {
	level   int
	message string
}

// Level returns the log level of the message
func (m MockLogMessage) Level() int {
	return m.level
}

// Message returns the log message
func (m MockLogMessage) Message() string {
	return m.message
}

// String returns the log message as a string
func (m MockLogMessage) String() string {
	switch m.level {
	case LevelTrace:
		return keyTrace + " " + m.message
	case LevelDebug:
		return keyDebug + " " + m.message
	case LevelInfo:
		return keyInfo + " " + m.message
	case LevelWarn:
		return keyWarn + " " + m.message
	case LevelError:
		return keyError + " " + m.message
	default:
		return "UNKNOWN " + m.message
	}
}

// MockLog is a mock logger
type MockLog struct {
	mu       sync.Mutex
	messages []MockLogMessage
}

func (l *MockLog) log(level int, t string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.messages = append(l.messages, MockLogMessage{level: level, message: fmt.Sprintf(t, args...)})
}

// Reset clears the log messages
func (l *MockLog) Reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.messages = l.messages[:0]
}

// Len returns the number of log messages
func (l *MockLog) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.messages)
}

// Messages returns a copy of the log messages
func (l *MockLog) Messages() []MockLogMessage {
	l.mu.Lock()
	defer l.mu.Unlock()
	msgs := make([]MockLogMessage, len(l.messages))
	copy(msgs, l.messages)
	return msgs
}

// Received returns true if a log message with the given level and message was received
func (l *MockLog) ReceivedRegex(level int, regex regexp.Regexp) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, msg := range l.messages {
		if msg.level == level && regex.MatchString(msg.message) {
			return true
		}
	}
	return false
}

// Received returns true if a log message with the given level and message was received
func (l *MockLog) ReceivedContains(level int, substring string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, msg := range l.messages {
		if msg.level == level && strings.Contains(msg.message, substring) {
			return true
		}
	}
	return false
}

func (l *MockLog) Tracef(t string, args ...any) { l.log(LevelTrace, t, args...) } // Tracef log message
func (l *MockLog) Debugf(t string, args ...any) { l.log(LevelDebug, t, args...) } // Debugf log message
func (l *MockLog) Infof(t string, args ...any)  { l.log(LevelInfo, t, args...) }  // Infof log message
func (l *MockLog) Warnf(t string, args ...any)  { l.log(LevelWarn, t, args...) }  // Warnf log message
func (l *MockLog) Errorf(t string, args ...any) { l.log(LevelError, t, args...) } // Errorf log message
