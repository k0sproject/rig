package rigtest

import (
	"context"
	"io"
	"regexp"
	"strings"
	"sync"

	"github.com/k0sproject/rig"
	"github.com/k0sproject/rig/exec"
)

// MockClient is a mock client. It can be used to simulate a client in tests.
type MockClient struct {
	commands []string
	starter  rig.ProcessStarter
	Windows  bool
	mu       sync.Mutex
}

// NewMockClient creates a new mock client
func NewMockClient() *MockClient {
	return &MockClient{
		starter: NewMockStarter(),
	}
}

// AddMockCommand adds a mock command to the client, see MockStarter.Add
func (m *MockClient) AddMockCommand(matcher *regexp.Regexp, waiterFn MockWaiterFn) {
	if starter, ok := m.starter.(*MockStarter); ok {
		starter.Add(matcher, waiterFn)
	}
}

// IsWindows returns true if the client is set to be a Windows client
func (m *MockClient) IsWindows() bool { return m.Windows }

// String returns the string representation of the client
func (m *MockClient) String() string { return "mockclient" }

// Protocol returns the protocol of the client
func (m *MockClient) Protocol() string { return "mock" }

// IPAddress returns the IP address of the client
func (m *MockClient) IPAddress() string { return "mock" }

// StartProcess simulates a start of a process on the client
func (m *MockClient) StartProcess(ctx context.Context, cmd string, stdin io.Reader, stdout io.Writer, stderr io.Writer) (exec.Waiter, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.commands = append(m.commands, cmd)
	return m.starter.StartProcess(ctx, cmd, stdin, stdout, stderr)
}

// Reset clears the command history
func (m *MockClient) Reset() {
	m.commands = []string{}
}

// Received returns true if a command matching the given regular expression was received
func (m *MockClient) Received(matcher regexp.Regexp) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, cmd := range m.commands {
		if matcher.MatchString(cmd) {
			return true
		}
	}
	return false
}

// ReceivedSubstring returns true if a command containing the given substring was received
func (m *MockClient) ReceivedSubstring(substring string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, cmd := range m.commands {
		if strings.Contains(cmd, substring) {
			return true
		}
	}
	return false
}

// ReceivedString returns true if a command matching the given string was received
func (m *MockClient) ReceivedString(match string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, cmd := range m.commands {
		if cmd == match {
			return true
		}
	}
	return false
}

// Len returns the number of commands received
func (m *MockClient) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.commands)
}

// Commands returns a copy of the commands received
func (m *MockClient) Commands() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	dup := make([]string, len(m.commands))
	copy(dup, m.commands)
	return dup
}

// MockWaiterFn is a function that mocks what happens during the Wait method of a started process
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

// MockStarter is a mock process starter
type MockStarter struct {
	ErrImmediately error
	matchers       map[*regexp.Regexp]MockWaiterFn
}

// StartProcess simulates a start of a process. You can add matchers to the starter to simulate different behaviors for different commands.
func (m *MockStarter) StartProcess(ctx context.Context, cmd string, stdin io.Reader, stdout io.Writer, stderr io.Writer) (exec.Waiter, error) {
	if m.ErrImmediately != nil {
		return nil, m.ErrImmediately
	}

	for matcher, waiter := range m.matchers {
		if matcher.MatchString(cmd) && waiter != nil {
			return &mockWaiter{in: stdin, out: stdout, errOut: stderr, ctx: ctx, fn: waiter}, nil
		}
	}

	return &mockWaiter{fn: func(_ context.Context, _ io.Reader, _, _ io.Writer) error { return nil }}, nil
}

// Add adds a matcher and a function to the starter.
func (m *MockStarter) Add(matcher *regexp.Regexp, waiterFn MockWaiterFn) {
	m.matchers[matcher] = waiterFn
}

// NewMockStarter creates a new mock starter
func NewMockStarter() *MockStarter {
	return &MockStarter{
		matchers: make(map[*regexp.Regexp]MockWaiterFn),
	}
}
