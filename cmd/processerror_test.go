package cmd_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/k0sproject/rig/v2/cmd"
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
