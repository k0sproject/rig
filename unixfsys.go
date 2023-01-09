package rig

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"math/big"
	"strings"
	"time"

	"github.com/alessio/shellescape"
	"github.com/k0sproject/rig/errstring"
	"github.com/k0sproject/rig/exec"
)

// rigHelper is a helper script to avoid having to write complex bash oneliners in Go
// it's not a read-loop "daemon" like the windows counterpart rigrcp.ps1
//
//go:embed script/righelper.bash
var rigHelper string

var (
	_ fs.File        = &unixFSFile{}
	_ fs.ReadDirFile = &unixFSDir{}
	_ fs.FS          = &unixFsys{}
)

type unixFsys struct {
	conn *Connection
	opts []exec.Option
}

func newUnixFsys(conn *Connection, opts ...exec.Option) *unixFsys {
	return &unixFsys{conn: conn, opts: opts}
}

type unixFSFile struct {
	fsys   *unixFsys
	path   string
	isOpen bool
	isEOF  bool
	pos    int64
	size   int64
	mode   FileMode
}

type unixFSDir struct {
	unixFSFile
	entries []fs.DirEntry
	hw      int
}

func (f *unixFSDir) ReadDir(n int) ([]fs.DirEntry, error) {
	if n == 0 {
		return f.unixFSFile.fsys.ReadDir(f.path)
	}
	if f.entries == nil {
		entries, err := f.unixFSFile.fsys.ReadDir(f.path)
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

func (f *unixFSFile) isReadable() bool {
	return f.mode&ModeRead != 0
}

func (f *unixFSFile) isWritable() bool {
	return f.mode&ModeWrite != 0
}

// ddParams returns "optimal" parameters for a dd command to extract bytesToRead bytes at offset
// from a file with fileSize length
func (f *unixFSFile) ddParams(offset int64, toRead int) (int, int64, int) {
	offsetB := big.NewInt(offset)
	toReadB := big.NewInt(int64(toRead))

	// find the greatest common divisor of the offset and the number of bytes to read
	gcdB := big.NewInt(0)
	gcdB.GCD(nil, nil, offsetB, toReadB)
	blockSize := int(gcdB.Int64())

	skip := offset / int64(blockSize)
	count := toRead / blockSize

	return blockSize, skip, count
}

func (f *unixFSFile) Stat() (fs.FileInfo, error) {
	return f.fsys.Stat(f.path)
}

func (f *unixFSFile) Read(p []byte) (int, error) {
	if f.isEOF {
		return 0, io.EOF
	}
	if !f.isReadable() {
		return 0, ErrCommandFailed.Wrapf("file %s is not open for reading", f.path)
	}
	bs, skip, count := f.ddParams(f.pos, len(p))
	errbuf := bytes.NewBuffer(nil)
	buf := bytes.NewBuffer(nil)
	cmd, err := f.fsys.conn.ExecStreams(fmt.Sprintf("dd if=%s bs=%d skip=%d count=%d", shellescape.Quote(f.path), bs, skip, count), nil, buf, errbuf, f.fsys.opts...)
	if err != nil {
		return 0, ErrCommandFailed.Wrapf("failed to execute dd: %w (%s)", err, errbuf.String())
	}
	if err := cmd.Wait(); err != nil {
		return 0, ErrCommandFailed.Wrapf("read (dd): %w (%s)", err, errbuf.String())
	}
	f.pos += int64(buf.Len())
	if buf.Len() < len(p) {
		f.isEOF = true
	}
	return copy(p, buf.Bytes()), nil
}

func (f *unixFSFile) Write(p []byte) (int, error) {
	if !f.isWritable() {
		return 0, ErrCommandFailed.Wrapf("file %s is not open for writing", f.path)
	}
	bs, skip, count := f.ddParams(f.pos, len(p))
	errbuf := bytes.NewBuffer(nil)
	cmd, err := f.fsys.conn.ExecStreams(fmt.Sprintf("dd if=/dev/stdin of=%s bs=%d count=%d seek=%d", shellescape.Quote(f.path), bs, count, skip), io.NopCloser(bytes.NewReader(p)), io.Discard, errbuf, f.fsys.opts...)
	if err != nil {
		return 0, ErrCommandFailed.Wrapf("write (dd): %w", err)
	}
	if err := cmd.Wait(); err != nil {
		return 0, ErrCommandFailed.Wrapf("write (dd): %w (%s)", err, errbuf.String())
	}
	f.pos += int64(len(p))
	if f.pos > f.size {
		f.size = f.pos
		f.isEOF = true
	}
	return len(p), nil
}

func (f *unixFSFile) CopyFromN(src io.Reader, num int64, alt io.Writer) (int64, error) {
	if !f.isWritable() {
		return 0, ErrCommandFailed.Wrapf("file %s is not open for writing", f.path)
	}
	var ddCmd string
	if f.pos+num >= f.size {
		if _, err := f.fsys.helper("truncate", f.path, fmt.Sprintf("%d", f.pos)); err != nil {
			return 0, ErrCommandFailed.Wrapf("truncate for writing: %w", err)
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
		return 0, ErrCommandFailed.Wrapf("failed to execute dd (copy-from): %w (%s)", err, errbuf.String())
	}
	if err != nil {
		return 0, ErrCommandFailed.Wrapf("copy-from: %w", err)
	}
	f.pos += num
	if f.pos >= f.size {
		f.isEOF = true
		f.size = f.pos
	}
	if err != nil {
		return 0, &fs.PathError{Op: "copy-from", Path: f.path, Err: ErrRcpCommandFailed.Wrapf("error while copying: %w", err)}
	}
	if err := cmd.Wait(); err != nil {
		return 0, &fs.PathError{Op: "copy-from", Path: f.path, Err: ErrRcpCommandFailed.Wrapf("error while copying: %w (%s)", err, errbuf.String())}
	}
	return num, nil
}

func (f *unixFSFile) Copy(dst io.Writer) (int, error) {
	if f.isEOF {
		return 0, io.EOF
	}
	if !f.isReadable() {
		return 0, ErrCommandFailed.Wrapf("file %s is not open for reading", f.path)
	}
	bs, skip, count := f.ddParams(f.pos, int(f.size-f.pos))
	errbuf := bytes.NewBuffer(nil)
	cmd, err := f.fsys.conn.ExecStreams(fmt.Sprintf("dd if=%s bs=%d skip=%d count=%d", shellescape.Quote(f.path), bs, skip, count), nil, dst, errbuf, f.fsys.opts...)
	if err != nil {
		return 0, ErrCommandFailed.Wrapf("failed to execute dd (copy): %w (%s)", err, errbuf.String())
	}
	if err := cmd.Wait(); err != nil {
		return 0, ErrCommandFailed.Wrapf("copy (dd): %w (%s)", err, errbuf.String())
	}
	f.pos = f.size
	f.isEOF = true
	return int(f.size - f.pos), nil
}

func (f *unixFSFile) Close() error {
	f.isOpen = false
	return nil
}

func (f *unixFSFile) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		f.pos = offset
	case io.SeekCurrent:
		f.pos += offset
	case io.SeekEnd:
		f.pos = f.size + offset
	default:
		return 0, ErrCommandFailed.Wrapf("invalid whence: %d", whence)
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
		return ErrCommandFailed.Wrapf("unmarshal helper response: %w", err)
	}
	if hr.ErrString != "" {
		hr.Err = errstring.New(strings.TrimSpace(hr.ErrString))
	}
	return nil
}

func (fsys *unixFsys) helper(args ...string) (*helperResponse, error) {
	var res helperResponse
	opts := fsys.opts
	opts = append(opts, exec.Stdin(rigHelper))
	out, err := fsys.conn.ExecOutput(fmt.Sprintf("bash -s -- %s", shellescape.QuoteCommand(args)), opts...)
	if err != nil {
		return nil, ErrCommandFailed.Wrapf("failed to execute helper: %w", err)
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		return nil, ErrCommandFailed.Wrapf("helper response unmarshal: %w", err)
	}
	if res.Err != nil {
		return &res, res.Err
	}
	return &res, nil
}

func (fsys *unixFsys) Stat(name string) (fs.FileInfo, error) {
	res, err := fsys.helper("stat", name)
	if err != nil {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: fmt.Errorf("%w: %s", fs.ErrNotExist, err)}
	}
	if res.Stat == nil {
		return nil, ErrCommandFailed.Wrapf("helper stat response empty")
	}
	return res.Stat, nil
}

func (fsys *unixFsys) Sha256(name string) (string, error) {
	res, err := fsys.helper("sum", name)
	if err != nil {
		return "", err
	}
	if res.Sum == nil {
		return "", ErrCommandFailed.Wrapf("helper sum response empty")
	}
	return res.Sum.Sha256, nil
}

func (fsys *unixFsys) Open(name string) (fs.File, error) {
	info, err := fsys.Stat(name)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	file := unixFSFile{fsys: fsys, path: name, isOpen: true, size: info.Size(), mode: ModeRead}
	if info.IsDir() {
		return &unixFSDir{unixFSFile: file}, nil
	}
	return &file, nil
}

func (fsys *unixFsys) OpenFile(name string, mode FileMode, perm int) (File, error) {
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
		info = &FileInfo{FName: name, FUnix: fs.FileMode(perm), FSize: 0, FIsDir: false, FModTime: time.Now(), fsys: fsys}
	}
	if info.IsDir() {
		return nil, &fs.PathError{Op: "open", Path: name, Err: ErrCommandFailed.Wrapf("%w: is a directory", fs.ErrPermission)}
	}
	switch {
	case mode&ModeAppend == ModeAppend:
		pos = info.Size()
	case mode&ModeCreate == ModeCreate:
		if _, err := fsys.helper("truncate", name, "0"); err != nil {
			return nil, err
		}
	}
	return &unixFSFile{fsys: fsys, path: name, isOpen: true, size: info.Size(), pos: pos, mode: mode}, nil
}

func (fsys *unixFsys) ReadDir(name string) ([]fs.DirEntry, error) {
	if name == "" {
		name = "."
	}
	res, err := fsys.helper("dir", name)
	if err != nil {
		return nil, err
	}
	if res.Dir == nil {
		return nil, ErrCommandFailed.Wrapf("helper dir response empty")
	}
	entries := make([]fs.DirEntry, len(res.Dir))
	for i, entry := range res.Dir {
		entries[i] = entry
	}
	return entries, nil
}

// Delete removes the named file or (empty) directory.
func (fsys *unixFsys) Delete(name string) error {
	if err := fsys.conn.Exec(fmt.Sprintf("rm -f %s", shellescape.Quote(name)), fsys.opts...); err != nil {
		return ErrCommandFailed.Wrapf("delete %s: %w", name, err)
	}
	return nil
}
