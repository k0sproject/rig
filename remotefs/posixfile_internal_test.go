package remotefs

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPosixFileAccessMode(t *testing.T) {
	for _, tc := range []struct {
		name     string
		flags    int
		readable bool
		writable bool
	}{
		{"read", os.O_RDONLY, true, false},
		{"write", os.O_WRONLY, false, true},
		{"read-write", os.O_RDWR, true, true},
		{"create exclusive read-write", os.O_RDWR | os.O_CREATE | os.O_EXCL, true, true},
		{"append write", os.O_WRONLY | os.O_APPEND, false, true},
		{"truncate read", os.O_RDONLY | os.O_TRUNC, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &PosixFile{isOpen: true, flags: tc.flags}
			require.Equal(t, tc.readable, f.isReadable(), "readable")
			require.Equal(t, tc.writable, f.isWritable(), "writable")
		})
	}

	closed := &PosixFile{flags: os.O_RDWR}
	require.False(t, closed.isReadable())
	require.False(t, closed.isWritable())
}
