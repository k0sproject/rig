package cmd_test

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/k0sproject/rig/v2/cmd"
	"github.com/k0sproject/rig/v2/protocol/localhost"
	"github.com/k0sproject/rig/v2/rigtest"
	"github.com/stretchr/testify/require"
)

// execWithStderr runs a failing command that writes stderr and returns the
// resulting error, built by the real Executor rather than by hand.
func execWithStderr(t *testing.T, stderr string) error {
	t.Helper()
	failed := errors.New("exit status 1")
	mr := rigtest.NewMockRunner()
	mr.AddCommand(rigtest.Equal("foo"), func(a *rigtest.A) error {
		fmt.Fprintln(a.Stderr, stderr)
		return failed
	})
	err := mr.Exec("foo")
	require.Error(t, err)
	require.ErrorIs(t, err, failed)
	return err
}

func TestProcessErrorStderr(t *testing.T) {
	t.Run("short stderr appears in full in the message", func(t *testing.T) {
		err := execWithStderr(t, "chmod: /tmp/x: No such file or directory")

		require.Contains(t, err.Error(), "chmod: /tmp/x: No such file or directory")
		require.Equal(t, "chmod: /tmp/x: No such file or directory", cmd.StderrOf(err))
	})

	t.Run("long stderr is shortened in the message but kept in full", func(t *testing.T) {
		longPath := "/tmp/" + strings.Repeat("nested/", 30) + "missing.conf"
		stderr := "chmod: cannot access '" + longPath + "': No such file or directory"
		require.Greater(t, len(stderr), 100, "the fixture must exceed the message limit")

		err := execWithStderr(t, stderr)

		require.NotContains(t, err.Error(), "No such file or directory",
			"the reason is what the shortened message loses")
		require.Contains(t, err.Error(), "...")
		require.Equal(t, stderr, cmd.StderrOf(err), "the full output must survive on the error value")
	})

	t.Run("no stderr", func(t *testing.T) {
		failed := errors.New("exit status 1")
		mr := rigtest.NewMockRunner()
		mr.AddCommandFailure(rigtest.Equal("foo"), failed)

		err := mr.Exec("foo")
		require.ErrorIs(t, err, failed)
		require.Empty(t, cmd.StderrOf(err))
		require.Equal(t, "command result: process finished with error: exit status 1", err.Error())
	})

	t.Run("unrelated error carries no stderr", func(t *testing.T) {
		require.Empty(t, cmd.StderrOf(errors.New("boom")))
		require.Empty(t, cmd.StderrOf(nil))
	})

	t.Run("ProcessError is reachable with errors.As", func(t *testing.T) {
		err := execWithStderr(t, "boom")

		var procErr *cmd.ProcessError
		require.ErrorAs(t, err, &procErr)
		require.Equal(t, "boom", procErr.Stderr())
		require.EqualError(t, procErr.Unwrap(), "exit status 1")
	})
}

// statusErr reports its status the way x/crypto/ssh's ExitError does.
type statusErr struct{ status int }

func (e statusErr) Error() string   { return fmt.Sprintf("exit status %d", e.status) }
func (e statusErr) ExitStatus() int { return e.status }

// codeErr reports its status the way os/exec's ExitError does.
type codeErr struct{ code int }

func (e codeErr) Error() string { return fmt.Sprintf("exit code %d", e.code) }
func (e codeErr) ExitCode() int { return e.code }

func execFailingWith(t *testing.T, failure error) error {
	t.Helper()
	mr := rigtest.NewMockRunner()
	mr.AddCommandFailure(rigtest.Equal("foo"), failure)
	err := mr.Exec("foo")
	require.Error(t, err)
	return err
}

func TestProcessErrorExitCode(t *testing.T) {
	t.Run("ExitStatus", func(t *testing.T) {
		err := execFailingWith(t, fmt.Errorf("wait: %w", statusErr{status: 127}))
		require.Equal(t, 127, cmd.ExitCodeOf(err))

		var procErr *cmd.ProcessError
		require.ErrorAs(t, err, &procErr)
		require.Equal(t, 127, procErr.ExitCode())
	})

	t.Run("ExitCode", func(t *testing.T) {
		err := execFailingWith(t, fmt.Errorf("wait: %w", codeErr{code: 126}))
		require.Equal(t, 126, cmd.ExitCodeOf(err))
	})

	t.Run("no status on the error", func(t *testing.T) {
		err := execFailingWith(t, errors.New("connection lost"))
		require.Equal(t, -1, cmd.ExitCodeOf(err))
	})

	t.Run("not a command error", func(t *testing.T) {
		require.Equal(t, -1, cmd.ExitCodeOf(codeErr{code: 2}), "only a command's own status counts")
		require.Equal(t, -1, cmd.ExitCodeOf(nil))
	})
}

func TestProcessErrorExitCodeLocalhost(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell")
	}
	conn, err := localhost.NewConnection()
	require.NoError(t, err)
	executor := cmd.NewExecutor(conn)

	err = executor.ExecContext(t.Context(), "exit 3")
	require.Equal(t, 3, cmd.ExitCodeOf(err))

	// The shell's wording for a missing command varies, the status does not.
	err = executor.ExecContext(t.Context(), "rig-no-such-command-for-test")
	require.Equal(t, 127, cmd.ExitCodeOf(err), "stderr: %s", cmd.StderrOf(err))
}
