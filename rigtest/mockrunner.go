// Package rigtest provides testing utilities for mocking functionality of the rig package.
package rigtest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"

	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/protocol"
)

var _ protocol.Connection = (*MockConnection)(nil)

type matcher struct {
	fn       CommandMatcher
	waiterFn CommandHandler
}

// A is the struct passed to the command handling functions.
type A struct {
	// Ctx is the context passed to the command
	Ctx context.Context //nolint:containedctx
	// Stdin is the standard input of the command
	Stdin io.Reader
	// Stdout is the standard output of the command
	Stdout io.Writer
	// Stderr is the standard error of the command
	Stderr io.Writer
	// Command is the command line
	Command string
}

// CommandHandler is a function that handles a mocked command.
type CommandHandler func(a *A) error

// CommandMatcher is a function that checks if a command matches a certain criteria.
type CommandMatcher func(string) bool

// HasPrefix returns a CommandMatcher that checks if a command starts with a given prefix.
func HasPrefix(prefix string) CommandMatcher {
	return func(cmd string) bool {
		return strings.HasPrefix(cmd, prefix)
	}
}

// HasSuffix returns a CommandMatcher that checks if a command ends with a given suffix.
func HasSuffix(suffix string) CommandMatcher {
	return func(cmd string) bool {
		return strings.HasSuffix(cmd, suffix)
	}
}

// Contains returns a CommandMatcher that checks if a command contains a given substring.
func Contains(substring string) CommandMatcher {
	return func(cmd string) bool {
		return strings.Contains(cmd, substring)
	}
}

// Equal returns a CommandMatcher that checks if a command equals a given string.
func Equal(str string) CommandMatcher {
	return func(cmd string) bool {
		return cmd == str
	}
}

// Matches returns a CommandMatcher that checks if a command matches a given regular expression.
func Matches(pattern string) CommandMatcher {
	regex := regexp.MustCompile(pattern)
	return func(cmd string) bool {
		return regex.MatchString(cmd)
	}
}

// MockRunner runs commands on a mock connection.
type MockRunner struct {
	exec.Runner
	*MockConnection
	*MockStarter
}

// NewMockRunner creates a new mock runner.
func NewMockRunner() *MockRunner {
	connection := NewMockConnection()
	return &MockRunner{
		Runner:         exec.NewHostRunner(connection),
		MockConnection: connection,
		MockStarter:    connection.MockStarter,
	}
}

// MockConnection is a mock client. It can be used to simulate a client in tests.
type MockConnection struct {
	commands []string
	*MockStarter
	Windows bool
	mu      sync.Mutex
}

// NewMockConnection creates a new mock connection.
func NewMockConnection() *MockConnection {
	return &MockConnection{
		MockStarter: NewMockStarter(),
	}
}

// IsWindows returns true if the runner's connection is set to be a Windows client.
func (m *MockRunner) IsWindows() bool {
	return m.Windows
}

// String returns the string representation of the runner.
func (m *MockRunner) String() string {
	return "[MockRunner] " + m.MockConnection.String()
}

func (m *MockRunner) StartProcess(ctx context.Context, cmd string, stdin io.Reader, stdout io.Writer, stderr io.Writer) (exec.Waiter, error) {
	return m.MockConnection.StartProcess(ctx, cmd, stdin, stdout, stderr)
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
	return m.MockStarter.StartProcess(ctx, cmd, stdin, stdout, stderr)
}

// Reset clears the command history.
func (m *MockConnection) Reset() {
	m.commands = []string{}
}

// Received returns true if a command matching the given regular expression was received.
func (m *MockConnection) Received(matchFn CommandMatcher) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, cmd := range m.commands {
		if matchFn(cmd) {
			return nil
		}
	}
	return errors.New("a matching command was not received") //nolint:goerr113
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

// LastCommand returns the last command received.
func (m *MockConnection) LastCommand() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.commands) == 0 {
		return ""
	}
	return m.commands[len(m.commands)-1]
}

// MockWaiter is a mock process waiter.
type MockWaiter struct {
	cmd    string
	in     io.Reader
	out    io.Writer
	errOut io.Writer
	ctx    context.Context //nolint:containedctx
	fn     CommandHandler
}

// Wait simulates a process wait.
func (m *MockWaiter) Wait() error {
	return m.fn(&A{Ctx: m.ctx, Stdin: m.in, Stdout: m.out, Stderr: m.errOut, Command: m.cmd})
}

// MockStarter is a mock process starter.
type MockStarter struct {
	ErrImmediate bool
	ErrDefault   error
	matchers     []matcher
}

// StartProcess simulates a start of a process. You can add matchers to the starter to simulate different behaviors for different commands.
func (m *MockStarter) StartProcess(ctx context.Context, cmd string, stdin io.Reader, stdout io.Writer, stderr io.Writer) (exec.Waiter, error) {
	if m.ErrImmediate && m.ErrDefault != nil {
		return nil, m.ErrDefault
	}

	for _, matcher := range m.matchers {
		if matcher.waiterFn != nil && matcher.fn(cmd) {
			return &MockWaiter{in: stdin, out: stdout, errOut: stderr, ctx: ctx, fn: matcher.waiterFn}, nil
		}
	}

	return &MockWaiter{fn: func(_ *A) error { return m.ErrDefault }}, nil
}

// AddCommand adds a mocked command handler which is called when the matcher matches the command line.
func (m *MockStarter) AddCommand(matchFn CommandMatcher, waitFn CommandHandler) {
	m.matchers = append(m.matchers, matcher{fn: matchFn, waiterFn: waitFn})
}

// AddCommandOutput adds a matcher and a function to the starter that writes the given output to the stdout of the process.
func (m *MockStarter) AddCommandOutput(matchFn CommandMatcher, output string) {
	m.matchers = append(m.matchers, matcher{fn: matchFn, waiterFn: func(a *A) error {
		_, err := a.Stdout.Write([]byte(output))
		if err != nil {
			return fmt.Errorf("command stdout write: %w", err)
		}
		return nil
	}})
}

// NewMockStarter creates a new mock starter.
func NewMockStarter() *MockStarter {
	return &MockStarter{}
}
