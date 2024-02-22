// Package rigtest provides testing utilities for mocking functionality of the rig package.
package rigtest

import (
	"context"
	"io"
	"regexp"
	"strings"
	"sync"

	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/protocol"
)

var _ protocol.Connection = (*MockConnection)(nil)

type matcher struct {
	fn       func(string) bool
	waiterFn MockWaiterFn
}

// MockConnection is a mock client. It can be used to simulate a client in tests.
type MockConnection struct {
	commands []string
	starter  protocol.ProcessStarter
	Windows  bool
	mu       sync.Mutex
}

// NewMockConnection creates a new mock connection.
func NewMockConnection() *MockConnection {
	return &MockConnection{
		starter: NewMockStarter(),
	}
}

// AddMockCommand adds a mock command to the client, see MockStarter.Add.
func (m *MockConnection) AddMockCommand(matcher *regexp.Regexp, waiterFn MockWaiterFn) {
	if starter, ok := m.starter.(*MockStarter); ok {
		starter.Add(matcher, waiterFn)
	}
}

// IsWindows returns true if the client is set to be a Windows client.
func (m *MockConnection) IsWindows() bool { return m.Windows }

// String returns the string representation of the client.
func (m *MockConnection) String() string { return "mockclient" }

// Protocol returns the protocol of the client.
func (m *MockConnection) Protocol() string { return "mock" }

// IPAddress returns the IP address of the client.
func (m *MockConnection) IPAddress() string { return "mock" }

// StartProcess simulates a start of a process on the client.
func (m *MockConnection) StartProcess(ctx context.Context, cmd string, stdin io.Reader, stdout io.Writer, stderr io.Writer) (exec.Waiter, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.commands = append(m.commands, cmd)
	return m.starter.StartProcess(ctx, cmd, stdin, stdout, stderr) //nolint:wrapcheck
}

// Reset clears the command history.
func (m *MockConnection) Reset() {
	m.commands = []string{}
}

// Received returns true if a command matching the given regular expression was received.
func (m *MockConnection) Received(matcher regexp.Regexp) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, cmd := range m.commands {
		if matcher.MatchString(cmd) {
			return true
		}
	}
	return false
}

// ReceivedSubstring returns true if a command containing the given substring was received.
func (m *MockConnection) ReceivedSubstring(substring string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, cmd := range m.commands {
		if strings.Contains(cmd, substring) {
			return true
		}
	}
	return false
}

// ReceivedString returns true if a command matching the given string was received.
func (m *MockConnection) ReceivedString(match string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, cmd := range m.commands {
		if cmd == match {
			return true
		}
	}
	return false
}

// Len returns the number of commands received.
func (m *MockConnection) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.commands)
}

// Commands returns a copy of the commands received.
func (m *MockConnection) Commands() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	dup := make([]string, len(m.commands))
	copy(dup, m.commands)
	return dup
}

// MockWaiterFn is a function that mocks what happens during the Wait method of a started process.
type MockWaiterFn func(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer) error

type mockWaiter struct {
	in     io.Reader
	out    io.Writer
	errOut io.Writer
	ctx    context.Context //nolint:containedctx
	fn     MockWaiterFn
}

func (m *mockWaiter) Wait() error {
	return m.fn(m.ctx, m.in, m.out, m.errOut)
}

// MockStarter is a mock process starter.
type MockStarter struct {
	ErrImmediately error
	matchers       []matcher
}

// StartProcess simulates a start of a process. You can add matchers to the starter to simulate different behaviors for different commands.
func (m *MockStarter) StartProcess(ctx context.Context, cmd string, stdin io.Reader, stdout io.Writer, stderr io.Writer) (exec.Waiter, error) {
	if m.ErrImmediately != nil {
		return nil, m.ErrImmediately
	}

	for _, matcher := range m.matchers {
		if matcher.waiterFn != nil && matcher.fn(cmd) {
			return &mockWaiter{in: stdin, out: stdout, errOut: stderr, ctx: ctx, fn: matcher.waiterFn}, nil
		}
	}

	return &mockWaiter{fn: func(_ context.Context, _ io.Reader, _, _ io.Writer) error { return nil }}, nil
}

// Add adds a matcher and a function to the starter.
func (m *MockStarter) Add(regex *regexp.Regexp, waiterFn MockWaiterFn) {
	m.matchers = append(m.matchers, matcher{fn: func(cmd string) bool { return regex.MatchString(cmd) }, waiterFn: waiterFn})
}

// NewMockStarter creates a new mock starter.
func NewMockStarter() *MockStarter {
	return &MockStarter{}
}
