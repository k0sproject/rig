package remotefs_test

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/k0sproject/rig/v2/remotefs"
	"github.com/k0sproject/rig/v2/rigtest"
	"github.com/stretchr/testify/require"
)

// openPosixFile mocks the stat probes PosixFS.OpenFile performs and returns a
// PosixFile for an existing /tmp/file of the given size, whose parent directory
// reports blockSize as its optimal I/O block size.
func openPosixFile(t *testing.T, mr *rigtest.MockRunner, flags int, blockSize, size string) remotefs.File {
	t.Helper()
	// initStat: selects the GNU stat syntax.
	mr.AddCommandSuccess(rigtest.Equal("stat -c %n /"))
	// fsBlockSize probes the parent directory. Registered before the generic
	// stat handler below so it is not swallowed by it.
	mr.AddCommandOutput(rigtest.Contains(`stat -c "%o"`), blockSize)
	// Stat of the file itself: 0x81a4 = 0o100644 (regular file, rw-r--r--).
	mr.AddCommandOutput(rigtest.Contains("-- /tmp/file"), "0x81a4 "+size+" 1234567890.000000000 ///tmp/file//")

	f, err := remotefs.NewPosixFS(mr).OpenFile("/tmp/file", flags, 0o644)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, f.Close()) })
	return f
}

// openPosixFileForWriting returns an empty, writable /tmp/file on a file system
// reporting the usual 4096 byte block size.
func openPosixFileForWriting(t *testing.T, mr *rigtest.MockRunner) remotefs.File {
	t.Helper()
	return openPosixFile(t, mr, os.O_WRONLY, "4096", "0")
}

// TestPosixFileWrite verifies that writes are piped into dd through stdin. dd
// must not be told to read /dev/stdin explicitly — see PosixFS.WriteFile for the
// coreutils implementations that reject that.
func TestPosixFileWrite(t *testing.T) {
	mr := rigtest.NewMockRunner()
	var got []byte
	mr.AddCommand(rigtest.HasPrefix("dd "), func(a *rigtest.A) error {
		var err error
		got, err = io.ReadAll(a.Stdin)
		return err
	})
	f := openPosixFileForWriting(t, mr)

	n, err := f.Write([]byte("hello"))
	require.NoError(t, err)
	require.Equal(t, 5, n)
	require.Equal(t, "hello", string(got))
	require.Equal(t, "dd of=/tmp/file bs=1 count=5 seek=0 conv=notrunc", mr.LastCommand())
	require.NoError(t, mr.NotReceived(rigtest.Contains("/dev/stdin")))
}

// TestPosixFileCopyFrom verifies the same for the streaming copy path used by
// remotefs.Upload.
func TestPosixFileCopyFrom(t *testing.T) {
	mr := rigtest.NewMockRunner()
	var got []byte
	mr.AddCommandSuccess(rigtest.HasPrefix("truncate"))
	mr.AddCommand(rigtest.HasPrefix("dd "), func(a *rigtest.A) error {
		var err error
		got, err = io.ReadAll(a.Stdin)
		return err
	})
	f := openPosixFileForWriting(t, mr)

	n, err := f.CopyFrom(strings.NewReader("hello"))
	require.NoError(t, err)
	require.Equal(t, int64(5), n)
	require.Equal(t, "hello", string(got))
	require.Equal(t, "dd of=/tmp/file bs=1048576 seek=0 conv=notrunc", mr.LastCommand())
	require.NoError(t, mr.NotReceived(rigtest.Contains("/dev/stdin")))
}

// TestPosixFileBlockSize pins down what fsBlockSize asks stat for and what it
// accepts back. It used to ask for %s, the size of the parent directory's own
// data, which equals the block size on ext4 and so looked right there. XFS keeps
// a small directory inline in the inode: a directory holding one file reported
// 29 bytes, and dd was then handed bs=29.
func TestPosixFileBlockSize(t *testing.T) {
	for _, tc := range []struct {
		name      string
		blockSize string
	}{
		{"reported", "8192"},
		{"implausible", "29"}, // an XFS directory holding a single short-named file
		{"unparseable", "?"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mr := rigtest.NewMockRunner()
			mr.AddCommandSuccess(rigtest.HasPrefix("dd "))
			// 16384 bytes is a whole number of blocks at any of the sizes under
			// test, so a rejected one shows up as ddParams falling back to bs=1.
			f := openPosixFile(t, mr, os.O_RDONLY, tc.blockSize, "16384")

			_, err := f.CopyTo(io.Discard)
			require.NoError(t, err)
			require.NoError(t, mr.Received(rigtest.Contains(`stat -c "%o"`)),
				"the optimal I/O block size is %o; %s is the directory's own size")

			want := "dd if=/tmp/file bs=8192 skip=0 count=2"
			if tc.blockSize != "8192" {
				// Anything implausible or unreadable falls back to the default.
				want = "dd if=/tmp/file bs=4096 skip=0 count=4"
			}
			require.Equal(t, want, mr.LastCommand())
		})
	}
}
