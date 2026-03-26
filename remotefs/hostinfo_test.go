package remotefs_test

import (
	"errors"
	"testing"
	"time"

	"github.com/k0sproject/rig/v2/remotefs"
	"github.com/k0sproject/rig/v2/rigtest"
	"github.com/stretchr/testify/require"
)

func TestPosixMachineID(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandOutput(rigtest.Equal("cat /etc/machine-id"), "abc123def456")
		fs := remotefs.NewPosixFS(mr)
		id, err := fs.MachineID()
		require.NoError(t, err)
		require.Equal(t, "abc123def456", id)
	})

	t.Run("empty", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandOutput(rigtest.Equal("cat /etc/machine-id"), "")
		fs := remotefs.NewPosixFS(mr)
		_, err := fs.MachineID()
		require.Error(t, err)
	})

	t.Run("command fails", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandFailure(rigtest.Equal("cat /etc/machine-id"), errors.New("no such file"))
		fs := remotefs.NewPosixFS(mr)
		_, err := fs.MachineID()
		require.Error(t, err)
	})
}

func TestPosixSystemTime(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandOutput(rigtest.Equal("date -u +%s"), "1700000000")
		fs := remotefs.NewPosixFS(mr)
		got, err := fs.SystemTime()
		require.NoError(t, err)
		require.Equal(t, time.Unix(1700000000, 0), got)
	})

	t.Run("invalid output", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandOutput(rigtest.Equal("date -u +%s"), "not-a-number")
		fs := remotefs.NewPosixFS(mr)
		_, err := fs.SystemTime()
		require.Error(t, err)
	})
}

func TestWindowsMachineID(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.Windows = true
		mr.AddCommandOutput(rigtest.HasPrefix("powershell.exe"), "6ba7b810-9dad-11d1-80b4-00c04fd430c8")
		fs := remotefs.NewWindowsFS(mr)
		id, err := fs.MachineID()
		require.NoError(t, err)
		require.Equal(t, "6ba7b810-9dad-11d1-80b4-00c04fd430c8", id)
	})

	t.Run("empty", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.Windows = true
		mr.AddCommandOutput(rigtest.HasPrefix("powershell.exe"), "")
		fs := remotefs.NewWindowsFS(mr)
		_, err := fs.MachineID()
		require.Error(t, err)
	})
}

func TestWindowsSystemTime(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.Windows = true
		mr.AddCommandOutput(rigtest.HasPrefix("powershell.exe"), "1700000000")
		fs := remotefs.NewWindowsFS(mr)
		got, err := fs.SystemTime()
		require.NoError(t, err)
		require.Equal(t, time.Unix(1700000000, 0), got)
	})

	t.Run("invalid output", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.Windows = true
		mr.AddCommandOutput(rigtest.HasPrefix("powershell.exe"), "not-a-number")
		fs := remotefs.NewWindowsFS(mr)
		_, err := fs.SystemTime()
		require.Error(t, err)
	})
}
