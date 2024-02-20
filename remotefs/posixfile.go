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

	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/shellescape"
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

	out, err := f.fs.ExecOutput(`stat -c "%%s" %[1]s 2> /dev/null || stat -f "%%k" %[1]s`, shellescape.Quote(path.Dir(f.path)))
	if err != nil {
		// fall back to default
		f.blockSize = defaultBlockSize
	} else if bs, err := strconv.Atoi(strings.TrimSpace(out)); err == nil {
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

func (f *PosixFile) ddParams(offset int64, numBytes int) (blocksize int, skip int64, count int) { //nolint:nonamedreturns // for readability
	optimalBs := f.fsBlockSize()

	// if numBytes aligns with the optimal block size, use it; otherwise, use bs = 1
	bs := optimalBs
	if numBytes%optimalBs != 0 {
		bs = 1
	}

	s := offset / int64(bs)
	c := (numBytes + bs - 1) / bs
	return bs, s, c
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

	if err := f.fs.Exec("dd if=%s bs=%d skip=%d count=%d", shellescape.Quote(f.path), bs, skip, count, exec.Stdout(buf), exec.HideOutput()); err != nil {
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
			"dd if=/dev/stdin of=%s bs=%d count=%d seek=%d conv=notrunc", f.path, bs, count, skip,
			exec.Stdin(limitedReader),
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
	counter := &ByteCounter{}
	writer := io.MultiWriter(dst, counter)
	err := f.fs.Exec(
		"dd if=%s bs=%d skip=%d count=%d", shellescape.Quote(f.path), bs, skip, count,
		exec.Stdout(writer),
		exec.HideOutput(),
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
	counter := &ByteCounter{}

	err := f.fs.Exec(
		"dd if=/dev/stdin of=%s bs=%d seek=%d conv=notrunc", shellescape.Quote(f.path), f.fsBlockSize(), f.pos,
		exec.Stdin(io.TeeReader(src, counter)),
	)
	if err != nil {
		return 0, f.pathErr(OpCopyFrom, fmt.Errorf("exec dd: %w", err))
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
