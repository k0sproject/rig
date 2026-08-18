package cmd_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/k0sproject/rig/v2/cmd"
	"github.com/k0sproject/rig/v2/rigtest"
	"github.com/k0sproject/rig/v2/sh"
	"github.com/stretchr/testify/require"
)

func TestSimpleExec(t *testing.T) {
	mr := rigtest.NewMockRunner()

	mr.AddCommand(rigtest.Equal("true"), func(a *rigtest.A) error { return nil })
	mr.AddCommand(rigtest.Equal("false"), func(a *rigtest.A) error { return errors.New("foo") })

	require.NoError(t, mr.Exec("true"))
	require.ErrorContains(t, mr.Exec("false"), "foo")
}

func TestWindowsShell(t *testing.T) {
	mr := rigtest.NewMockRunner()
	mr.Windows = true
	_ = mr.Exec("echo hello")
	rigtest.ReceivedEqual(t, mr, "cmd.exe /C echo hello", "commands should by default be run through cmd.exe")
	_ = mr.Exec("foo.exe foo")
	rigtest.ReceivedWithPrefix(t, mr, "foo.exe", "commands starting with *.exe should be run directly")
}

func TestSetShell(t *testing.T) {
	t.Run("posix commands are wrapped", func(t *testing.T) {
		conn := rigtest.NewMockConnection()
		runner := cmd.NewExecutor(conn)
		runner.SetShell(sh.DefaultShell)
		require.NoError(t, runner.Exec("echo hello"))
		rigtest.ReceivedEqual(t, conn, `/bin/sh -c -- 'echo hello'`,
			"the imposed shell must run the command instead of the remote user's login shell")
	})

	t.Run("no shell means no wrapping", func(t *testing.T) {
		conn := rigtest.NewMockConnection()
		runner := cmd.NewExecutor(conn)
		require.NoError(t, runner.Exec("echo hello"))
		rigtest.ReceivedEqual(t, conn, "echo hello")
	})

	t.Run("windows commands are not wrapped", func(t *testing.T) {
		conn := rigtest.NewMockConnection()
		conn.Windows = true
		runner := cmd.NewExecutor(conn)
		runner.SetShell(sh.DefaultShell)
		require.NoError(t, runner.Exec("echo hello"))
		rigtest.ReceivedEqual(t, conn, "cmd.exe /C echo hello")
		rigtest.NotReceivedContains(t, conn, sh.DefaultShell)
	})

	t.Run("chained runners wrap once", func(t *testing.T) {
		conn := rigtest.NewMockConnection()
		base := cmd.NewExecutor(conn)
		base.SetShell(sh.DefaultShell)
		// A decorated runner on top of the base one, like the sudo runners are.
		chained := cmd.NewExecutor(base, func(command string) string { return "sudo -n -- " + sh.Shell(command) })
		require.NoError(t, chained.Exec("echo hello"))

		// The inner shell belongs to the decorator, the outer one to the base
		// runner. The base runner must not add a second layer of its own.
		rigtest.ReceivedEqual(t, conn, `/bin/sh -c -- 'sudo -n -- /bin/sh -c -- '"'"'echo hello'"'"''`)
	})
}

// TestRedactSurvivesQuoting covers a secret that contains a single quote. Both
// the sudo decorators and the imposed shell quote what they wrap, which splits
// such a secret into pieces a literal redacter can no longer match, so
// redaction has to happen before the command is formatted.
func TestRedactSurvivesQuoting(t *testing.T) {
	const secret = "pa'ss"

	var gated string
	conn := rigtest.NewMockConnection()
	runner := cmd.NewExecutor(conn)
	runner.SetShell(sh.DefaultShell)
	runner.SetCommandGate(cmd.CommandGateFunc(func(_ context.Context, _, command string) error {
		gated = command
		return nil
	}))

	require.NoError(t, runner.Exec(`login "`+secret+`"`, cmd.Redact(secret)))

	require.NotContains(t, gated, secret, "the gate must not be shown the secret")
	require.Contains(t, gated, "[REDACTED]")
	// The host still gets the real command: the mask is for humans only. The
	// secret itself is not contiguous there, since quoting it is what broke
	// redaction in the first place.
	rigtest.NotReceivedContains(t, conn, "[REDACTED]", "the mask must never be sent to the host")
	rigtest.ReceivedContains(t, conn, "login")

	explanation := runner.Explain(`login "`+secret+`"`, cmd.Redact(secret))
	require.NotContains(t, explanation.Logged, secret, "Explain's loggable form must not contain the secret")
}

// TestRedactOrdinarySecretNeedsNoReplay covers the common case: a secret that
// quoting cannot split is masked in the formatted command itself. The decorators
// must not be re-run for it, so the described command is exactly the one sent,
// even for a decorator that varies with the command it is given.
func TestRedactOrdinarySecretNeedsNoReplay(t *testing.T) {
	const secret = "s3cr3t"

	var gated string
	conn := rigtest.NewMockConnection()
	// A decorator that looks at the command it is given. Formatting a masked
	// command would take the other branch.
	runner := cmd.NewExecutor(conn, func(command string) string {
		if strings.Contains(command, secret) {
			return "with-secret " + command
		}
		return "without-secret " + command
	})
	runner.SetShell(sh.DefaultShell)
	runner.SetCommandGate(cmd.CommandGateFunc(func(_ context.Context, _, command string) error {
		gated = command
		return nil
	}))

	require.NoError(t, runner.Exec("login "+secret, cmd.Redact(secret)))

	require.NotContains(t, gated, secret)
	require.Contains(t, gated, "with-secret", "the described command must be the one that ran")
	rigtest.ReceivedContains(t, conn, "with-secret")
}

// TestRedactSecretIntroducedByDecorator covers a secret that is not in the
// command at all but added by a decorator, which a later decorator or the shell
// wrapping then quotes. Masking has to happen after each decorator for this to
// be caught.
func TestRedactSecretIntroducedByDecorator(t *testing.T) {
	const secret = "pa'ss"

	var gated string
	conn := rigtest.NewMockConnection()
	runner := cmd.NewExecutor(conn, func(command string) string {
		return `PASSWORD="` + secret + `" ` + command
	})
	runner.SetShell(sh.DefaultShell)
	runner.SetCommandGate(cmd.CommandGateFunc(func(_ context.Context, _, command string) error {
		gated = command
		return nil
	}))

	require.NoError(t, runner.Exec("login", cmd.Redact(secret)))

	require.NotContains(t, gated, secret, "a secret added by a decorator must be masked too")
	require.Contains(t, gated, "[REDACTED]")
}

// TestChainedRunnerReportsFullCommand covers a chained runner, as the sudo
// runners are: the shell is imposed by the base runner, so part of the command
// comes from a runner further down the chain. Logs, the gate, tracers and Explain
// must describe the command the host receives, not just this runner's part of it.
func TestChainedRunnerReportsFullCommand(t *testing.T) {
	const want = `/bin/sh -c -- 'sudo -n -- /bin/sh -c -- '"'"'echo hello'"'"''`

	var gated string
	conn := rigtest.NewMockConnection()
	base := cmd.NewExecutor(conn)
	base.SetShell(sh.DefaultShell)
	chained := cmd.NewExecutor(base, func(command string) string { return "sudo -n -- " + sh.Shell(command) })
	chained.SetCommandGate(cmd.CommandGateFunc(func(_ context.Context, _, command string) error {
		gated = command
		return nil
	}))
	tracer := &tracerRecorder{}
	chained.SetTracer(tracer)

	require.NoError(t, chained.Exec("echo hello"))

	rigtest.ReceivedEqual(t, conn, want)
	require.Equal(t, want, gated, "the gate must see the command as the host receives it")
	// The Tracer contract promises the final command after all wrapping.
	require.Contains(t, tracer.events, "formatted:"+want)
	require.Contains(t, tracer.events, "started:"+want)

	explanation := chained.Explain("echo hello")
	require.Equal(t, want, explanation.Formatted)
	// Logged must not stack a second parent layer on top of Formatted.
	require.Equal(t, want, explanation.Logged)
}

// TestChainedRunnerRunsDecoratorsOnce pins that a parent runner's decorators are
// applied once per command. A decorator is an exported callback, so running one
// twice can have side effects of its own and, if it is not a pure function of its
// input, produce a command that differs from the one reported.
func TestChainedRunnerRunsDecoratorsOnce(t *testing.T) {
	conn := rigtest.NewMockConnection()
	calls := 0
	base := cmd.NewExecutor(conn, func(command string) string {
		calls++
		return "env VAR=1 " + command
	})
	base.SetShell(sh.DefaultShell)
	chained := cmd.NewExecutor(base, func(command string) string { return "sudo -n -- " + sh.Shell(command) })

	require.NoError(t, chained.Exec("echo hello"))

	require.Equal(t, 1, calls, "the parent's decorator must run once per command")
	require.Equal(t, 1, strings.Count(conn.LastCommand(), "env VAR=1"), "got %q", conn.LastCommand())
}

// TestStartProcessImposesShell covers the chaining entry point. Start resolves
// the host's OS before formatting, but a runner that is not a [cmd.Executor] can
// call StartProcess directly, and the imposed shell must not be skipped just
// because nothing has probed the OS yet.
func TestStartProcessImposesShell(t *testing.T) {
	conn := rigtest.NewMockConnection()
	runner := cmd.NewExecutor(conn)
	runner.SetShell(sh.DefaultShell)

	waiter, err := runner.StartProcess(context.Background(), "echo hello", nil, nil, nil)
	require.NoError(t, err)
	require.NoError(t, waiter.Wait())

	rigtest.ReceivedEqual(t, conn, `/bin/sh -c -- 'echo hello'`)
}

// TestRedactSecretIntroducedByParentDecorator is
// [TestRedactSecretIntroducedByDecorator] for a decorator on the parent runner,
// whose formatting is replayed to describe what a chained runner sends.
func TestRedactSecretIntroducedByParentDecorator(t *testing.T) {
	const secret = "pa'ss"

	var gated string
	conn := rigtest.NewMockConnection()
	base := cmd.NewExecutor(conn, func(command string) string {
		return `PASSWORD="` + secret + `" ` + command
	})
	base.SetShell(sh.DefaultShell)
	chained := cmd.NewExecutor(base, func(command string) string { return "sudo -n -- " + sh.Shell(command) })
	chained.SetCommandGate(cmd.CommandGateFunc(func(_ context.Context, _, command string) error {
		gated = command
		return nil
	}))

	require.NoError(t, chained.Exec("login", cmd.Redact(secret)))

	require.NotContains(t, gated, secret, "a secret added by a parent decorator must be masked too")
	require.Contains(t, gated, "[REDACTED]")
}

func TestSetShellExplain(t *testing.T) {
	conn := rigtest.NewMockConnection()
	runner := cmd.NewExecutor(conn)
	runner.SetShell(sh.DefaultShell)

	// Explain must not probe the host for its OS, so before anything has run the
	// OS-specific wrapping is left out and flagged as such.
	explanation := runner.Explain("echo hello")
	require.Equal(t, "echo hello", explanation.Formatted)
	require.False(t, explanation.OSWrappingKnown)

	require.NoError(t, runner.Exec("echo hello"))

	explanation = runner.Explain("echo hello")
	require.Equal(t, `/bin/sh -c -- 'echo hello'`, explanation.Formatted)
	require.True(t, explanation.OSWrappingKnown)
}

func TestPrintfErrors(t *testing.T) {
	mr := rigtest.NewMockRunner()
	args := []interface{}{"hello"}
	err := mr.Exec(fmt.Sprintf("echo %s %d", args...)) // intentional error
	require.ErrorIs(t, err, cmd.ErrInvalidCommand, "commands with printf errors should return ErrInvalidCommand")
	require.ErrorContains(t, err, "refusing", "commands with printf errors should return a helpful error message")
}

func TestExecOutput(t *testing.T) {
	mr := rigtest.NewMockRunner()
	mr.AddCommandOutput(rigtest.Equal("foo"), "bar\n")
	out, err := mr.ExecOutput("foo")
	require.NoError(t, err)
	require.Equal(t, "bar", out)
	out, err = mr.ExecOutput("foo", cmd.TrimOutput(false))
	require.NoError(t, err)
	require.Equal(t, "bar\n", out)
}

func TestStderrError(t *testing.T) {
	mr := rigtest.NewMockRunner()
	mr.AddCommand(rigtest.HasSuffix("foo"), func(a *rigtest.A) error {
		fmt.Fprintln(a.Stderr, "baz")
		return errors.New("foo")
	})
	err := mr.Exec("foo")
	require.Error(t, err)
	require.Equal(t, "command result: process finished with error: foo (baz)", err.Error())
}

func TestStderrErrorWindows(t *testing.T) {
	rigtest.TraceToStderr()
	defer rigtest.TraceOff()
	conn := rigtest.NewMockConnection()
	conn.Windows = true
	conn.AddCommand(rigtest.HasSuffix("foo"), func(a *rigtest.A) error {
		fmt.Fprintln(a.Stderr, "baz")
		return nil
	})
	runner := cmd.NewExecutor(conn)
	err := runner.Exec("foo")
	require.Error(t, err)
	require.Equal(t, "command result: process finished with error: command wrote output to stderr (baz)", err.Error())
}

func TestStderrErrorWindowsAllow(t *testing.T) {
	conn := rigtest.NewMockConnection()
	conn.Windows = true
	conn.AddCommand(rigtest.Equal("foo"), func(a *rigtest.A) error {
		fmt.Fprintln(a.Stderr, "baz")
		return nil
	})
	runner := cmd.NewExecutor(conn)
	err := runner.Exec("foo", cmd.AllowWinStderr())
	require.NoError(t, err)
}

func TestStdinInput(t *testing.T) {
	mr := rigtest.NewMockRunner()
	var readN int64
	mr.AddCommand(rigtest.Equal("foo"), func(a *rigtest.A) error {
		readN, _ = io.Copy(a.Stdout, a.Stdin)
		return nil
	})
	out, err := mr.ExecOutput("foo", cmd.StdinString("barbar"))
	require.NoError(t, err)
	require.Equal(t, "barbar", out)
	require.Equal(t, 6, int(readN))
}

func TestBackground(t *testing.T) {
	mr := rigtest.NewMockRunner()
	mr.AddCommand(rigtest.Equal("foo"), func(_ *rigtest.A) error {
		return errors.New("error from mock wait")
	})
	cmd, err := mr.StartBackground("foo")
	require.NoError(t, err)
	rigtest.ReceivedEqual(t, mr, "foo")
	require.ErrorContains(t, cmd.Wait(), "error from mock wait")

}
