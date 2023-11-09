package rigfs

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/alessio/shellescape"
	"github.com/k0sproject/rig/exec"
)

// rigHelper is a helper script to avoid having to write complex bash oneliners in Go
// it's not a read-loop "daemon" like the windows counterpart rigrcp.ps1
//
//go:embed righelper.sh
var rigHelper string

var (
	_ fs.File        = (*PosixFile)(nil)
	_ fs.ReadDirFile = (*PosixDir)(nil)
	_ fs.FS          = (*PosixFsys)(nil)
)

// PosixFsys implements fs.FS for a remote filesystem that uses POSIX commands for access
type PosixFsys struct {
	conn connection
	opts []exec.Option
}

// NewPosixFsys returns a fs.FS implementation for a remote filesystem that uses POSIX commands for access
func NewPosixFsys(conn connection, opts ...exec.Option) *PosixFsys {
	return &PosixFsys{conn: conn, opts: opts}
}

const defaultBlockSize = 4096

// PosixFile implements fs.File for a remote file
type PosixFile struct {
	fsys   *PosixFsys
	path   string
	isOpen bool
	isEOF  bool
	pos    int64
	size   int64
	mode   FileMode

	blockSize int
}

// PosixDir implements fs.ReadDirFile for a remote directory
type PosixDir struct {
	PosixFile
	entries []fs.DirEntry
	hw      int
}

// ReadDir returns a list of directory entries
func (f *PosixDir) ReadDir(n int) ([]fs.DirEntry, error) {
	if n == 0 {
		return f.PosixFile.fsys.ReadDir(f.path)
	}
	if f.entries == nil {
		entries, err := f.PosixFile.fsys.ReadDir(f.path)
		if err != nil {
			return nil, err
		}
		f.entries = entries
		f.hw = 0
	}
	if f.hw >= len(f.entries) {
		return nil, io.EOF
	}
	var min int
	if n > len(f.entries)-f.hw {
		min = len(f.entries) - f.hw
	} else {
		min = n
	}
	old := f.hw
	f.hw += min
	return f.entries[old:f.hw], nil
}

func (f *PosixFile) fsBlockSize() int {
	if f.blockSize > 0 {
		return f.blockSize
	}

	out, err := f.fsys.conn.ExecOutput(fmt.Sprintf(`stat -c "%%s" %[1]s 2> /dev/null || stat -f "%%k" %[1]s`, shellescape.Quote(filepath.Dir(f.path))), f.fsys.opts...)
	if err != nil {
		// fall back to default
		f.blockSize = defaultBlockSize
	} else if bs, err := strconv.Atoi(strings.TrimSpace(out)); err == nil {
		f.blockSize = bs
	}

	return f.blockSize
}

func (f *PosixFile) isReadable() bool {
	return f.mode&ModeRead != 0
}

func (f *PosixFile) isWritable() bool {
	return f.mode&ModeWrite != 0
}

func (f *PosixFile) ddParams(offset int64, numBytes int) (int, int64, int) {
	bs := f.fsBlockSize()

	if numBytes < bs {
		bs = numBytes
		skip := offset / int64(bs)
		return bs, skip, 1
	}

	skip := offset / int64(bs)
	count := numBytes / bs
	return bs, skip, count
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
		return 0, fmt.Errorf("%w: file %s is not open for reading", ErrCommandFailed, f.path)
	}
	errbuf := bytes.NewBuffer(nil)

	bs, skip, count := f.ddParams(f.pos, len(p))
	toRead := bs * count
	buf := bytes.NewBuffer(nil)
	errbuf.Reset()

	cmd, err := f.fsys.conn.ExecStreams(fmt.Sprintf("dd if=%s bs=%d skip=%d count=%d", shellescape.Quote(f.path), bs, skip, count), nil, buf, errbuf, f.fsys.opts...)
	if err != nil {
		return 0, fmt.Errorf("%w: failed to execute dd: %w (%s)", ErrCommandFailed, err, errbuf.String())
	}
	if err := cmd.Wait(); err != nil {
		return 0, fmt.Errorf("%w: read (dd): %w (%s)", ErrCommandFailed, err, errbuf.String())
	}

	readBytes := copy(p, buf.Bytes())
	f.pos += int64(readBytes)
	if readBytes < len(p) || readBytes < toRead {
		f.isEOF = true
		return readBytes, io.EOF
	}
	return readBytes, nil
}

func (f *PosixFile) Write(p []byte) (int, error) {
	if !f.isWritable() {
		return 0, fmt.Errorf("%w: file %s is not open for writing", ErrCommandFailed, f.path)
	}

	var written int
	remaining := p
	for written < len(p) {
		bs, skip, count := f.ddParams(f.pos, len(remaining))
		toWrite := bs * count

		errbuf := bytes.NewBuffer(nil)
		limitedReader := bytes.NewReader(remaining[:toWrite])
		cmd, err := f.fsys.conn.ExecStreams(fmt.Sprintf("dd if=/dev/stdin of=%s bs=%d count=%d seek=%d conv=notrunc", f.path, bs, count, skip), io.NopCloser(limitedReader), io.Discard, errbuf, f.fsys.opts...)
		if err != nil {
			return 0, fmt.Errorf("%w: write (dd): %w", ErrCommandFailed, err)
		}
		if err := cmd.Wait(); err != nil {
			return 0, fmt.Errorf("%w: write (dd): %w (%s)", ErrCommandFailed, err, errbuf.String())
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

// CopyFromN copies n bytes from the remote file. The alt writer can be used for progress
// tracking, use nil when not needed.
func (f *PosixFile) CopyFromN(src io.Reader, num int64, alt io.Writer) (int64, error) {
	if !f.isWritable() {
		return 0, fmt.Errorf("%w: file %s is not open for writing", ErrCommandFailed, f.path)
	}
	var ddCmd string
	if f.pos+num >= f.size {
		if _, err := f.fsys.helper("truncate", f.path, strconv.FormatInt(f.pos, 10)); err != nil {
			return 0, fmt.Errorf("%w: truncate %s for writing: %w", ErrCommandFailed, f.path, err)
		}
		ddCmd = fmt.Sprintf("dd if=/dev/stdin of=%s bs=16M oflag=append conv=notrunc", shellescape.Quote(f.path))
	} else {
		ddCmd = fmt.Sprintf("dd if=/dev/stdin of=%s bs=1 seek=%d conv=notrunc", shellescape.Quote(f.path), f.pos)
	}
	limited := io.LimitReader(src, num)
	var reader io.Reader
	if alt != nil {
		reader = io.TeeReader(limited, alt)
	} else {
		reader = limited
	}

	errbuf := bytes.NewBuffer(nil)
	cmd, err := f.fsys.conn.ExecStreams(ddCmd, io.NopCloser(reader), io.Discard, errbuf, f.fsys.opts...)
	if err != nil {
		return 0, fmt.Errorf("%w: failed to execute dd (copy-from): %w (%s)", ErrCommandFailed, err, errbuf.String())
	}
	if err != nil {
		return 0, fmt.Errorf("%w: copy-from: %w", ErrCommandFailed, err)
	}
	f.pos += num
	if f.pos >= f.size {
		f.isEOF = true
		f.size = f.pos
	}
	if err != nil {
		return 0, &fs.PathError{Op: "copy-from", Path: f.path, Err: fmt.Errorf("%w: error while copying: %w", ErrRcpCommandFailed, err)}
	}
	if err := cmd.Wait(); err != nil {
		return 0, &fs.PathError{Op: "copy-from", Path: f.path, Err: fmt.Errorf("%w: error while copying: %w (%s)", ErrRcpCommandFailed, err, errbuf.String())}
	}
	return num, nil
}

// Copy copies the remote file at src to the local file at dst
func (f *PosixFile) Copy(dst io.Writer) (int64, error) {
	if f.isEOF {
		return 0, io.EOF
	}
	if !f.isReadable() {
		return 0, fmt.Errorf("%w: file %s is not open for reading", ErrCommandFailed, f.path)
	}
	bs, skip, count := f.ddParams(f.pos, int(f.size-f.pos))
	errbuf := bytes.NewBuffer(nil)
	cmd, err := f.fsys.conn.ExecStreams(fmt.Sprintf("dd if=%s bs=%d skip=%d count=%d", shellescape.Quote(f.path), bs, skip, count), nil, dst, errbuf, f.fsys.opts...)
	if err != nil {
		return 0, fmt.Errorf("%w: failed to execute dd (copy): %w (%s)", ErrCommandFailed, err, errbuf.String())
	}
	if err := cmd.Wait(); err != nil {
		return 0, fmt.Errorf("%w: copy (dd): %w (%s)", ErrCommandFailed, err, errbuf.String())
	}
	f.pos = f.size
	f.isEOF = true
	return f.size - f.pos, nil
}

// Close closes the file, rendering it unusable for I/O. It returns an error, if any.
func (f *PosixFile) Close() error {
	f.isOpen = false
	return nil
}

// Seek sets the offset for the next Read or Write to offset, interpreted according to whence:
// 0 means relative to the origin of the file,
// 1 means relative to the current offset, and
// 2 means relative to the end.
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
		return 0, fmt.Errorf("%w: invalid whence: %d", ErrCommandFailed, whence)
	}
	f.isEOF = f.pos >= f.size

	return f.pos, nil
}

type helperResponse struct {
	Err       error        `json:"-"`
	ErrString string       `json:"error"`
	Stat      *FileInfo    `json:"stat"`
	Dir       []*FileInfo  `json:"dir"`
	Sum       *sumResponse `json:"sum"`
}

func (h *helperResponse) UnmarshalJSON(b []byte) error {
	type helperresponse *helperResponse
	hr := helperresponse(h)
	if err := json.Unmarshal(b, hr); err != nil {
		return fmt.Errorf("%w: unmarshal helper response: %w", ErrCommandFailed, err)
	}
	if hr.ErrString != "" {
		hr.Err = fmt.Errorf("%w: %s", ErrCommandFailed, strings.TrimSpace(hr.ErrString))
	}
	return nil
}

func (fsys *PosixFsys) helper(args ...string) (*helperResponse, error) {
	var res helperResponse
	opts := fsys.opts
	opts = append(opts, exec.Stdin(rigHelper))
	out, err := fsys.conn.ExecOutput(fmt.Sprintf("sh -s -- %s", shellescape.QuoteCommand(args)), opts...)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to execute helper: %w", ErrCommandFailed, err)
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		return nil, fmt.Errorf("%w: helper response unmarshal: %w", ErrCommandFailed, err)
	}
	if res.Err != nil {
		return &res, res.Err
	}
	return &res, nil
}

// Stat returns the FileInfo structure describing file.
func (fsys *PosixFsys) Stat(name string) (fs.FileInfo, error) {
	res, err := fsys.helper("stat", name)
	if err != nil {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: fmt.Errorf("%w: %w", fs.ErrNotExist, err)}
	}
	if res.Stat == nil {
		return nil, fmt.Errorf("%w: helper stat response empty", ErrCommandFailed)
	}
	return res.Stat, nil
}

// Sha256 returns the sha256 checksum of the file at path
func (fsys *PosixFsys) Sha256(name string) (string, error) {
	res, err := fsys.helper("sum", name)
	if err != nil {
		return "", err
	}
	if res.Sum == nil {
		return "", fmt.Errorf("%w: helper sum response empty", ErrCommandFailed)
	}
	return res.Sum.Sha256, nil
}

// Open opens the named file for reading.
func (fsys *PosixFsys) Open(name string) (fs.File, error) {
	info, err := fsys.Stat(name)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	file := PosixFile{fsys: fsys, path: name, isOpen: true, size: info.Size(), mode: ModeRead}
	if info.IsDir() {
		return &PosixDir{PosixFile: file}, nil
	}
	return &file, nil
}

// OpenFile is the generalized open call; most users will use Open instead.
func (fsys *PosixFsys) OpenFile(name string, mode FileMode, perm FileMode) (File, error) {
	var pos int64
	info, err := fsys.Stat(name)
	if err != nil {
		switch {
		case mode&ModeRead == ModeRead:
			return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
		case mode&ModeCreate == ModeCreate:
			if _, err := fsys.helper("touch", name, fmt.Sprintf("%#o", perm)); err != nil {
				return nil, err
			}
		}
		info = &FileInfo{FName: name, FMode: fs.FileMode(perm), FSize: 0, FIsDir: false, FModTime: time.Now(), fsys: fsys}
	}
	if info.IsDir() {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fmt.Errorf("%w: is a directory", fs.ErrPermission)}
	}
	switch {
	case mode&ModeAppend == ModeAppend:
		pos = info.Size()
	case mode&ModeCreate == ModeCreate:
		if _, err := fsys.helper("truncate", name, "0"); err != nil {
			return nil, err
		}
	}
	return &PosixFile{fsys: fsys, path: name, isOpen: true, size: info.Size(), pos: pos, mode: mode}, nil
}

// ReadDir reads the directory named by dirname and returns a list of directory entries
func (fsys *PosixFsys) ReadDir(name string) ([]fs.DirEntry, error) {
	if name == "" {
		name = "."
	}
	res, err := fsys.helper("dir", name)
	if err != nil {
		return nil, err
	}
	if res.Dir == nil {
		return nil, fmt.Errorf("%w: helper dir response empty", ErrCommandFailed)
	}
	entries := make([]fs.DirEntry, len(res.Dir))
	for i, entry := range res.Dir {
		entries[i] = entry
	}
	return entries, nil
}

// Remove deletes the named file or (empty) directory.
func (fsys *PosixFsys) Remove(name string) error {
	if err := fsys.conn.Exec(fmt.Sprintf("rm -f %s", shellescape.Quote(name)), fsys.opts...); err != nil {
		return fmt.Errorf("%w: delete %s: %w", ErrCommandFailed, name, err)
	}
	return nil
}

// RemoveAll removes path and any children it contains.
func (fsys *PosixFsys) RemoveAll(name string) error {
	if err := fsys.conn.Exec(fmt.Sprintf("rm -rf %s", shellescape.Quote(name)), fsys.opts...); err != nil {
		return fmt.Errorf("%w: remove all %s: %w", ErrCommandFailed, name, err)
	}
	return nil
}

// MkDirAll creates a new directory structure with the specified name and permission bits.
// If the directory already exists, MkDirAll does nothing and returns nil.
func (fsys *PosixFsys) MkDirAll(name string, perm FileMode) error {
	dir := shellescape.Quote(name)
	if existing, err := fsys.Stat(name); err == nil {
		if existing.IsDir() {
			return nil
		}
		return fmt.Errorf("%w: mkdir %s: %w", ErrCommandFailed, name, fs.ErrExist)
	}

	if err := fsys.conn.Exec(fmt.Sprintf("mkdir -p %s", dir), fsys.opts...); err != nil {
		return fmt.Errorf("%w: mkdir %s: %w", ErrCommandFailed, name, err)
	}

	if err := fsys.conn.Exec(fmt.Sprintf("chmod %#o %s", os.FileMode(perm).Perm(), dir), fsys.opts...); err != nil {
		return fmt.Errorf("%w: chmod (mkdir) %s: %w", ErrCommandFailed, name, err)
	}

	return nil
}
