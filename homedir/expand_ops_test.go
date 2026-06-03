package homedir_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/k0sproject/rig/v2/homedir"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpandFile(t *testing.T) {
	dir := t.TempDir()
	f, err := os.CreateTemp(dir, "testfile")
	require.NoError(t, err)
	require.NoError(t, f.Close())
	filePath := f.Name()

	t.Run("existing file returns path", func(t *testing.T) {
		got, err := homedir.ExpandFile(filePath)
		require.NoError(t, err)
		assert.Equal(t, filePath, got)
	})

	t.Run("directory path returns error", func(t *testing.T) {
		_, err := homedir.ExpandFile(dir)
		require.ErrorIs(t, err, homedir.ErrInvalidPath)
	})

	t.Run("non-existent path returns error", func(t *testing.T) {
		_, err := homedir.ExpandFile(filepath.Join(dir, "does-not-exist"))
		require.Error(t, err)
	})

	t.Run("empty path returns error", func(t *testing.T) {
		_, err := homedir.ExpandFile("")
		require.ErrorIs(t, err, homedir.ErrInvalidPath)
	})
}

func TestExpandDir(t *testing.T) {
	dir := t.TempDir()
	f, err := os.CreateTemp(dir, "testfile")
	require.NoError(t, err)
	require.NoError(t, f.Close())
	filePath := f.Name()

	t.Run("existing directory returns path", func(t *testing.T) {
		got, err := homedir.ExpandDir(dir)
		require.NoError(t, err)
		assert.Equal(t, dir, got)
	})

	t.Run("file path returns error", func(t *testing.T) {
		_, err := homedir.ExpandDir(filePath)
		require.ErrorIs(t, err, homedir.ErrInvalidPath)
	})

	t.Run("non-existent path returns error", func(t *testing.T) {
		_, err := homedir.ExpandDir(filepath.Join(dir, "does-not-exist"))
		require.Error(t, err)
	})

	t.Run("empty path returns error", func(t *testing.T) {
		_, err := homedir.ExpandDir("")
		require.ErrorIs(t, err, homedir.ErrInvalidPath)
	})
}
