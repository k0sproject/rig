package remotefs

import (
	"io/fs"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWinFileInfoMode(t *testing.T) {
	t.Run("writable file is not a directory", func(t *testing.T) {
		fi := &winFileInfo{FMode: "-----", IsReadOnly: false}
		require.Equal(t, fs.FileMode(0o777), fi.Mode())
		require.True(t, fi.Mode().IsRegular())
		require.False(t, fi.Mode().IsDir())
	})

	t.Run("read-only file", func(t *testing.T) {
		fi := &winFileInfo{FMode: "-----", IsReadOnly: true}
		require.Equal(t, fs.FileMode(0o555), fi.Mode())
		require.True(t, fi.Mode().IsRegular())
	})

	t.Run("directory sets ModeDir", func(t *testing.T) {
		fi := &winFileInfo{FMode: "d----", IsReadOnly: false}
		require.True(t, fi.Mode().IsDir(), "directory must have ModeDir set")
		require.False(t, fi.Mode().IsRegular(), "directory must not be regular")
		require.Equal(t, fs.ModeDir|0o777, fi.Mode())
	})

	t.Run("read-only directory", func(t *testing.T) {
		fi := &winFileInfo{FMode: "d----", IsReadOnly: true}
		require.True(t, fi.Mode().IsDir())
		require.Equal(t, fs.ModeDir|0o555, fi.Mode())
	})
}
