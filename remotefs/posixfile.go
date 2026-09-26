package remotefs

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/k0sproject/rig/v2/cmd"
	"github.com/k0sproject/rig/v2/iostream"
	"github.com/k0sproject/rig/v2/sh"
	"github.com/k0sproject/rig/v2/sh/shellescape"
)

var (
	_ fs.File = (*PosixFile)(nil)
	_ File    = (*PosixFile)(nil)
)

// PosixFile implements fs.File for a remote file.
type PosixFile struct {
	withPath
	fs     *PosixFS
	isOpen bool
	isEOF  bool
	pos    int64
	size   int64
	mode   fs.FileMode
	flags  int

	blockSize int
}

func (f *PosixFile) fsBlockSize() int {
	if f.blockSize > 0 {
		return f.blockSize
	}

	f.blockSize = defaultBlockSize

	// %o is the optimal I/O block size, the GNU spelling of BSD's %k. Not %s,
	// which is the size of the directory's own data: that equals the block size
	// on ext4, but XFS keeps small directories inline in the inode, where it is
	// a few dozen bytes.
	out, err := f.fs.ExecOutput(fmt.Sprintf(`stat -c "%%o" %[1]s 2> /dev/null || stat -f "%%k" %[1]s`, shellescape.Quote(path.Dir(f.path))))
	if err != nil {
		return f.blockSize
	}

	// Anything outside this range is stat reporting something that is not a
	// block size, and the default is safer than passing it on to dd.
	if bs, err := strconv.Atoi(strings.TrimSpace(out)); err == nil &&
		bs >= minBlockSize && bs <= maxBlockSize && bs&(bs-1) == 0 {
		f.blockSize = bs
	}

	return f.blockSize
}

func (f *PosixFile) isReadable() bool {
	return f.isOpen && (f.flags&os.O_WRONLY != os.O_WRONLY || f.flags&os.O_RDWR == os.O_RDWR)
}

func (f *PosixFile) isWritable() bool {
	return f.isOpen && f.flags&os.O_WRONLY != 0
}

// alignBlockSize halves bs until it divides each of counts evenly, so that a
// byte count can be handed to dd as a number of blocks without losing anything.
// bs is a power of two, so the reduction bottoms out at 1 rather than looping.
func alignBlockSize(bs int64, counts ...int64) int64 {
	for _, c := range counts {
		for c%bs != 0 {
			bs /= 2
		}
	}

	return bs
}

func (f *PosixFile) ddParams(offset int64, numBytes int) (blocksize int, skip int64, count int) { //nolint:nonamedreturns // for readability
	// dd counts skip in blocks rather than in bytes, so the block size has to
	// divide the offset as well as the length: at bs=4096, an offset of 2048
	// would otherwise round down to skip=0 and read from the wrong place.
	// Callers also rely on blocksize*count being exactly numBytes.
	bs := alignBlockSize(int64(f.fsBlockSize()), offset, int64(numBytes))

	return int(bs), offset / bs, int(int64(numBytes) / bs)
}

// Stat returns a FileInfo describing the named file.
func (f *PosixFile) Stat() (fs.FileInfo, error) {
	return f.fs.Stat(f.path)
}

// Read reads up to len(p) bytes into p. It returns the number of bytes read (0 <= n <= len(p)) and any error encountered.
func (f *PosixFile) Read(p []byte) (int, error) {
	if f.isEOF {
		return 0, io.EOF
	}
	if !f.isReadable() {
		return 0, fmt.Errorf("%w: file %s is not open for reading", fs.ErrClosed, f.path)
	}

	buf := bytes.NewBuffer(nil)

	bs, skip, count := f.ddParams(f.pos, len(p))

	if err := f.fs.Exec(sh.Command("dd", "if="+f.path, fmt.Sprintf("bs=%d", bs), fmt.Sprintf("skip=%d", skip), fmt.Sprintf("count=%d", count)), cmd.Stdout(buf), cmd.HideOutput()); err != nil {
		return 0, fmt.Errorf("failed to execute dd: %w", err)
	}

	readBytes := buf.Bytes()

	// Trim extra data if readBytes is larger than the requested size
	if len(readBytes) > len(p) {
		readBytes = readBytes[:len(p)]
	}

	copied := copy(p, readBytes)
	f.pos += int64(copied)

	if copied < len(p) {
		f.isEOF = true
	}
	return copied, nil
}

func (f *PosixFile) Write(p []byte) (int, error) {
	if !f.isWritable() {
		return 0, fmt.Errorf("%w: file %s is not open for writing", fs.ErrClosed, f.path)
	}

	var written int
	remaining := p
	for written < len(p) {
		bs, skip, count := f.ddParams(f.pos, len(remaining))
		toWrite := bs * count

		limitedReader := bytes.NewReader(remaining[:toWrite])

		err := f.fs.Exec(
			// "if=" is omitted because dd reads stdin by default. Naming
			// /dev/stdin adds nothing and only invites the kind of breakage
			// documented in PosixFS.WriteFile.
			sh.Command("dd", "of="+f.path, fmt.Sprintf("bs=%d", bs), fmt.Sprintf("count=%d", count), fmt.Sprintf("seek=%d", skip), "conv=notrunc"),
			cmd.Stdin(limitedReader),
		)
		if err != nil {
			return 0, fmt.Errorf("write (dd): %w", err)
		}

		written += toWrite
		remaining = remaining[toWrite:]
		f.pos += int64(toWrite)
		if f.pos > f.size {
			f.size = f.pos
		}
	}

	if written < len(p) {
		return written, io.ErrShortWrite
	}

	return written, nil
}

// CopyTo copies the remote file to the writer dst.
func (f *PosixFile) CopyTo(dst io.Writer) (int64, error) {
	if f.isEOF {
		return 0, io.EOF
	}
	if !f.isReadable() {
		return 0, f.pathErr(OpCopyTo, fmt.Errorf("%w: file %s is not open for reading", fs.ErrClosed, f.path))
	}
	bs, skip, count := f.ddParams(f.pos, int(f.size-f.pos))
	counter := &iostream.ByteCounter{}
	writer := io.MultiWriter(dst, counter)
	err := f.fs.Exec(
		sh.Command("dd", "if="+f.path, fmt.Sprintf("bs=%d", bs), fmt.Sprintf("skip=%d", skip), fmt.Sprintf("count=%d", count)),
		cmd.Stdout(writer),
		cmd.HideOutput(),
	)
	if err != nil {
		return 0, f.pathErr(OpCopyTo, fmt.Errorf("failed to execute dd: %w", err))
	}

	f.pos += counter.Count()
	f.isEOF = true
	return counter.Count(), nil
}

// CopyFrom copies the local reader src to the remote file.
func (f *PosixFile) CopyFrom(src io.Reader) (int64, error) {
	if !f.isWritable() {
		return 0, f.pathErr(OpCopyFrom, fmt.Errorf("%w: file %s is not open for writing", fs.ErrClosed, f.path))
	}
	if err := f.fs.Truncate(f.Name(), f.pos); err != nil {
		return 0, f.pathErr(OpCopyFrom, fmt.Errorf("truncate: %w", err))
	}
	counter := &iostream.ByteCounter{}

	// The file has just been cut back to f.pos, so an append lands exactly at the
	// resume point. dd would want that offset in output blocks instead, which
	// means a block size dividing it: a resume from an odd byte offset would be
	// copied one byte at a time. An append needs no block size at all.
	err := f.fs.Exec(
		sh.CommandBuilder("cat").AppendOutToFile(f.path).String(),
		cmd.Stdin(io.TeeReader(src, counter)),
	)
	if err != nil {
		return 0, f.pathErr(OpCopyFrom, fmt.Errorf("exec cat: %w", err))
	}

	f.pos += counter.Count()
	f.size = f.pos
	return counter.Count(), nil
}

// Close closes the file, rendering it unusable for I/O. It returns an error, if any.
func (f *PosixFile) Close() error {
	f.isOpen = false
	return nil
}

// Seek sets the offset for the next Read or Write to offset, interpreted according to whence:
// io.SeekStart means relative to the origin of the file,
// io.SeekCurrent means relative to the current offset, and
// io.SeekEnd means relative to the end.
// Seek returns the new offset relative to the start of the file and an error, if any.
func (f *PosixFile) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		f.pos = offset
	case io.SeekCurrent:
		f.pos += offset
	case io.SeekEnd:
		f.pos = f.size + offset
	default:
		return 0, fmt.Errorf("%w: whence: %d", errInvalid, whence)
	}
	f.isEOF = f.pos >= f.size

	return f.pos, nil
}
