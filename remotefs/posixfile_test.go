package remotefs_test

import (
	"fmt"
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
// remotefs.Upload, which appends to a file cut back to the resume point.
func TestPosixFileCopyFrom(t *testing.T) {
	mr := rigtest.NewMockRunner()
	var got []byte
	mr.AddCommandSuccess(rigtest.HasPrefix("truncate"))
	mr.AddCommand(rigtest.HasPrefix("cat"), func(a *rigtest.A) error {
		var err error
		got, err = io.ReadAll(a.Stdin)
		return err
	})
	f := openPosixFileForWriting(t, mr)

	n, err := f.CopyFrom(strings.NewReader("hello"))
	require.NoError(t, err)
	require.Equal(t, int64(5), n)
	require.Equal(t, "hello", string(got))
	require.Equal(t, "cat >>/tmp/file", mr.LastCommand())
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
			// 16384 bytes is a whole number of blocks both at the reported 8192
			// and at the 4096 byte default, so the block size dd is handed is the
			// one fsBlockSize settled on rather than a reduction of it.
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

// TestPosixFileCopyFromResume covers a copy that resumes at a nonzero offset.
// The remote file is cut back to that offset and the stream appended to it, so
// the transfer never has to express the offset in blocks: dd's seek did, which
// left a resume from an odd byte offset copying one byte at a time.
func TestPosixFileCopyFromResume(t *testing.T) {
	const mib = 1 << 20
	for _, tc := range []struct {
		name string
		pos  int64
	}{
		{"start", 0},
		{"whole blocks", 3 * mib},
		{"less than a block", 4096},
		{"unaligned", 1_500_000},
		{"odd", mib + 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mr := rigtest.NewMockRunner()
			mr.AddCommandSuccess(rigtest.HasPrefix("truncate"))
			var got []byte
			mr.AddCommand(rigtest.HasPrefix("cat"), func(a *rigtest.A) error {
				var err error
				got, err = io.ReadAll(a.Stdin)
				return err
			})
			f := openPosixFileForWriting(t, mr)

			pos, err := f.Seek(tc.pos, io.SeekStart)
			require.NoError(t, err)
			require.Equal(t, tc.pos, pos)

			n, err := f.CopyFrom(strings.NewReader("hello"))
			require.NoError(t, err)
			require.Equal(t, int64(5), n)
			require.Equal(t, "hello", string(got))

			// Everything already on the remote side is kept, so the file is cut
			// back to the resume point rather than emptied.
			require.NoError(t, mr.Received(rigtest.Equal(
				fmt.Sprintf("truncate -s %d /tmp/file", tc.pos))))

			// The same command at every offset: the append starts where the
			// truncate stopped, whatever that offset happens to divide by.
			require.Equal(t, "cat >>/tmp/file", mr.LastCommand())
		})
	}
}

// TestPosixFileReadAtOffset covers reads that do not start on a block boundary.
// dd counts skip in blocks, so a block size that divides the length but not the
// offset silently rounds the offset down: at bs=4096, seeking to 2048 and asking
// for 8192 bytes was handed skip=0 and returned the first 8 KiB of the file.
func TestPosixFileReadAtOffset(t *testing.T) {
	for _, tc := range []struct {
		name   string
		offset int64
		length int
		bs     int64
	}{
		{"whole blocks", 8192, 8192, 4096},
		{"half a block in", 2048, 8192, 2048},
		{"odd", 4097, 8192, 1},
		{"partial block", 4096, 100, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mr := rigtest.NewMockRunner()
			mr.AddCommand(rigtest.HasPrefix("dd "), func(a *rigtest.A) error {
				_, err := a.Stdout.Write(make([]byte, tc.length))
				return err
			})
			// A megabyte of file, so every offset under test is still short of the
			// end and Read is not cut off by EOF.
			f := openPosixFile(t, mr, os.O_RDONLY, "4096", "1048576")

			pos, err := f.Seek(tc.offset, io.SeekStart)
			require.NoError(t, err)
			require.Equal(t, tc.offset, pos)

			n, err := f.Read(make([]byte, tc.length))
			require.NoError(t, err)
			require.Equal(t, tc.length, n)

			bs, skip, count := parseDDRead(t, mr.LastCommand())
			require.Equal(t, tc.bs, bs)
			require.Equal(t, tc.offset, bs*skip,
				"bs=%d skip=%d reads from byte %d, not %d", bs, skip, bs*skip, tc.offset)
			require.Equal(t, int64(tc.length), bs*count,
				"bs=%d count=%d reads %d bytes, not %d", bs, count, bs*count, tc.length)
		})
	}
}

// TestPosixFileCopyToAtOffset is the same for the streaming read path, where the
// length comes from what is left of the file rather than from a caller's buffer.
func TestPosixFileCopyToAtOffset(t *testing.T) {
	mr := rigtest.NewMockRunner()
	mr.AddCommandSuccess(rigtest.HasPrefix("dd "))
	f := openPosixFile(t, mr, os.O_RDONLY, "4096", "10240")

	_, err := f.Seek(2048, io.SeekStart)
	require.NoError(t, err)

	_, err = f.CopyTo(io.Discard)
	require.NoError(t, err)

	// The remaining 8192 bytes are a whole number of 4096 byte blocks, which is
	// what used to make dd skip to block 0 and copy the file from the start.
	bs, skip, count := parseDDRead(t, mr.LastCommand())
	require.Equal(t, int64(2048), bs*skip)
	require.Equal(t, int64(8192), bs*count)
}

func parseDDRead(t *testing.T, command string) (bs, skip, count int64) { //nolint:nonamedreturns // for readability
	t.Helper()
	_, err := fmt.Sscanf(command, "dd if=/tmp/file bs=%d skip=%d count=%d", &bs, &skip, &count)
	require.NoError(t, err, "unexpected dd invocation: %s", command)
	return bs, skip, count
}
