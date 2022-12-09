package rig

import (
	"bufio"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/k0sproject/rig/errstring"
	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/log"
	ps "github.com/k0sproject/rig/powershell"
)

const bufSize = 32768

var (
	// ErrNotRunning is returned when the rigrcp process is not running
	ErrNotRunning = errstring.New("rigrcp is not running")
	// ErrRcpCommandFailed is returned when a command to the rigrcp process fails
	ErrRcpCommandFailed = errstring.New("rigrcp command failed")
)

// rigrcpScript is a helper script for transferring files between local and remote systems
//
//go:embed script/rigrcp.ps1
var rigrcpScript string

var (
	_ fs.File        = &winfsFile{}
	_ fs.ReadDirFile = &winfsDir{}
	_ fs.FS          = &windowsFsys{}
)

type windowsFsys struct {
	conn *Connection
	rcp  *rigrcp
	buf  []byte
	mu   sync.Mutex
}

type seekResponse struct {
	Position int64 `json:"position"`
}

type readResponse struct {
	Bytes int64 `json:"bytes"`
}

type sumResponse struct {
	Sha256 string `json:"sha256"`
}

type rigrcpResponse struct {
	Err       error         `json:"-"`
	ErrString string        `json:"error"`
	Stat      *FileInfo     `json:"stat"`
	Dir       []*FileInfo   `json:"dir"`
	Seek      *seekResponse `json:"seek"`
	Read      *readResponse `json:"read"`
	Sum       *sumResponse  `json:"sum"`
}

func (r *rigrcpResponse) UnmarshalJSON(b []byte) error {
	type rigresponse *rigrcpResponse
	rr := rigresponse(r)
	if err := json.Unmarshal(b, rr); err != nil {
		return ErrCommandFailed.Wrapf("failed to unmarshal rigrcp response: %w", err)
	}
	if r.ErrString != "" {
		r.Err = errstring.New(strings.TrimSpace(r.ErrString))
	}
	return nil
}

// newWindowsFsys returns a new fs.FS implementing filesystem for Windows targets
func newWindowsFsys(conn *Connection, opts ...exec.Option) *windowsFsys {
	return &windowsFsys{
		conn: conn,
		buf:  make([]byte, bufSize),
		rcp:  &rigrcp{conn: conn, opts: opts},
	}
}

type rigrcp struct {
	conn    *Connection
	opts    []exec.Option
	mu      sync.Mutex
	done    chan struct{}
	stdin   io.WriteCloser
	stdout  *bufio.Reader
	stderr  io.WriteCloser
	running bool
}

func (rcp *rigrcp) run() error {
	log.Debugf("starting rigrcp")
	rcp.mu.Lock()
	defer rcp.mu.Unlock()

	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()
	rcp.stdout = bufio.NewReader(stdoutR)
	rcp.stdin = stdinW
	rcp.stderr = os.Stderr
	rcp.done = make(chan struct{})

	waiter, err := rcp.conn.ExecStreams(ps.CompressedCmd(rigrcpScript), stdinR, stdoutW, rcp.stderr, rcp.opts...)
	if err != nil {
		return ErrCommandFailed.Wrapf("failed to start rigrcp: %w", err)
	}
	rcp.running = true
	log.Tracef("started rigrcp")

	go func() {
		err := waiter.Wait()
		if err != nil {
			log.Errorf("rigrcp: %v", err)
		}
		log.Debugf("rigrcp exited")
		close(rcp.done)
		rcp.running = false
	}()

	return nil
}

func (rcp *rigrcp) command(cmd string) (rigrcpResponse, error) {
	var res rigrcpResponse
	if !rcp.running {
		if err := rcp.run(); err != nil {
			return res, err
		}
	}
	rcp.mu.Lock()
	defer rcp.mu.Unlock()

	resp := make(chan []byte, 1)
	go func() {
		b, err := rcp.stdout.ReadBytes(0)
		if err != nil {
			log.Errorf("failed to read response: %v", err)
			close(resp)
			return
		}
		resp <- b[:len(b)-1] // drop the zero byte
	}()

	log.Tracef("writing rigrcp command: %s", cmd)
	if _, err := rcp.stdin.Write([]byte(cmd + "\n")); err != nil {
		return res, ErrRcpCommandFailed.Wrap(err)
	}
	select {
	case <-rcp.done:
		return res, ErrRcpCommandFailed.Wrapf("rigrcp exited")
	case data := <-resp:
		if data == nil {
			return res, nil
		}
		if len(data) == 0 {
			return res, nil
		}
		if err := json.Unmarshal(data, &res); err != nil {
			return res, ErrRcpCommandFailed.Wrapf("failed to unmarshal response: %w", err)
		}
		log.Tracef("rigrcp response: %+v", res)
		if res.Err != nil {
			if res.Err.Error() == "eof" {
				return res, io.EOF
			}
			return res, ErrRcpCommandFailed.Wrapf("rigrcp error: %w", res.Err)
		}
		return res, nil
	}
}

// winfsFile is a file on a Windows target. It implements fs.File.
type winfsFile struct {
	fsys *windowsFsys
	path string
}

// Seek sets the offset for the next Read or Write on the remote file.
// The whence argument controls the interpretation of offset.
// 0 = offset from the beginning of file
// 1 = offset from the current position
// 2 = offset from the end of file
func (f *winfsFile) Seek(offset int64, whence int) (int64, error) {
	resp, err := f.fsys.rcp.command(fmt.Sprintf("seek %d %d", offset, whence))
	if err != nil {
		return -1, &fs.PathError{Op: "seek", Path: name, Err: ErrRcpCommandFailed.Wrapf("failed to seek: %w", err)}
	}
	if resp.Seek == nil {
		return -1, &fs.PathError{Op: "seek", Path: name, Err: ErrRcpCommandFailed.Wrapf("invalid response: %v", resp)}
	}
	return resp.Seek.Position, nil
}

// winfsDir is a directory on a Windows target. It implements fs.ReadDirFile.
type winfsDir struct {
	winfsFile
	entries []fs.DirEntry
	hw      int
}

// ReadDir reads the contents of the directory and returns
// a slice of up to n fs.DirEntry values in directory order.
// Subsequent calls on the same file will yield further DirEntry values.
func (d *winfsDir) ReadDir(n int) ([]fs.DirEntry, error) {
	if n == 0 {
		return d.winfsFile.fsys.ReadDir(d.path)
	}
	if d.entries == nil {
		entries, err := d.winfsFile.fsys.ReadDir(d.path)
		if err != nil {
			return nil, err
		}
		d.entries = entries
		d.hw = 0
	}
	if d.hw >= len(d.entries) {
		return nil, io.EOF
	}
	var min int
	if n > len(d.entries)-d.hw {
		min = len(d.entries) - d.hw
	} else {
		min = n
	}
	old := d.hw
	d.hw += min
	return d.entries[old:d.hw], nil
}

// CopyFromN copies n bytes from the reader to the opened file on the target.
// The alt io.Writer parameter can be set to a non nil value if a progress bar or such
// is desired.
func (f *winfsFile) CopyFromN(src io.Reader, num int64, alt io.Writer) (int64, error) {
	_, err := f.fsys.rcp.command(fmt.Sprintf("w %d", num))
	if err != nil {
		return 0, &fs.PathError{Op: "copy-to", Path: f.path, Err: ErrRcpCommandFailed.Wrapf("failed to copy: %w", err)}
	}
	var writer io.Writer
	if alt != nil {
		writer = io.MultiWriter(f.fsys.rcp.stdin, alt)
	} else {
		writer = f.fsys.rcp.stdin
	}
	copied, err := io.CopyN(writer, src, num)
	if err != nil {
		return copied, &fs.PathError{Op: "copy-to", Path: f.path, Err: ErrRcpCommandFailed.Wrapf("error while copying: %w", err)}
	}
	return copied, nil
}

// Copy copies the complete remote file from the current file position to the supplied io.Writer.
func (f *winfsFile) Copy(dst io.Writer) (int, error) {
	resp, err := f.fsys.rcp.command("r -1")
	if errors.Is(err, io.EOF) {
		return 0, io.EOF
	}
	if err != nil {
		return 0, &fs.PathError{Op: "read", Path: f.path, Err: ErrRcpCommandFailed.Wrapf("failed to copy: %w", err)}
	}
	if resp.Read == nil {
		return 0, &fs.PathError{Op: "read", Path: f.path, Err: ErrRcpCommandFailed.Wrapf("invalid response: %v", resp)}
	}
	if resp.Read.Bytes == 0 {
		return 0, io.EOF
	}
	var totalRead int64
	for totalRead < resp.Read.Bytes {
		f.fsys.mu.Lock()
		read, err := f.fsys.rcp.stdout.Read(f.fsys.buf)
		totalRead += int64(read)
		if err != nil {
			f.fsys.mu.Unlock()
			return int(totalRead), &fs.PathError{Op: "read", Path: f.path, Err: ErrRcpCommandFailed.Wrapf("failed to read: %w", err)}
		}
		_, err = dst.Write(f.fsys.buf[:read])
		f.fsys.mu.Unlock()
		if err != nil {
			return int(totalRead), &fs.PathError{Op: "write", Path: f.path, Err: ErrRcpCommandFailed.Wrapf("failed to write: %w", err)}
		}
	}
	return int(totalRead), nil
}

// Write writes len(p) bytes from p to the remote file.
func (f *winfsFile) Write(p []byte) (int, error) {
	_, err := f.fsys.rcp.command(fmt.Sprintf("w %d", len(p)))
	if errors.Is(err, io.EOF) {
		return 0, io.EOF
	}
	if err != nil {
		return 0, &fs.PathError{Op: "write", Path: f.path, Err: ErrRcpCommandFailed.Wrapf("failed to initiate write: %w", err)}
	}
	written, err := f.fsys.rcp.stdin.Write(p)
	if err != nil {
		return written, &fs.PathError{Op: "write", Path: f.path, Err: ErrRcpCommandFailed.Wrapf("write error: %w", err)}
	}
	return written, nil
}

// Read reads up to len(p) bytes from the remote file.
func (f *winfsFile) Read(p []byte) (int, error) {
	resp, err := f.fsys.rcp.command(fmt.Sprintf("r %d", len(p)))
	if errors.Is(err, io.EOF) {
		return 0, io.EOF
	}
	if err != nil {
		return 0, &fs.PathError{Op: "read", Path: f.path, Err: ErrRcpCommandFailed.Wrapf("failed to read: %w", err)}
	}
	if resp.Read == nil {
		return 0, &fs.PathError{Op: "read", Path: f.path, Err: ErrRcpCommandFailed.Wrapf("invalid response: %v", resp)}
	}
	if resp.Read.Bytes == 0 {
		return 0, io.EOF
	}
	var totalRead int64
	for totalRead < resp.Read.Bytes {
		read, err := f.fsys.rcp.stdout.Read(p[totalRead:resp.Read.Bytes])
		totalRead += int64(read)
		if err != nil {
			return int(totalRead), &fs.PathError{Op: "read", Path: f.path, Err: ErrRcpCommandFailed.Wrapf("failed to read: %w", err)}
		}
	}
	return int(totalRead), nil
}

// Stat returns the FileInfo for the remote file.
func (f *winfsFile) Stat() (fs.FileInfo, error) {
	return f.fsys.Stat(f.path)
}

// Close closes the remote file.
func (f *winfsFile) Close() error {
	_, err := f.fsys.rcp.command("c")
	if err != nil {
		return &fs.PathError{Op: "close", Path: f.path, Err: ErrRcpCommandFailed.Wrapf("failed to close: %w", err)}
	}
	return nil
}

// Open opens the named file for reading and returns fs.File.
// Use OpenFile to get a file that can be written to or if you need any of the methods not
// available on fs.File interface without type assertion.
func (fsys *windowsFsys) Open(name string) (fs.File, error) {
	f, err := fsys.OpenFile(name, ModeRead, 0o644)
	if err != nil {
		return nil, err
	}
	return f, nil
}

// OpenFile opens the named remote file with the specified FileMode. perm is ignored on Windows.
func (fsys *windowsFsys) OpenFile(name string, mode FileMode, perm int) (File, error) {
	var modeStr string
	switch mode {
	case ModeRead:
		modeStr = "ro"
	case ModeWrite:
		modeStr = "w"
	case ModeReadWrite:
		modeStr = "rw"
	case ModeAppend:
		modeStr = "a"
	case ModeCreate:
		modeStr = "c"
	default:
		return nil, &fs.PathError{Op: "open", Path: name, Err: ErrRcpCommandFailed.Wrapf("invalid mode: %d", mode)}
	}

	log.Debugf("opening remote file %s (mode %s)", name, modeStr, perm)
	_, err := fsys.rcp.command(fmt.Sprintf("o %s %s", modeStr, filepath.FromSlash(name)))
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return &winfsFile{fsys: fsys, path: name}, nil
}

// Stat returns fs.FileInfo for the remote file.
func (fsys *windowsFsys) Stat(name string) (fs.FileInfo, error) {
	resp, err := fsys.rcp.command(fmt.Sprintf("stat %s", filepath.FromSlash(name)))
	if err != nil {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: ErrRcpCommandFailed.Wrapf("failed to stat: %w", err)}
	}
	if resp.Stat == nil {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: ErrRcpCommandFailed.Wrapf("invalid response: %v", resp)}
	}
	return resp.Stat, nil
}

// Sha256 returns the SHA256 hash of the remote file.
func (fsys *windowsFsys) Sha256(name string) (string, error) {
	resp, err := fsys.rcp.command(fmt.Sprintf("sum %s", filepath.FromSlash(name)))
	if err != nil {
		return "", &fs.PathError{Op: "sum", Path: name, Err: ErrRcpCommandFailed.Wrapf("failed to sum: %w", err)}
	}
	if resp.Sum == nil {
		return "", &fs.PathError{Op: "sum", Path: name, Err: ErrRcpCommandFailed.Wrapf("invalid response: %v", resp)}
	}
	return resp.Sum.Sha256, nil
}

// ReadDir reads the directory named by dirname and returns a list of directory entries.
func (fsys *windowsFsys) ReadDir(name string) ([]fs.DirEntry, error) {
	name = strings.ReplaceAll(name, "/", "\\")
	resp, err := fsys.rcp.command(fmt.Sprintf("dir %s", filepath.FromSlash(name)))
	if err != nil {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: ErrRcpCommandFailed.Wrapf("failed to readdir: %v: %w", err, fs.ErrNotExist)}
	}
	if resp.Dir == nil {
		return nil, nil
	}
	entries := make([]fs.DirEntry, len(resp.Dir))
	for i, entry := range resp.Dir {
		entries[i] = entry
	}
	return entries, nil
}

// Delete removes the named file or (empty) directory.
func (fsys *windowsFsys) Delete(name string) error {
	if err := fsys.conn.Exec(fmt.Sprintf("del %s", ps.DoubleQuote(filepath.FromSlash(name)))); err != nil {
		return ErrCommandFailed.Wrapf("delete %s: %w", name, err)
	}
	return nil
}
