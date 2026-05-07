package sudo_test

import (
	"errors"
	"testing"

	"github.com/k0sproject/rig/v2/rigtest"
	"github.com/k0sproject/rig/v2/sudo"
	"github.com/stretchr/testify/require"
)

func TestRegisterWindowsNoop(t *testing.T) {
	t.Run("elevated session registers noop runner", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.Windows = true
		mr.AddCommandSuccess(rigtest.HasPrefix("powershell.exe"))

		registry := sudo.NewRegistry()
		sudo.RegisterWindowsNoop(registry)

		runner, err := registry.Get(mr)
		require.NoError(t, err)
		require.NotNil(t, runner)
	})

	t.Run("non-elevated session skips registration", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.Windows = true
		mr.AddCommandFailure(rigtest.HasPrefix("powershell.exe"), errors.New("access denied"))

		registry := sudo.NewRegistry()
		sudo.RegisterWindowsNoop(registry)

		_, err := registry.Get(mr)
		require.ErrorIs(t, err, sudo.ErrNoSudo)
	})

	t.Run("non-windows host skips registration", func(t *testing.T) {
		mr := rigtest.NewMockRunner()

		registry := sudo.NewRegistry()
		sudo.RegisterWindowsNoop(registry)

		_, err := registry.Get(mr)
		require.ErrorIs(t, err, sudo.ErrNoSudo)
	})
}
