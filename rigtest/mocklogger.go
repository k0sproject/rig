package rigtest

import (
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/k0sproject/rig/log"
)

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
	case log.LevelTrace:
		return "TRACE " + m.message
	case log.LevelDebug:
		return "DEBUG " + m.message
	case log.LevelInfo:
		return "INFO  " + m.message
	case log.LevelWarn:
		return "WARN  " + m.message
	case log.LevelError:
		return "ERROR " + m.message
	default:
		return "???   " + m.message
	}
}

// MockLogger is a mock logger
type MockLogger struct {
	mu       sync.Mutex
	messages []MockLogMessage
}

func (l *MockLogger) log(level int, t string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.messages = append(l.messages, MockLogMessage{level: level, message: fmt.Sprintf(t, args...)})
}

// Reset clears the log messages
func (l *MockLogger) Reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.messages = l.messages[:0]
}

// Len returns the number of log messages
func (l *MockLogger) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.messages)
}

// Messages returns a copy of the log messages
func (l *MockLogger) Messages() []MockLogMessage {
	l.mu.Lock()
	defer l.mu.Unlock()
	msgs := make([]MockLogMessage, len(l.messages))
	copy(msgs, l.messages)
	return msgs
}

// Received returns true if a log message with the given level and message was received
func (l *MockLogger) Received(level int, regex regexp.Regexp) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, msg := range l.messages {
		if msg.level == level && regex.MatchString(msg.message) {
			return true
		}
	}
	return false
}

// ReceivedSubstring returns true if a log message with the given level and message was received
func (l *MockLogger) ReceivedSubstring(level int, substring string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, msg := range l.messages {
		if msg.level == level && strings.Contains(msg.message, substring) {
			return true
		}
	}
	return false
}

// ReceivedString returns true if a log message with the given level and message was received
func (l *MockLogger) ReceivedString(level int, message string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, msg := range l.messages {
		if msg.level == level && msg.message == message {
			return true
		}
	}
	return false
}

// Tracef log message
func (l *MockLogger) Tracef(t string, args ...any) { l.log(log.LevelTrace, t, args...) }

// Debugf log message
func (l *MockLogger) Debugf(t string, args ...any) { l.log(log.LevelDebug, t, args...) }

// Infof log message
func (l *MockLogger) Infof(t string, args ...any) { l.log(log.LevelInfo, t, args...) }

// Warnf log message
func (l *MockLogger) Warnf(t string, args ...any) { l.log(log.LevelWarn, t, args...) }

// Errorf log message
func (l *MockLogger) Errorf(t string, args ...any) { l.log(log.LevelError, t, args...) }
