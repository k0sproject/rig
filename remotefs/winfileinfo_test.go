package remotefs

import (
	"encoding/json"
	"io/fs"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWinFileInfoModeSymlink(t *testing.T) {
	// Get-Item output as Stat parses it, for a file symlink, a directory
	// symlink, a junction and a plain file. LinkType is null for the latter.
	cases := []struct {
		name        string
		json        string
		wantSymlink bool
		wantDir     bool
	}{
		{"file symlink", `{"Name":"link.txt","Mode":"-a---l","Attributes":1056,"LinkType":"SymbolicLink","Length":0}`, true, false},
		{"directory symlink", `{"Name":"linkdir","Mode":"d----l","Attributes":1040,"LinkType":"SymbolicLink","Length":null}`, true, true},
		{"junction", `{"Name":"junction","Mode":"d----l","Attributes":1040,"LinkType":"Junction","Length":null}`, false, true},
		{"plain file", `{"Name":"file.txt","Mode":"-a----","Attributes":32,"LinkType":null,"Length":5}`, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fi := &winFileInfo{}
			require.NoError(t, json.Unmarshal([]byte(tc.json), fi))
			mode := fi.Mode()
			require.Equal(t, tc.wantSymlink, mode&fs.ModeSymlink != 0, "ModeSymlink")
			require.Equal(t, tc.wantDir, mode.IsDir(), "ModeDir")
			require.Equal(t, !tc.wantSymlink && !tc.wantDir, mode.IsRegular(), "IsRegular")
		})
	}
}

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
