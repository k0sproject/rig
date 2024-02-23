package rigtest_test

import (
	"context"
	"errors"
	"testing"

	"github.com/k0sproject/rig/rigtest"
	"github.com/stretchr/testify/require"
)

func TestAddAndProcessMockCommand(t *testing.T) {
	mc := rigtest.NewMockRunner()
	expectedOutput := "mock output"

	mc.AddCommand(rigtest.Equal("test"), func(a *rigtest.A) error {
		a.Stdout.Write([]byte(expectedOutput))
		return nil
	})

	out, err := mc.ExecOutput("test")
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	require.Equal(t, expectedOutput, out)
	require.NoError(t, mc.Received(rigtest.HasPrefix("test")))
}

func TestCommandHistoryAndReset(t *testing.T) {
	mc := rigtest.NewMockRunner()
	require.Zero(t, mc.Len())
	require.NoError(t, mc.Exec("command1"))
	require.Equal(t, 1, mc.Len())
	require.NoError(t, mc.Exec("command2"))
	require.Equal(t, 2, mc.Len())
	require.Contains(t, mc.Commands(), "command1")
	require.Contains(t, mc.Commands(), "command2")
	require.Equal(t, "command2", mc.LastCommand())
	mc.Reset()
	require.Zero(t, mc.Len())
	require.Equal(t, "", mc.LastCommand())
	require.Empty(t, mc.Commands())
}

func TestIsWindows(t *testing.T) {
	mc := rigtest.NewMockRunner()
	require.False(t, mc.IsWindows())
	mc.Windows = true
	require.True(t, mc.IsWindows())
}

func TestDefaultErrors(t *testing.T) {
	mc := rigtest.NewMockRunner()
	defaultErr := errors.New("default error")
	mc.ErrDefault = defaultErr

	t.Run("Immediate", func(t *testing.T) {
		mc.ErrImmediate = true
		cmd, err := mc.Start(context.Background(), "unknowncommand")
		require.ErrorIs(t, err, defaultErr)
		require.Nil(t, cmd)
	})

	t.Run("Wait", func(t *testing.T) {
		mc.ErrImmediate = false
		cmd, err := mc.Start(context.Background(), "unknowncommand")
		require.NoError(t, err)
		require.ErrorIs(t, cmd.Wait(), defaultErr)
	})
}

func TestCommandOutput(t *testing.T) {
	mc := rigtest.NewMockRunner()
	mc.AddCommandOutput(rigtest.Contains("test"), "hello world")
	out, err := mc.ExecOutput("test")
	require.NoError(t, err)
	require.Equal(t, "hello world", out)
}
