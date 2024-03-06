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

	"github.com/k0sproject/rig/cmd"
	"github.com/k0sproject/rig/log"
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

// Not returns a CommandMatcher that negates the result of the given match function.
func Not(matchFn CommandMatcher) CommandMatcher {
	return func(cmd string) bool {
		return !matchFn(cmd)
	}
}

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

// Match returns a CommandMatcher that checks if a command matches a given regular expression.
func Match(pattern string) CommandMatcher {
	regex := regexp.MustCompile(pattern)
	return func(cmd string) bool {
		return regex.MatchString(cmd)
	}
}

// MockRunner runs commands on a mock connection.
type MockRunner struct {
	log.LoggerInjectable
	cmd.Runner
	*MockConnection
	*MockStarter
}

// NewMockRunner creates a new mock runner.
func NewMockRunner() *MockRunner {
	connection := NewMockConnection()
	return &MockRunner{
		Runner:         cmd.NewExecutor(connection),
		MockConnection: connection,
		MockStarter:    connection.MockStarter,
	}
}

// MockConnection is a mock client. It can be used to simulate a client in tests.
type MockConnection struct {
	log.LoggerInjectable
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

func (m *MockRunner) StartProcess(ctx context.Context, cmd string, stdin io.Reader, stdout io.Writer, stderr io.Writer) (protocol.Waiter, error) {
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
func (m *MockConnection) StartProcess(ctx context.Context, cmd string, stdin io.Reader, stdout io.Writer, stderr io.Writer) (protocol.Waiter, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.commands = append(m.commands, cmd)
	return m.MockStarter.StartProcess(ctx, cmd, stdin, stdout, stderr)
}

// Reset clears the command history.
func (m *MockConnection) Reset() {
	m.commands = []string{}
}

// Received returns an error unless a command matching the given matcher was received.
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

// NotReceived returns an error if a command matching the given regular expression was received.
func (m *MockConnection) NotReceived(matchFn CommandMatcher) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, cmd := range m.commands {
		if matchFn(cmd) {
			return errors.New("a matching command was received") //nolint:goerr113
		}
	}
	return nil
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

func (m *MockWaiter) Close() error {
	log.Trace(m.ctx, "closing waiter streams", log.KeyCommand, m.cmd)
	if in, ok := m.in.(io.Closer); ok {
		log.Trace(m.ctx, "closing stdin", log.KeyCommand, m.cmd)
		_ = in.Close()
	}

	if out, ok := m.out.(io.Closer); ok {
		log.Trace(m.ctx, "closing stdout", log.KeyCommand, m.cmd)
		_ = out.Close()
	}
	if errOut, ok := m.errOut.(io.Closer); ok {
		log.Trace(m.ctx, "closing stderr", log.KeyCommand, m.cmd)
		_ = errOut.Close()
	}
	return nil
}

// Wait simulates a process wait.
func (m *MockWaiter) Wait() error {
	defer m.Close()
	log.Trace(m.ctx, "running suplied function", log.KeyCommand, m.cmd)
	defer log.Trace(m.ctx, "function returned", log.KeyCommand, m.cmd)
	a := &A{Ctx: m.ctx, Stdin: m.in, Stdout: m.out, Stderr: m.errOut, Command: m.cmd}
	return m.fn(a)
}

// MockStarter is a mock process starter.
type MockStarter struct {
	ErrImmediate bool
	ErrDefault   error
	matchers     []matcher
}

// StartProcess simulates a start of a process. You can add matchers to the starter to simulate different behaviors for different commands.
func (m *MockStarter) StartProcess(ctx context.Context, cmd string, stdin io.Reader, stdout io.Writer, stderr io.Writer) (protocol.Waiter, error) {
	log.Trace(ctx, "start process", log.KeyCommand, cmd)
	if m.ErrImmediate && m.ErrDefault != nil {
		log.Trace(ctx, "returning immediately", log.KeyCommand, cmd, log.KeyError, m.ErrDefault)
		return nil, m.ErrDefault
	}

	for _, matcher := range m.matchers {
		if matcher.waiterFn != nil && matcher.fn(cmd) {
			log.Trace(ctx, "matched command using matcher", log.KeyCommand, cmd)
			return &MockWaiter{in: stdin, out: stdout, errOut: stderr, ctx: ctx, fn: matcher.waiterFn, cmd: cmd}, nil
		}
	}

	log.Trace(ctx, "no match found for command", log.KeyCommand, cmd)
	w := &MockWaiter{in: stdin, out: stdout, errOut: stderr, ctx: ctx, cmd: cmd, fn: func(_ *A) error { return m.ErrDefault }}
	return w, nil
}

// AddCommand adds a mocked command handler which is called when the matcher matches the command line.
func (m *MockStarter) AddCommand(matchFn CommandMatcher, waitFn CommandHandler) {
	m.matchers = append(m.matchers, matcher{fn: matchFn, waiterFn: waitFn})
}

// AddCommandOutput adds a matcher and a function to the starter that writes the given output to the stdout of the process.
func (m *MockStarter) AddCommandOutput(matchFn CommandMatcher, output string) {
	m.matchers = append(m.matchers, matcher{fn: matchFn, waiterFn: func(a *A) error {
		log.Trace(a.Ctx, "writing output to stdout", log.KeyCommand, a.Command, "output", output)
		_, err := fmt.Fprint(a.Stdout, output)
		if err != nil {
			return fmt.Errorf("command stdout write: %w", err)
		}
		log.Trace(a.Ctx, "output written to stdout", log.KeyCommand, a.Command)
		return nil
	}})
}

// AddCommandOutput adds a matcher and a function to the starter that writes the given output to the stdout of the process.
func (m *MockStarter) AddCommandSuccess(matchFn CommandMatcher) {
	m.matchers = append(m.matchers, matcher{fn: matchFn, waiterFn: func(a *A) error {
		log.Trace(a.Ctx, "returning nil from command", log.KeyCommand, a.Command)
		return nil
	}})
}

// AddCommandOutput adds a matcher and a function to the starter that writes the given output to the stdout of the process.
func (m *MockStarter) AddCommandFailure(matchFn CommandMatcher, err error) {
	m.matchers = append(m.matchers, matcher{fn: matchFn, waiterFn: func(a *A) error {
		log.Trace(a.Ctx, "returning error from command", log.KeyCommand, a.Command, log.KeyError, err)
		return err
	}})
}

// NewMockStarter creates a new mock starter.
func NewMockStarter() *MockStarter {
	return &MockStarter{}
}
