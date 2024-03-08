package rigtest

import (
	"context"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/k0sproject/rig/v2/log"
)

var _ log.Logger = (*MockLogger)(nil)

// TraceToStderr sets the trace logger to log to stderr.
func TraceToStderr() {
	log.SetTraceLogger(slog.New(slog.NewTextHandler(os.Stderr, nil)))
}

// TraceOff sets the trace logger to null.
func TraceOff() {
	log.SetTraceLogger(log.Null)
}

// Trace allows running a function with trace logging enabled.
func Trace(fn func()) {
	TraceToStderr()
	fn()
	defer TraceOff()
}

// MockLogMessage is a mock log message.
type MockLogMessage struct {
	level         int
	message       string
	keysAndValues []any
}

// Level returns the log level of the message.
func (m MockLogMessage) Level() int {
	return m.level
}

// Message returns the log message.
func (m MockLogMessage) Message() string {
	return m.message
}

// KeysAndValues returns the log message's keys and values.
func (m MockLogMessage) KeysAndValues() []any {
	return m.keysAndValues
}

// String returns the log message as a string.
func (m MockLogMessage) String() string {
	sb := strings.Builder{}
	logger := slog.New(slog.NewTextHandler(&sb, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	logger.Log(context.Background(), slog.Level(m.level), m.message, m.keysAndValues...)
	return sb.String()
}

// MockLogger is a mock logger.
type MockLogger struct {
	mu       sync.Mutex
	messages []MockLogMessage
}

func (l *MockLogger) log(level int, t string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.messages = append(l.messages, MockLogMessage{level: level, message: t, keysAndValues: args})
}

// Reset clears the log messages.
func (l *MockLogger) Reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.messages = l.messages[:0]
}

// Len returns the number of log messages.
func (l *MockLogger) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.messages)
}

// Messages returns a copy of the log messages.
func (l *MockLogger) Messages() []MockLogMessage {
	l.mu.Lock()
	defer l.mu.Unlock()
	msgs := make([]MockLogMessage, len(l.messages))
	copy(msgs, l.messages)
	return msgs
}

// Received returns true if a log message with the given level and message was received.
func (l *MockLogger) Received(regex regexp.Regexp) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, msg := range l.messages {
		if regex.MatchString(msg.message) {
			return true
		}
	}
	return false
}

// ReceivedSubstring returns true if a log message with the given level and message was received.
func (l *MockLogger) ReceivedSubstring(substring string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, msg := range l.messages {
		if strings.Contains(msg.message, substring) {
			return true
		}
	}
	return false
}

// ReceivedString returns true if a log message with the given level and message was received.
func (l *MockLogger) ReceivedString(message string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, msg := range l.messages {
		if msg.message == message {
			return true
		}
	}
	return false
}

// Trace level log message.
func (l *MockLogger) Trace(t string, args ...any) { l.log(-8, t, args...) }

// Debug level log message.
func (l *MockLogger) Debug(t string, args ...any) { l.log(-4, t, args...) }

// Info level log message.
func (l *MockLogger) Info(t string, args ...any) { l.log(0, t, args...) }

// Warn level log message.
func (l *MockLogger) Warn(t string, args ...any) { l.log(2, t, args...) }

// Error level log message.
func (l *MockLogger) Error(t string, args ...any) { l.log(4, t, args...) }
