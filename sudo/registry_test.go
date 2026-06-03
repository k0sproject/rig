package sudo_test

import (
	"errors"
	"testing"

	"github.com/k0sproject/rig/v2/rigtest"
	"github.com/k0sproject/rig/v2/sudo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errProbe = errors.New("probe failed")

// TestNoopDecorator verifies that Noop returns the command unchanged.
func TestNoopDecorator(t *testing.T) {
	assert.Equal(t, "whoami", sudo.Noop("whoami"))
	assert.Equal(t, "cat /etc/passwd", sudo.Noop("cat /etc/passwd"))
}

// TestRegisterSudo verifies the RegisterSudo detection logic.
func TestRegisterSudo(t *testing.T) {
	t.Run("detected when sudo probe succeeds", func(t *testing.T) {
		reg := sudo.NewRegistry()
		sudo.RegisterSudo(reg)

		mr := rigtest.NewMockRunner()
		mr.ErrDefault = errProbe
		mr.AddCommandSuccess(rigtest.Contains("sudo -n"))

		runner, err := reg.Get(mr)
		require.NoError(t, err)
		require.NotNil(t, runner)
		require.NoError(t, mr.Received(rigtest.Contains("sudo -n")))
	})

	t.Run("not detected when sudo probe fails", func(t *testing.T) {
		reg := sudo.NewRegistry()
		sudo.RegisterSudo(reg)

		mr := rigtest.NewMockRunner()
		mr.ErrDefault = errProbe

		_, err := reg.Get(mr)
		require.ErrorIs(t, err, sudo.ErrNoSudo)
	})

	t.Run("skipped on Windows", func(t *testing.T) {
		reg := sudo.NewRegistry()
		sudo.RegisterSudo(reg)

		mr := rigtest.NewMockRunner()
		mr.Windows = true
		mr.AddCommandSuccess(rigtest.Contains("sudo"))

		_, err := reg.Get(mr)
		require.ErrorIs(t, err, sudo.ErrNoSudo)
		require.NoError(t, mr.NotReceived(rigtest.Contains("sudo")))
	})

	t.Run("returned runner wraps commands with sudo", func(t *testing.T) {
		reg := sudo.NewRegistry()
		sudo.RegisterSudo(reg)

		mr := rigtest.NewMockRunner()
		mr.AddCommandSuccess(rigtest.Contains("sudo"))

		runner, err := reg.Get(mr)
		require.NoError(t, err)

		require.NoError(t, runner.Exec("whoami"))
		require.NoError(t, mr.Received(rigtest.Contains("sudo -n")))
		require.NoError(t, mr.Received(rigtest.Contains("whoami")))
	})
}

// TestRegisterDoas verifies the RegisterDoas detection logic.
func TestRegisterDoas(t *testing.T) {
	t.Run("detected when doas probe succeeds", func(t *testing.T) {
		reg := sudo.NewRegistry()
		sudo.RegisterDoas(reg)

		mr := rigtest.NewMockRunner()
		mr.ErrDefault = errProbe
		mr.AddCommandSuccess(rigtest.Contains("doas -n"))

		runner, err := reg.Get(mr)
		require.NoError(t, err)
		require.NotNil(t, runner)
		require.NoError(t, mr.Received(rigtest.Contains("doas -n")))
	})

	t.Run("not detected when doas probe fails", func(t *testing.T) {
		reg := sudo.NewRegistry()
		sudo.RegisterDoas(reg)

		mr := rigtest.NewMockRunner()
		mr.ErrDefault = errProbe

		_, err := reg.Get(mr)
		require.ErrorIs(t, err, sudo.ErrNoSudo)
	})

	t.Run("skipped on Windows", func(t *testing.T) {
		reg := sudo.NewRegistry()
		sudo.RegisterDoas(reg)

		mr := rigtest.NewMockRunner()
		mr.Windows = true
		mr.AddCommandSuccess(rigtest.Contains("doas"))

		_, err := reg.Get(mr)
		require.ErrorIs(t, err, sudo.ErrNoSudo)
		require.NoError(t, mr.NotReceived(rigtest.Contains("doas")))
	})

	t.Run("returned runner wraps commands with doas", func(t *testing.T) {
		reg := sudo.NewRegistry()
		sudo.RegisterDoas(reg)

		mr := rigtest.NewMockRunner()
		mr.AddCommandSuccess(rigtest.Contains("doas"))

		runner, err := reg.Get(mr)
		require.NoError(t, err)

		require.NoError(t, runner.Exec("whoami"))
		require.NoError(t, mr.Received(rigtest.Contains("doas -n")))
		require.NoError(t, mr.Received(rigtest.Contains("whoami")))
	})
}

// TestRegisterUID0Noop verifies the RegisterUID0Noop detection logic.
func TestRegisterUID0Noop(t *testing.T) {
	t.Run("detected when running as root", func(t *testing.T) {
		reg := sudo.NewRegistry()
		sudo.RegisterUID0Noop(reg)

		mr := rigtest.NewMockRunner()
		mr.ErrDefault = errProbe
		mr.AddCommandSuccess(rigtest.Contains("id -u"))

		runner, err := reg.Get(mr)
		require.NoError(t, err)
		require.NotNil(t, runner)
		require.NoError(t, mr.Received(rigtest.Contains("id -u")))
	})

	t.Run("not detected when not root", func(t *testing.T) {
		reg := sudo.NewRegistry()
		sudo.RegisterUID0Noop(reg)

		mr := rigtest.NewMockRunner()
		mr.ErrDefault = errProbe

		_, err := reg.Get(mr)
		require.ErrorIs(t, err, sudo.ErrNoSudo)
	})

	t.Run("skipped on Windows", func(t *testing.T) {
		reg := sudo.NewRegistry()
		sudo.RegisterUID0Noop(reg)

		mr := rigtest.NewMockRunner()
		mr.Windows = true

		_, err := reg.Get(mr)
		require.ErrorIs(t, err, sudo.ErrNoSudo)
		require.NoError(t, mr.NotReceived(rigtest.Contains("id -u")))
	})

	t.Run("returned runner passes commands through unmodified", func(t *testing.T) {
		reg := sudo.NewRegistry()
		sudo.RegisterUID0Noop(reg)

		mr := rigtest.NewMockRunner()
		mr.AddCommandSuccess(rigtest.Contains("id -u"))
		mr.AddCommandSuccess(rigtest.Equal("whoami"))

		runner, err := reg.Get(mr)
		require.NoError(t, err)

		require.NoError(t, runner.Exec("whoami"))
		require.NoError(t, mr.Received(rigtest.Equal("whoami")))
	})
}

// TestSudoProvider verifies NewSudoProvider and SudoRunner.
func TestSudoProvider(t *testing.T) {
	t.Run("SudoRunner returns decorated runner on success", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandSuccess(rigtest.Contains("sudo"))

		reg := sudo.NewRegistry()
		sudo.RegisterSudo(reg)

		provider := sudo.NewSudoProvider(reg.Get, mr)
		runner, err := provider.SudoRunner()
		require.NoError(t, err)
		require.NotNil(t, runner)
	})

	t.Run("SudoRunner returns error when no method found", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.ErrDefault = errProbe

		reg := sudo.NewRegistry()
		sudo.RegisterSudo(reg)

		provider := sudo.NewSudoProvider(reg.Get, mr)
		_, err := provider.SudoRunner()
		require.ErrorIs(t, err, sudo.ErrNoSudo)
	})

	t.Run("SudoRunner is memoised", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandSuccess(rigtest.Contains("sudo"))

		reg := sudo.NewRegistry()
		sudo.RegisterSudo(reg)

		provider := sudo.NewSudoProvider(reg.Get, mr)
		r1, err1 := provider.SudoRunner()
		require.NoError(t, err1)
		r2, err2 := provider.SudoRunner()
		require.NoError(t, err2)
		// Same pointer returned both times.
		assert.Same(t, r1, r2)
	})
}
