package remotefs_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"testing"

	"github.com/k0sproject/rig/v2/remotefs"
	"github.com/k0sproject/rig/v2/rigtest"
	"github.com/stretchr/testify/require"
)

func TestWinFSFollow(t *testing.T) {
	const path = `C:\logs\app.log`

	t.Run("output flows to writer", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.Windows = true
		mr.AddCommand(rigtest.Contains("powershell.exe"), func(a *rigtest.A) error {
			_, _ = a.Stdout.Write([]byte("new line\n"))
			return nil
		})
		fsys := remotefs.NewWindowsFS(mr)
		var buf bytes.Buffer
		require.NoError(t, fsys.Follow(context.Background(), path, &buf))
		require.Equal(t, "new line\n", buf.String())
	})

	t.Run("context cancellation returns nil", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.Windows = true
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		mr.AddCommand(rigtest.Contains("powershell.exe"), func(a *rigtest.A) error {
			return a.Ctx.Err()
		})
		fsys := remotefs.NewWindowsFS(mr)
		require.NoError(t, fsys.Follow(ctx, path, io.Discard), "context cancellation should not return an error")
	})

	t.Run("command error propagates", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.Windows = true
		mr.AddCommandFailure(rigtest.Contains("powershell.exe"), errors.New("access denied"))
		fsys := remotefs.NewWindowsFS(mr)
		require.Error(t, fsys.Follow(context.Background(), path, io.Discard))
	})
}

func TestWinFSChmod(t *testing.T) {
	t.Run("writable mode clears read-only attribute", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.Windows = true
		mr.AddCommandSuccess(rigtest.Contains("attrib"))
		fsys := remotefs.NewWindowsFS(mr)
		// 0o644 has the owner-write bit (0o200) set → should clear read-only (attrib -R).
		require.NoError(t, fsys.Chmod(`C:\file.txt`, 0o644))
		require.NoError(t, mr.Received(rigtest.Contains("attrib -R")))
		require.NoError(t, mr.NotReceived(rigtest.Contains("attrib +R")))
	})

	t.Run("read-only mode sets read-only attribute", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.Windows = true
		mr.AddCommandSuccess(rigtest.Contains("attrib"))
		fsys := remotefs.NewWindowsFS(mr)
		// 0o444 has no owner-write bit → should set read-only (attrib +R).
		require.NoError(t, fsys.Chmod(`C:\file.txt`, fs.FileMode(0o444)))
		require.NoError(t, mr.Received(rigtest.Contains("attrib +R")))
		require.NoError(t, mr.NotReceived(rigtest.Contains("attrib -R")))
	})
}
