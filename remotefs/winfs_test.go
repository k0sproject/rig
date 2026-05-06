package remotefs_test

import (
	"io/fs"
	"testing"

	"github.com/k0sproject/rig/v2/remotefs"
	"github.com/k0sproject/rig/v2/rigtest"
	"github.com/stretchr/testify/require"
)

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
