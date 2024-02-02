package rigfs

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/alessio/shellescape"
	"github.com/k0sproject/rig/exec"
)

var (
	_          fs.File        = (*PosixFile)(nil)
	_          File           = (*PosixFile)(nil)
	_          fs.ReadDirFile = (*PosixDir)(nil)
	_          fs.FS          = (*PosixFsys)(nil)
	_          Fsys           = (*PosixFsys)(nil)
	errInvalid                = errors.New("invalid")
)

// PosixFsys implements fs.FS for a remote filesystem that uses POSIX commands for access
type PosixFsys struct {
	exec.SimpleRunner

	// TODO: these should probably be in some kind of "coreutils" package
	statCmd   *string
	chtimesFn func(name string, atime, mtime int64) error
}

// NewPosixFsys returns a fs.FS implementation for a remote filesystem that uses POSIX commands for access
func NewPosixFsys(conn exec.SimpleRunner) *PosixFsys {
	return &PosixFsys{conn, nil, nil}
}

const (
	defaultBlockSize = 4096
	supportedFlags   = os.O_RDONLY | os.O_WRONLY | os.O_RDWR | os.O_CREATE | os.O_EXCL | os.O_TRUNC | os.O_APPEND | os.O_SYNC
)

// PosixFile implements fs.File for a remote file
type PosixFile struct {
	withPath
	fsys   *PosixFsys
	isOpen bool
	isEOF  bool
	pos    int64
	size   int64
	mode   fs.FileMode
	flags  int

	blockSize int
}

// PosixDir implements fs.ReadDirFile for a remote directory
type PosixDir struct {
	PosixFile
	buffer *dirEntryBuffer
}

// ReadDir returns a list of directory entries
func (f *PosixDir) ReadDir(n int) ([]fs.DirEntry, error) {
	if f.buffer == nil {
		entries, err := f.fsys.ReadDir(f.path)
		if err != nil {
			return nil, err
		}
		f.buffer = newDirEntryBuffer(entries)
	}
	return f.buffer.Next(n)
}

func (f *PosixFile) fsBlockSize() int {
	if f.blockSize > 0 {
		return f.blockSize
	}

	out, err := f.fsys.ExecOutput(`stat -c "%%s" %[1]s 2> /dev/null || stat -f "%%k" %[1]s`, shellescape.Quote(path.Dir(f.path)))
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

// Stat returns a FileInfo describing the named file
func (f *PosixFile) Stat() (fs.FileInfo, error) {
	return f.fsys.Stat(f.path)
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

	if err := f.fsys.Exec("dd if=%s bs=%d skip=%d count=%d", shellescape.Quote(f.path), bs, skip, count, exec.Stdout(buf), exec.HideOutput()); err != nil {
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

		err := f.fsys.Exec(
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

// CopyTo copies the remote file to the writer dst
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
	err := f.fsys.Exec(
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

// CopyFrom copies the local reader src to the remote file
func (f *PosixFile) CopyFrom(src io.Reader) (int64, error) {
	if !f.isWritable() {
		return 0, f.pathErr(OpCopyFrom, fmt.Errorf("%w: file %s is not open for writing", fs.ErrClosed, f.path))
	}
	if err := f.fsys.Truncate(f.Name(), f.pos); err != nil {
		return 0, f.pathErr(OpCopyFrom, fmt.Errorf("truncate: %w", err))
	}
	counter := &ByteCounter{}

	err := f.fsys.Exec(
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

var (
	statCmdGNU = "stat -c '%%#f %%s %%.9Y //%%n//' -- %s 2> /dev/null"
	statCmdBSD = "stat -f '%%#p %%z %%Fm //%%N//' -- %s 2> /dev/null"
)

func (fsys *PosixFsys) initStat() error {
	if fsys.statCmd != nil {
		return nil
	}
	out, err := fsys.ExecOutput("stat --help 2>&1", exec.HideOutput())
	if err != nil {
		return fmt.Errorf("can't access stat command: %w", err)
	}
	if strings.Contains(out, "BusyBox") || strings.Contains(out, "--format=") {
		fsys.statCmd = &statCmdGNU
	} else {
		fsys.statCmd = &statCmdBSD
	}
	return nil
}

// second precision touch for busybox
func (fsys *PosixFsys) secChtimes(name string, atime, mtime int64) error {
	accessOrMod := [2]rune{'a', 'm'}
	// only supports setting one of them at a time
	for i, t := range [2]int64{atime, mtime} {
		ts := int64ToTime(t)
		utc := ts.UTC()
		cmd := fmt.Sprintf("[ -e %[3]s ] && env -i LC_ALL=C TZ=UTC touch -%[1]c -d @%[2]d -- %[3]s",
			accessOrMod[i],
			utc.Unix(),
			shellescape.Quote(name),
		)
		if err := fsys.Exec(cmd); err != nil {
			return fmt.Errorf("touch %s (%ctime): %w", name, accessOrMod[i], err)
		}
	}
	return nil
}

// nanosecond precision touch for stats that support it
func (fsys *PosixFsys) nsecChtimes(name string, atime, mtime int64) error {
	atimeTS := int64ToTime(atime)
	mtimeTS := int64ToTime(mtime)
	utcA := atimeTS.UTC()
	utcM := mtimeTS.UTC()
	cmd := fmt.Sprintf("[ -e %[3]s ] && env -i LC_ALL=C TZ=UTC touch -a -d %[1]s -m -d %[2]s -- %[3]s",
		fmt.Sprintf("%s.%09d", utcA.Format("2006-01-02T15:04:05"), utcA.Nanosecond()),
		fmt.Sprintf("%s.%09d", utcM.Format("2006-01-02T15:04:05"), utcM.Nanosecond()),
		shellescape.Quote(name),
	)
	if err := fsys.Exec(cmd); err != nil {
		return fmt.Errorf("touch (ns) %s: %w", name, err)
	}
	return nil
}

func (fsys *PosixFsys) initTouch() error {
	if fsys.chtimesFn != nil {
		return nil
	}
	out, err := fsys.ExecOutput("touch --help 2>&1", exec.HideOutput())
	if err != nil {
		return fmt.Errorf("can't access touch command: %w", err)
	}
	if strings.Contains(out, "BusyBox") {
		fsys.chtimesFn = fsys.secChtimes
		return nil
	}
	tmpF, err := CreateTemp(fsys, "", "rigfs-touch-test")
	if err != nil {
		return fmt.Errorf("can't create temp file for touch test: %w", err)
	}
	if err := tmpF.Close(); err != nil {
		return fmt.Errorf("can't close temp file for touch test: %w", err)
	}
	defer func() {
		_ = fsys.Remove(tmpF.Name())
	}()
	if err := fsys.nsecChtimes(tmpF.Name(), 0, 0); err != nil {
		fsys.chtimesFn = fsys.secChtimes
	} else {
		fsys.chtimesFn = fsys.nsecChtimes
	}

	return nil
}

func posixBitsToFileMode(bits int64) fs.FileMode {
	var mode fs.FileMode

	switch bits & 0o170000 {
	case 0o040000: // Directory
		mode |= fs.ModeDir
	case 0o100000: // Regular file
		// nop, no specific FileMode for regular files
	case 0o120000: // Symbolic link
		mode |= fs.ModeSymlink
	case 0o060000: // Block device
		mode |= fs.ModeDevice
	case 0o020000: // Character device
		mode |= fs.ModeDevice | fs.ModeCharDevice
	case 0o010000: // FIFO (Named pipe)
		mode |= fs.ModeNamedPipe
	case 0o140000: // Socket
		mode |= fs.ModeSocket
	}

	// Mapping permission bits
	mode |= fs.FileMode(bits & 0o777) // Owner, group, and other permissions

	// Mapping special permission bits
	if bits&0o4000 != 0 { // Set-user-ID
		mode |= fs.ModeSetuid
	}
	if bits&0o2000 != 0 { // Set-group-ID
		mode |= fs.ModeSetgid
	}
	if bits&0o1000 != 0 { // Sticky bit
		mode |= fs.ModeSticky
	}

	return mode
}

func (fsys *PosixFsys) parseStat(stat string) (*FileInfo, error) {
	// output looks like: 0x81a4 0 1699970097.220228000 //test_20231114155456.txt//
	parts := strings.SplitN(stat, " ", 4)
	if len(parts) != 4 {
		return nil, fmt.Errorf("%w: parse stat output %s", errInvalid, stat)
	}

	res := &FileInfo{fsys: fsys}

	if strings.HasPrefix(parts[0], "0x") {
		m, err := strconv.ParseInt(parts[0][2:], 16, 64)
		if err != nil {
			return nil, fmt.Errorf("parse stat mode %s: %w", stat, err)
		}
		res.FMode = posixBitsToFileMode(m)
	} else {
		m, err := strconv.ParseInt(parts[0], 8, 64)
		if err != nil {
			return nil, fmt.Errorf("parse stat mode %s: %w", stat, err)
		}
		res.FMode = posixBitsToFileMode(m)
	}

	res.FIsDir = res.FMode&fs.ModeDir != 0

	size, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parse stat size %s: %w", stat, err)
	}
	res.FSize = size

	timeParts := strings.SplitN(parts[2], ".", 2)
	mtime, err := strconv.ParseInt(timeParts[0], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parse stat mtime %s: %w", stat, err)
	}
	var mtimeNano int64
	if len(timeParts) == 2 {
		mtimeNano, err = strconv.ParseInt(timeParts[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse stat mtime ns %s: %w", stat, err)
		}
	}
	res.FModTime = time.Unix(mtime, mtimeNano)
	res.FName = strings.TrimSuffix(strings.TrimPrefix(parts[3], "//"), "//")

	return res, nil
}

func (fsys *PosixFsys) multiStat(names ...string) ([]fs.FileInfo, error) {
	if err := fsys.initStat(); err != nil {
		return nil, err
	}
	var idx int
	res := make([]fs.FileInfo, 0, len(names))
	var batch strings.Builder
	for idx < len(names) {
		batch.Reset()
		// build max 1kb batches of names to stat
		for batch.Len() < 1024 && idx < len(names) {
			if names[idx] != "" {
				batch.WriteString(shellescape.Quote(names[idx]))
				if idx < len(names)-1 {
					batch.WriteRune(' ')
				}
			}
			idx++
		}

		out, err := fsys.ExecOutput(*fsys.statCmd, batch.String())
		if err != nil {
			if len(names) == 1 {
				return nil, &fs.PathError{Op: OpStat, Path: names[0], Err: fs.ErrNotExist}
			}
			return nil, fmt.Errorf("stat %s: %w", names, err)
		}
		lines := strings.Split(out, "\n")
		for _, line := range lines {
			if line == "" {
				continue
			}
			info, err := fsys.parseStat(line)
			if err != nil {
				return res, err
			}
			res = append(res, info)
		}
	}
	return res, nil
}

// Stat returns the FileInfo structure describing file.
func (fsys *PosixFsys) Stat(name string) (fs.FileInfo, error) {
	items, err := fsys.multiStat(name)
	if err != nil {
		return nil, err
	}
	switch len(items) {
	case 0:
		return nil, &fs.PathError{Op: OpStat, Path: name, Err: fs.ErrNotExist}
	case 1:
		return items[0], nil
	default:
		return nil, fmt.Errorf("%w: stat %s: too many results", errInvalid, name)
	}
}

// Sha256 returns the sha256 checksum of the file at path
func (fsys *PosixFsys) Sha256(name string) (string, error) {
	out, err := fsys.ExecOutput("sha256sum -b %s", shellescape.Quote(name))
	if err != nil {
		if isNotExist(err) {
			return "", &fs.PathError{Op: "sha256sum", Path: name, Err: fs.ErrNotExist}
		}
		return "", fmt.Errorf("sha256sum %s: %w", name, err)
	}
	sha := strings.Fields(out)[0]
	if len(sha) != 64 {
		return "", fmt.Errorf("%w: sha256sum invalid output %s: %s", errInvalid, name, out)
	}
	return sha, nil
}

// Touch creates a new empty file at path or updates the timestamp of an existing file to the current time
func (fsys *PosixFsys) Touch(name string) error {
	err := fsys.Exec("touch -- %s", shellescape.Quote(name))
	if err != nil {
		return fmt.Errorf("touch %s: %w", name, err)
	}
	return nil
}

func int64ToTime(timestamp int64) time.Time {
	seconds := timestamp / 1e9
	nanoseconds := timestamp % 1e9
	return time.Unix(seconds, nanoseconds)
}

// Chtimes changes the access and modification times of the named file
func (fsys *PosixFsys) Chtimes(name string, atime, mtime int64) error {
	if err := fsys.initTouch(); err != nil {
		return err
	}
	return fsys.chtimesFn(name, atime, mtime)
}

// Truncate changes the size of the named file or creates a new file if it doesn't exist
func (fsys *PosixFsys) Truncate(name string, size int64) error {
	if err := fsys.Exec("truncate -s %d %s", size, shellescape.Quote(name)); err != nil {
		return fmt.Errorf("truncate %s: %w", name, err)
	}
	return nil
}

// Chmod changes the mode of the named file to mode
func (fsys *PosixFsys) Chmod(name string, mode fs.FileMode) error {
	if err := fsys.Exec("chmod %#o %s", mode, shellescape.Quote(name)); err != nil {
		if isNotExist(err) {
			return &fs.PathError{Op: "chmod", Path: name, Err: fs.ErrNotExist}
		}
		return fmt.Errorf("chmod %s: %w", name, err)
	}
	return nil
}

// Chown changes the numeric uid and gid of the named file
func (fsys *PosixFsys) Chown(name string, uid, gid int) error {
	if err := fsys.Exec("chown %d:%d %s", uid, gid, shellescape.Quote(name)); err != nil {
		if isNotExist(err) {
			return &fs.PathError{Op: "chown", Path: name, Err: fs.ErrNotExist}
		}
		return fmt.Errorf("chown %s: %w", name, err)
	}
	return nil
}

// Open opens the named file for reading.
func (fsys *PosixFsys) Open(name string) (fs.File, error) {
	return fsys.OpenFile(name, os.O_RDONLY, 0)
}

func (fsys *PosixFsys) openNew(name string, flags int, perm fs.FileMode) (fs.FileInfo, error) {
	if flags&os.O_CREATE == 0 {
		return nil, &fs.PathError{Op: OpOpen, Path: name, Err: fs.ErrNotExist}
	}

	if _, err := fsys.Stat(path.Dir(name)); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, &fs.PathError{Op: OpOpen, Path: name, Err: fmt.Errorf("%w: parent directory does not exist", fs.ErrNotExist)}
		}
		return nil, &fs.PathError{Op: OpOpen, Path: name, Err: fmt.Errorf("%w: failed to stat parent directory", fs.ErrInvalid)}
	}

	if err := fsys.Exec("install -m %#o /dev/null %s", perm, shellescape.Quote(name)); err != nil {
		return nil, &fs.PathError{Op: OpOpen, Path: name, Err: err}
	}

	// re-stat to ensure file is now there and get the correct bits if there's a umask
	return fsys.Stat(name)
}

func (fsys *PosixFsys) openExisting(name string, flags int, info fs.FileInfo) (fs.FileInfo, error) {
	// directories can't be opened for writing
	if info.IsDir() && flags&(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_EXCL) != 0 {
		return nil, &fs.PathError{Op: OpOpen, Path: name, Err: fmt.Errorf("%w: is a directory", fs.ErrInvalid)}
	}

	// if O_CREATE and O_EXCL are set, the file must not exist
	if flags&(os.O_CREATE|os.O_EXCL) == (os.O_CREATE | os.O_EXCL) {
		return nil, &fs.PathError{Op: OpOpen, Path: name, Err: fs.ErrExist}
	}

	if flags&os.O_TRUNC != 0 {
		if err := fsys.Truncate(name, 0); err != nil {
			return nil, err
		}
	}

	return fsys.Stat(name)
}

// OpenFile is used to open a file with access/creation flags for reading or writing. For info on flags,
// see https://pkg.go.dev/os#pkg-constants
func (fsys *PosixFsys) OpenFile(name string, flags int, perm fs.FileMode) (File, error) {
	if flags&^supportedFlags != 0 {
		return nil, fmt.Errorf("%w: unsupported flags: %d", errInvalid, flags)
	}

	info, err := fsys.Stat(name)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		info, err = fsys.openNew(name, flags, perm)
	} else {
		info, err = fsys.openExisting(name, flags, info)
	}

	if err != nil {
		return nil, err
	}

	var pos int64
	if flags&os.O_APPEND != 0 {
		pos = info.Size()
	}

	file := &PosixFile{
		withPath: withPath{name},
		fsys:     fsys,
		isOpen:   true,
		size:     info.Size(),
		pos:      pos,
		mode:     info.Mode(),
		flags:    flags,
	}
	if info.IsDir() {
		return &PosixDir{PosixFile: *file}, nil
	}
	return file, nil
}

// ReadDir reads the directory named by dirname and returns a list of directory entries
func (fsys *PosixFsys) ReadDir(name string) ([]fs.DirEntry, error) {
	if name == "" {
		name = "."
	}

	out, err := fsys.ExecOutput("find %s -maxdepth 1 -print0", shellescape.Quote(name))
	if err != nil {
		return nil, fmt.Errorf("read dir (find) %s: %w", name, err)
	}
	items := strings.Split(out, "\x00")
	if len(items) == 0 || (len(items) == 1 && items[0] == "") {
		return nil, &fs.PathError{Op: "read dir", Path: name, Err: fs.ErrNotExist}
	}
	if items[0] != name {
		return nil, &fs.PathError{Op: "read dir", Path: name, Err: fs.ErrNotExist}
	}
	if len(items) == 1 {
		return nil, nil
	}

	res := make([]fs.DirEntry, 0, len(items)-1)
	infos, err := fsys.multiStat(items[1:]...)
	for _, entry := range infos {
		if info, ok := entry.(fs.DirEntry); ok {
			res = append(res, info)
		}
	}
	return res, err
}

// Remove deletes the named file or (empty) directory.
func (fsys *PosixFsys) Remove(name string) error {
	if err := fsys.Exec("rm -f %s", shellescape.Quote(name)); err != nil {
		return fmt.Errorf("delete %s: %w", name, err)
	}
	return nil
}

func isNotExist(err error) bool {
	return err != nil && (errors.Is(err, fs.ErrNotExist) || strings.Contains(err.Error(), "No such file or directory"))
}

// RemoveAll removes path and any children it contains.
func (fsys *PosixFsys) RemoveAll(name string) error {
	if err := fsys.Exec("rm -rf %s", shellescape.Quote(name)); err != nil {
		return fmt.Errorf("remove all %s: %w", name, err)
	}
	return nil
}

// Rename renames (moves) oldpath to newpath.
func (fsys *PosixFsys) Rename(oldpath, newpath string) error {
	if err := fsys.Exec("mv -f %s %s", shellescape.Quote(oldpath), shellescape.Quote(newpath)); err != nil {
		return fmt.Errorf("rename %s -> %s: %w", oldpath, newpath, err)
	}
	return nil
}

// TempDir returns the default directory to use for temporary files.
func (fsys *PosixFsys) TempDir() string {
	out, err := fsys.ExecOutput("echo ${TMPDIR:-/tmp}")
	if err != nil {
		return "/tmp"
	}
	return out
}

// MkdirAll creates a new directory structure with the specified name and permission bits.
// If the directory already exists, MkDirAll does nothing and returns nil.
func (fsys *PosixFsys) MkdirAll(name string, perm fs.FileMode) error {
	dir := shellescape.Quote(name)
	if existing, err := fsys.Stat(name); err == nil {
		if existing.IsDir() {
			return nil
		}
		return fmt.Errorf("mkdir %s: %w", name, fs.ErrExist)
	}

	if err := fsys.Exec("install -d -m %#o %s", perm, shellescape.Quote(dir)); err != nil {
		return fmt.Errorf("mkdir %s: %w", name, err)
	}

	return nil
}

// Mkdir creates a new directory with the specified name and permission bits.
func (fsys *PosixFsys) Mkdir(name string, perm fs.FileMode) error {
	if err := fsys.Exec("mkdir -m %#o %s", perm, shellescape.Quote(name)); err != nil {
		return &fs.PathError{Op: "mkdir", Path: name, Err: err}
	}

	return nil
}

// WriteFile writes data to a file named by filename.
func (fsys *PosixFsys) WriteFile(filename string, data []byte, perm fs.FileMode) error {
	if err := fsys.Exec("install -D -m %#o /dev/stdin %s", perm, shellescape.Quote(filename), exec.Stdin(bytes.NewReader(data))); err != nil {
		return fmt.Errorf("write file %s: %w", filename, err)
	}
	return nil
}

// ReadFile reads the file named by filename and returns the contents.
func (fsys *PosixFsys) ReadFile(filename string) ([]byte, error) {
	out, err := fsys.ExecOutput("cat -- %s", shellescape.Quote(filename), exec.HideOutput(), exec.TrimOutput(false))
	if err != nil {
		return nil, fmt.Errorf("read file %s: %w", filename, err)
	}
	return []byte(out), nil
}

// MkdirTemp creates a new temporary directory in the directory dir with a name beginning with prefix and returns the path of the new directory.
func (fsys *PosixFsys) MkdirTemp(dir, prefix string) (string, error) {
	if dir == "" {
		dir = fsys.TempDir()
	}
	out, err := fsys.ExecOutput("mktemp -d %s", shellescape.Quote(fsys.Join(dir, prefix+"XXXXXX")))
	if err != nil {
		return "", fmt.Errorf("mkdir temp %s: %w", dir, err)
	}
	return out, nil
}

// FileExist checks if a file exists on the host
func (fsys *PosixFsys) FileExist(name string) bool {
	return fsys.Exec("test -f %s", shellescape.Quote(name), exec.HideOutput()) == nil
}

// CommandExist checks if a command exists on the host
func (fsys *PosixFsys) CommandExist(name string) bool {
	return fsys.Exec("command -v %s", name, exec.HideOutput()) == nil
}

// Join joins any number of path elements into a single path, adding a separating slash if necessary.
func (fsys *PosixFsys) Join(elem ...string) string {
	return path.Join(elem...)
}

// Getenv returns the value of the environment variable named by the key
func (fsys *PosixFsys) Getenv(key string) string {
	out, err := fsys.ExecOutput("echo ${%s}", key, exec.HideOutput())
	if err != nil {
		return ""
	}
	return out
}

// Hostname returns the name of the host
func (fsys *PosixFsys) Hostname() (string, error) {
	out, err := fsys.ExecOutput("hostname")
	if err != nil {
		return "", fmt.Errorf("hostname: %w", err)
	}
	return out, nil
}

// LongHostname returns the FQDN of the host
func (fsys *PosixFsys) LongHostname() (string, error) {
	out, err := fsys.ExecOutput("hostname -f 2> /dev/null")
	if err != nil {
		return "", fmt.Errorf("hostname -f: %w", err)
	}

	return out, nil
}

// UserCacheDir returns the default root directory to use for user-specific cached data.
func (fsys *PosixFsys) UserCacheDir() string {
	if cache := fsys.Getenv("XDG_CACHE_HOME"); cache != "" {
		return cache
	}
	return fsys.Join(fsys.UserHomeDir(), ".cache")
}

// UserConfigDir returns the default root directory to use for user-specific configuration data.
func (fsys *PosixFsys) UserConfigDir() string {
	if config := fsys.Getenv("XDG_CONFIG_HOME"); config != "" {
		return config
	}
	return fsys.Join(fsys.UserHomeDir(), ".config")
}

// UserHomeDir returns the current user's home directory.
func (fsys *PosixFsys) UserHomeDir() string {
	return fsys.Getenv("HOME")
}
