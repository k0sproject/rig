package rigtest_test

import (
	"context"
	"errors"
	"testing"

	"github.com/k0sproject/rig/rigtest"
	"github.com/stretchr/testify/require"
)

func TestAddAndProcessMockCommand(t *testing.T) {
	mr := rigtest.NewMockRunner()
	expectedOutput := "mock output"

	mr.AddCommand(rigtest.Equal("test"), func(a *rigtest.A) error {
		a.Stdout.Write([]byte(expectedOutput))
		return nil
	})

	out, err := mr.ExecOutput("test")
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	require.Equal(t, expectedOutput, out)
	require.NoError(t, mr.Received(rigtest.HasPrefix("test")))
}

func TestCommandHistoryAndReset(t *testing.T) {
	mr := rigtest.NewMockRunner()
	require.Zero(t, mr.Len())
	require.NoError(t, mr.Exec("command1"))
	require.Equal(t, 1, mr.Len())
	require.NoError(t, mr.Exec("command2"))
	require.Equal(t, 2, mr.Len())
	rigtest.ReceivedContains(t, mr, "command1")
	rigtest.ReceivedContains(t, mr, "command2")
	require.Equal(t, "command2", mr.LastCommand())
	mr.Reset()
	require.Zero(t, mr.Len())
	require.Equal(t, "", mr.LastCommand())
	require.Empty(t, mr.Commands())
}

func TestIsWindows(t *testing.T) {
	mr := rigtest.NewMockRunner()
	require.False(t, mr.IsWindows())
	mr.Windows = true
	require.True(t, mr.IsWindows())
}

func TestDefaultErrors(t *testing.T) {
	mr := rigtest.NewMockRunner()
	defaultErr := errors.New("default error")
	mr.ErrDefault = defaultErr

	t.Run("Immediate", func(t *testing.T) {
		mr.ErrImmediate = true
		cmd, err := mr.Start(context.Background(), "unknowncommand")
		require.ErrorIs(t, err, defaultErr)
		require.Nil(t, cmd)
	})

	t.Run("Wait", func(t *testing.T) {
		mr.ErrImmediate = false
		cmd, err := mr.Start(context.Background(), "unknowncommand")
		require.NoError(t, err)
		require.ErrorIs(t, cmd.Wait(), defaultErr)
	})
}

func TestCommandOutput(t *testing.T) {
	mr := rigtest.NewMockRunner()
	mr.AddCommandOutput(rigtest.Contains("test"), "hello world")
	out, err := mr.ExecOutput("test")
	require.NoError(t, err)
	require.Equal(t, "hello world", out)
}
