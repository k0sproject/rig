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

// openPosixFileForWriting mocks the stat probes PosixFS.OpenFile performs and
// returns a PosixFile for an existing, empty, writable /tmp/file.
func openPosixFileForWriting(t *testing.T, mr *rigtest.MockRunner) remotefs.File {
	t.Helper()
	// initStat: selects the GNU stat syntax.
	mr.AddCommandSuccess(rigtest.Equal("stat -c %n /"))
	// fsBlockSize probes the parent directory. Registered before the generic
	// stat handler below so it is not swallowed by it.
	mr.AddCommandOutput(rigtest.Contains(`stat -c "%s"`), "4096")
	// Stat of the file itself: 0x81a4 = 0o100644 (regular file, rw-r--r--).
	mr.AddCommandOutput(rigtest.Contains("-- /tmp/file"), "0x81a4 0 1234567890.000000000 ///tmp/file//")

	f, err := remotefs.NewPosixFS(mr).OpenFile("/tmp/file", os.O_WRONLY, 0o644)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, f.Close()) })
	return f
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
	require.Equal(t, "dd of=/tmp/file bs=4096 seek=0 conv=notrunc", mr.LastCommand())
	require.NoError(t, mr.NotReceived(rigtest.Contains("/dev/stdin")))
}
