package cmd_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/k0sproject/rig/v2/cmd"
	"github.com/k0sproject/rig/v2/protocol"
	"github.com/k0sproject/rig/v2/rigtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tracerRecorder collects lifecycle events for assertions.
type tracerRecorder struct {
	mu     sync.Mutex
	events []string
}

func (r *tracerRecorder) CommandFormatted(host, formatted string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, "formatted:"+formatted)
}

func (r *tracerRecorder) ProcessStarted(host, formatted string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, "started:"+formatted)
}

func (r *tracerRecorder) ProcessFinished(host, formatted string, duration time.Duration, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err != nil {
		r.events = append(r.events, "finished-err:"+err.Error())
	} else {
		r.events = append(r.events, "finished-ok:"+formatted)
	}
}

// outputTracerRecorder also captures per-line output.
type outputTracerRecorder struct {
	tracerRecorder
	mu     sync.Mutex
	stdout []string
	stderr []string
}

type explainOnlyStarter struct {
	started []string
}

func (s *explainOnlyStarter) StartProcess(_ context.Context, command string, _ io.Reader, _ io.Writer, _ io.Writer) (protocol.Waiter, error) {
	s.started = append(s.started, command)
	return nil, errors.New("explain must not start a process")
}

func (r *outputTracerRecorder) StdoutLine(host, line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stdout = append(r.stdout, line)
}

func (r *outputTracerRecorder) StderrLine(host, line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stderr = append(r.stderr, line)
}

func TestTracerLifecycleOrder(t *testing.T) {
	mr := rigtest.NewMockRunner()
	mr.AddCommand(rigtest.Equal("echo hi"), func(_ *rigtest.A) error { return nil })

	tr := &tracerRecorder{}
	err := mr.Exec("echo hi", cmd.Trace(tr))
	require.NoError(t, err)

	require.Len(t, tr.events, 3)
	assert.Equal(t, "formatted:echo hi", tr.events[0])
	assert.Equal(t, "started:echo hi", tr.events[1])
	assert.Equal(t, "finished-ok:echo hi", tr.events[2])
}

func TestTracerRunnerWide(t *testing.T) {
	conn := rigtest.NewMockConnection()
	conn.AddCommand(rigtest.Equal("whoami"), func(_ *rigtest.A) error { return nil })
	runner := cmd.NewExecutor(conn)

	tr := &tracerRecorder{}
	runner.SetTracer(tr)

	require.NoError(t, runner.Exec("whoami"))
	require.Len(t, tr.events, 3)
	assert.Equal(t, "formatted:whoami", tr.events[0])
}

func TestTracerPerCallOverridesRunnerWide(t *testing.T) {
	conn := rigtest.NewMockConnection()
	conn.AddCommand(rigtest.Match("."), func(_ *rigtest.A) error { return nil })
	runner := cmd.NewExecutor(conn)

	runnerWide := &tracerRecorder{}
	perCall := &tracerRecorder{}
	runner.SetTracer(runnerWide)

	require.NoError(t, runner.Exec("hello", cmd.Trace(perCall)))

	assert.Empty(t, runnerWide.events, "runner-wide tracer should not fire when per-call overrides it")
	assert.Len(t, perCall.events, 3)
}

func TestTracerProcessFinishedOnError(t *testing.T) {
	mr := rigtest.NewMockRunner()
	execErr := errors.New("boom")
	mr.AddCommand(rigtest.Equal("fail"), func(_ *rigtest.A) error { return execErr })

	tr := &tracerRecorder{}
	err := mr.Exec("fail", cmd.Trace(tr))
	require.Error(t, err)

	require.Len(t, tr.events, 3)
	assert.Equal(t, "formatted:fail", tr.events[0])
	assert.Equal(t, "started:fail", tr.events[1])
	assert.Equal(t, "finished-err:process finished with error: boom", tr.events[2])
}

func TestOutputTracerPerLine(t *testing.T) {
	mr := rigtest.NewMockRunner()
	mr.AddCommand(rigtest.Equal("lines"), func(a *rigtest.A) error {
		fmt.Fprintln(a.Stdout, "line1")
		fmt.Fprintln(a.Stdout, "line2")
		fmt.Fprintln(a.Stderr, "err1")
		return nil
	})

	tr := &outputTracerRecorder{}
	err := mr.Exec("lines", cmd.Trace(tr))
	require.NoError(t, err)

	assert.Equal(t, []string{"line1", "line2"}, tr.stdout)
	assert.Equal(t, []string{"err1"}, tr.stderr)
}

func TestTracerDecoratedCommand(t *testing.T) {
	conn := rigtest.NewMockConnection()
	conn.AddCommand(rigtest.Match("."), func(_ *rigtest.A) error { return nil })

	globalDec := func(c string) string { return "sudo " + c }
	runner := cmd.NewExecutor(conn, globalDec)

	tr := &tracerRecorder{}
	require.NoError(t, runner.Exec("whoami", cmd.Trace(tr)))

	require.Len(t, tr.events, 3)
	// CommandFormatted must receive the fully decorated string
	assert.Equal(t, "formatted:sudo whoami", tr.events[0])
}

// --- Explain tests ---

func TestExplainPlainCommand(t *testing.T) {
	mr := rigtest.NewMockRunner()
	ex := mr.Explain("ls -la")
	assert.Equal(t, "ls -la", ex.Formatted)
	assert.Equal(t, "ls -la", ex.Decoded)
	assert.Equal(t, "ls -la", ex.Logged)
	assert.True(t, ex.CommandLogged)
	// OSWrappingKnown is false until IsWindows() has been called (OS not yet cached).
	assert.False(t, ex.OSWrappingKnown)
}

func TestExplainWithGlobalDecorator(t *testing.T) {
	conn := rigtest.NewMockConnection()
	globalDec := func(c string) string { return "sudo " + c }
	runner := cmd.NewExecutor(conn, globalDec)

	ex := runner.Explain("whoami")
	assert.Equal(t, "sudo whoami", ex.Formatted)
	assert.Equal(t, "sudo whoami", ex.Decoded)
}

func TestExplainWithPerCallDecorator(t *testing.T) {
	conn := rigtest.NewMockConnection()
	runner := cmd.NewExecutor(conn)

	callDec := func(c string) string { return "env X=1 " + c }
	ex := runner.Explain("myapp", cmd.Decorate(callDec))
	assert.Equal(t, "env X=1 myapp", ex.Formatted)
}

func TestExplainWindowsWrapping(t *testing.T) {
	conn := rigtest.NewMockConnection()
	conn.Windows = true
	runner := cmd.NewExecutor(conn)

	// Prime the OS cache so Explain can include OS-specific wrapping.
	runner.IsWindows()

	ex := runner.Explain("k0s version")
	assert.Equal(t, "cmd.exe /C k0s version", ex.Formatted)
	assert.True(t, ex.OSWrappingKnown)

	exExe := runner.Explain("k0s.exe version")
	assert.Equal(t, "k0s.exe version", exExe.Formatted, "*.exe commands must not be double-wrapped")
}

func TestExplainDoesNotProbeWindows(t *testing.T) {
	starter := &explainOnlyStarter{}
	runner := cmd.NewExecutor(starter)

	ex := runner.Explain("k0s version")

	assert.Equal(t, "k0s version", ex.Formatted)
	assert.False(t, ex.OSWrappingKnown)
	assert.Empty(t, starter.started, "Explain must not start a process to detect OS wrapping")
}

func TestExplainDoesNotProbeWindowsChecker(t *testing.T) {
	// A WindowsChecker connection whose IsWindows() records calls — Explain must not call it.
	conn := rigtest.NewMockConnection()
	conn.Windows = true
	runner := cmd.NewExecutor(conn)

	// Do NOT call runner.IsWindows() — OS cache is empty.
	ex := runner.Explain("k0s version")

	assert.Equal(t, "k0s version", ex.Formatted, "OS wrapping must not be applied when OS is not yet cached")
	assert.False(t, ex.OSWrappingKnown)
	assert.Empty(t, conn.Commands(), "Explain must not start any process")
}

func TestOutputTracerNoOutputDoesNotDeadlock(t *testing.T) {
	mr := rigtest.NewMockRunner()
	mr.AddCommand(rigtest.Equal("silent"), func(_ *rigtest.A) error { return nil })

	tr := &outputTracerRecorder{}
	done := make(chan error, 1)
	go func() {
		done <- mr.Exec("silent", cmd.Trace(tr))
	}()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Exec with OutputTracer deadlocked on a command that produced no output")
	}

	assert.Empty(t, tr.stdout)
	assert.Empty(t, tr.stderr)
}

func TestExplainCommandLogSuppression(t *testing.T) {
	mr := rigtest.NewMockRunner()

	ex := mr.Explain("curl -H token", cmd.HideCommand())

	assert.Equal(t, "curl -H token", ex.Formatted)
	assert.False(t, ex.CommandLogged)
	assert.Empty(t, ex.Logged)
}

func TestExplainRedaction(t *testing.T) {
	mr := rigtest.NewMockRunner()
	secret := "s3cr3t"
	ex := mr.Explain("curl -u admin:"+secret, cmd.Redact(secret))

	// Formatted is the wire command — secret must be present
	assert.Contains(t, ex.Formatted, secret)
	// Logged is the log view — secret must be masked
	assert.NotContains(t, ex.Logged, secret)
	assert.Contains(t, ex.Logged, cmd.DefaultRedactMask)
}

func TestExplainMatchesStart(t *testing.T) {
	conn := rigtest.NewMockConnection()
	conn.AddCommand(rigtest.Match("."), func(_ *rigtest.A) error { return nil })

	globalDec := func(c string) string { return "env VAR=1 " + c }
	runner := cmd.NewExecutor(conn, globalDec)

	callDec := func(c string) string { return "sudo " + c }

	ex := runner.Explain("myapp run", cmd.Decorate(callDec))
	require.NoError(t, runner.Exec("myapp run", cmd.Decorate(callDec)))

	rigtest.ReceivedEqual(t, conn, ex.Formatted)
}
