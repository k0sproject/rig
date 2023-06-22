package rigfs

import (
	"bufio"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"sync"

	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/log"
	ps "github.com/k0sproject/rig/pkg/powershell"
)

const bufSize = 32768

var (
	// ErrNotRunning is returned when the rigrcp process is not running
	ErrNotRunning = errors.New("rigrcp is not running")
	// ErrRcpCommandFailed is returned when a command to the rigrcp process fails
	ErrRcpCommandFailed = errors.New("rigrcp command failed")
)

// rigWinRCPScript is a helper script for transferring files between local and remote systems
//
//go:embed rigrcp.ps1
var rigWinRCPScript string

var (
	_ fs.File        = (*winFile)(nil)
	_ fs.ReadDirFile = (*winDir)(nil)
	_ fs.FS          = (*WinFsys)(nil)
)

// WinFsys is a fs.FS implementation for remote Windows hosts
type WinFsys struct {
	conn connection
	rcp  *winRCP
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
		return fmt.Errorf("%w: failed to unmarshal rigrcp response: %w", ErrCommandFailed, err)
	}
	if r.ErrString != "" {
		r.Err = fmt.Errorf("%w: %s", ErrCommandFailed, strings.TrimSpace(r.ErrString))
	}
	return nil
}

// NewWindowsFsys returns a new fs.FS implementing filesystem for Windows targets
func NewWindowsFsys(conn connection, opts ...exec.Option) *WinFsys {
	return &WinFsys{
		conn: conn,
		buf:  make([]byte, bufSize),
		rcp:  &winRCP{conn: conn, opts: opts},
	}
}

type winRCP struct {
	conn    connection
	opts    []exec.Option
	mu      sync.Mutex
	done    chan struct{}
	stdin   io.WriteCloser
	stdout  *bufio.Reader
	stderr  io.WriteCloser
	running bool
}

func (rcp *winRCP) run() error {
	log.Debugf("starting rigrcp")
	rcp.mu.Lock()
	defer rcp.mu.Unlock()

	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()
	rcp.stdout = bufio.NewReader(stdoutR)
	rcp.stdin = stdinW
	rcp.stderr = os.Stderr
	rcp.done = make(chan struct{})

	waiter, err := rcp.conn.ExecStreams(ps.CompressedCmd(rigWinRCPScript), stdinR, stdoutW, rcp.stderr, rcp.opts...)
	if err != nil {
		return fmt.Errorf("%w: failed to start rigrcp: %w", ErrCommandFailed, err)
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

func (rcp *winRCP) command(cmd string) (rigrcpResponse, error) {
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
		return res, fmt.Errorf("%w: %w", ErrRcpCommandFailed, err)
	}
	select {
	case <-rcp.done:
		return res, fmt.Errorf("%w: rigrcp exited", ErrRcpCommandFailed)
	case data := <-resp:
		if data == nil {
			return res, nil
		}
		if len(data) == 0 {
			return res, nil
		}
		if err := json.Unmarshal(data, &res); err != nil {
			return res, fmt.Errorf("%w: failed to unmarshal response: %w", ErrRcpCommandFailed, err)
		}
		log.Tracef("rigrcp response: %+v", res)
		if res.Err != nil {
			if res.Err.Error() == "eof" {
				return res, io.EOF
			}
			return res, fmt.Errorf("%w: %w", ErrRcpCommandFailed, res.Err)
		}
		return res, nil
	}
}

// winFile is a file on a Windows target. It implements fs.File.
type winFile struct {
	fsys *WinFsys
	path string
}

// Seek sets the offset for the next Read or Write on the remote file.
// The whence argument controls the interpretation of offset.
// 0 = offset from the beginning of file
// 1 = offset from the current position
// 2 = offset from the end of file
func (f *winFile) Seek(offset int64, whence int) (int64, error) {
	resp, err := f.fsys.rcp.command(fmt.Sprintf("seek %d %d", offset, whence))
	if err != nil {
		return -1, &fs.PathError{Op: "seek", Path: f.path, Err: fmt.Errorf("%w: seek: %w", ErrRcpCommandFailed, err)}
	}
	if resp.Seek == nil {
		return -1, &fs.PathError{Op: "seek", Path: f.path, Err: fmt.Errorf("%w: seek response: %v", ErrRcpCommandFailed, resp)}
	}
	return resp.Seek.Position, nil
}

// winDir is a directory on a Windows target. It implements fs.ReadDirFile.
type winDir struct {
	winFile
	entries []fs.DirEntry
	hw      int
}

// ReadDir reads the contents of the directory and returns
// a slice of up to n fs.DirEntry values in directory order.
// Subsequent calls on the same file will yield further DirEntry values.
func (d *winDir) ReadDir(n int) ([]fs.DirEntry, error) {
	if n == 0 {
		return d.winFile.fsys.ReadDir(d.path)
	}
	if d.entries == nil {
		entries, err := d.winFile.fsys.ReadDir(d.path)
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
func (f *winFile) CopyFromN(src io.Reader, num int64, alt io.Writer) (int64, error) {
	_, err := f.fsys.rcp.command(fmt.Sprintf("w %d", num))
	if err != nil {
		return 0, &fs.PathError{Op: "copy-to", Path: f.path, Err: fmt.Errorf("%w: copy: %w", ErrRcpCommandFailed, err)}
	}
	var writer io.Writer
	if alt != nil {
		writer = io.MultiWriter(f.fsys.rcp.stdin, alt)
	} else {
		writer = f.fsys.rcp.stdin
	}
	copied, err := io.CopyN(writer, src, num)
	if err != nil {
		return copied, &fs.PathError{Op: "copy-to", Path: f.path, Err: fmt.Errorf("%w: copy stream: %w", ErrRcpCommandFailed, err)}
	}
	return copied, nil
}

// Copy copies the complete remote file from the current file position to the supplied io.Writer.
func (f *winFile) Copy(dst io.Writer) (int64, error) {
	resp, err := f.fsys.rcp.command("r -1")
	if errors.Is(err, io.EOF) {
		return 0, io.EOF
	}
	if err != nil {
		return 0, &fs.PathError{Op: "read", Path: f.path, Err: fmt.Errorf("%w: copy: %w", ErrRcpCommandFailed, err)}
	}
	if resp.Read == nil {
		return 0, &fs.PathError{Op: "read", Path: f.path, Err: fmt.Errorf("%w: copy response: %v", ErrCommandFailed, resp)}
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
			return totalRead, &fs.PathError{Op: "read", Path: f.path, Err: fmt.Errorf("%w: copy (read): %w", ErrRcpCommandFailed, err)}
		}
		_, err = dst.Write(f.fsys.buf[:read])
		f.fsys.mu.Unlock()
		if err != nil {
			return totalRead, &fs.PathError{Op: "write", Path: f.path, Err: fmt.Errorf("%w: copy (write): %w", ErrRcpCommandFailed, err)}
		}
	}
	return totalRead, nil
}

// Write writes len(p) bytes from p to the remote file.
func (f *winFile) Write(p []byte) (int, error) {
	_, err := f.fsys.rcp.command(fmt.Sprintf("w %d", len(p)))
	if errors.Is(err, io.EOF) {
		return 0, io.EOF
	}
	if err != nil {
		return 0, &fs.PathError{Op: "write", Path: f.path, Err: fmt.Errorf("%w: initiate write: %w", ErrRcpCommandFailed, err)}
	}
	written, err := f.fsys.rcp.stdin.Write(p)
	if err != nil {
		return written, &fs.PathError{Op: "write", Path: f.path, Err: fmt.Errorf("%w: write error: %w", ErrRcpCommandFailed, err)}
	}
	return written, nil
}

// Read reads up to len(p) bytes from the remote file.
func (f *winFile) Read(p []byte) (int, error) {
	resp, err := f.fsys.rcp.command(fmt.Sprintf("r %d", len(p)))
	if errors.Is(err, io.EOF) {
		return 0, io.EOF
	}
	if err != nil {
		return 0, &fs.PathError{Op: "read", Path: f.path, Err: fmt.Errorf("%w: read: %w", ErrRcpCommandFailed, err)}
	}
	if resp.Read == nil {
		return 0, &fs.PathError{Op: "read", Path: f.path, Err: fmt.Errorf("%w: read response: %v", ErrRcpCommandFailed, resp)}
	}
	if resp.Read.Bytes == 0 {
		return 0, io.EOF
	}
	var totalRead int64
	for totalRead < resp.Read.Bytes {
		read, err := f.fsys.rcp.stdout.Read(p[totalRead:resp.Read.Bytes])
		totalRead += int64(read)
		if err != nil {
			return int(totalRead), &fs.PathError{Op: "read", Path: f.path, Err: fmt.Errorf("%w: read: %w", ErrRcpCommandFailed, err)}
		}
	}
	return int(totalRead), nil
}

// Stat returns the FileInfo for the remote file.
func (f *winFile) Stat() (fs.FileInfo, error) {
	return f.fsys.Stat(f.path)
}

// Close closes the remote file.
func (f *winFile) Close() error {
	_, err := f.fsys.rcp.command("c")
	if err != nil {
		return &fs.PathError{Op: "close", Path: f.path, Err: fmt.Errorf("%w: close: %w", ErrRcpCommandFailed, err)}
	}
	return nil
}

// Open opens the named file for reading and returns fs.File.
// Use OpenFile to get a file that can be written to or if you need any of the methods not
// available on fs.File interface without type assertion.
func (fsys *WinFsys) Open(name string) (fs.File, error) {
	f, err := fsys.OpenFile(name, ModeRead, 0o644)
	if err != nil {
		return nil, err
	}
	return f, nil
}

// OpenFile opens the named remote file with the specified FileMode. Permission bits are ignored on Windows.
func (fsys *WinFsys) OpenFile(name string, mode FileMode, _ FileMode) (File, error) {
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
		return nil, &fs.PathError{Op: "open", Path: name, Err: fmt.Errorf("%w: invalid mode: %d", ErrRcpCommandFailed, mode)}
	}

	log.Debugf("opening remote file %s (mode %s)", name, modeStr)
	_, err := fsys.rcp.command(fmt.Sprintf("o %s %s", modeStr, fsys.formatPath(name)))
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return &winFile{fsys: fsys, path: name}, nil
}

// Stat returns fs.FileInfo for the remote file.
func (fsys *WinFsys) Stat(name string) (fs.FileInfo, error) {
	resp, err := fsys.rcp.command(fmt.Sprintf("stat %s", fsys.formatPath(name)))
	if err != nil {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: fmt.Errorf("%w: stat %s: %w", ErrRcpCommandFailed, name, err)}
	}
	if resp.Stat == nil {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: fmt.Errorf("%w: stat response: %v", ErrRcpCommandFailed, resp)}
	}
	return resp.Stat, nil
}

// Sha256 returns the SHA256 hash of the remote file.
func (fsys *WinFsys) Sha256(name string) (string, error) {
	resp, err := fsys.rcp.command(fmt.Sprintf("sum %s", fsys.formatPath(name)))
	if err != nil {
		return "", &fs.PathError{Op: "sum", Path: name, Err: fmt.Errorf("%w: sha256sum: %w", ErrRcpCommandFailed, err)}
	}
	if resp.Sum == nil {
		return "", &fs.PathError{Op: "sum", Path: name, Err: fmt.Errorf("%w: sha256sum response: %v", ErrRcpCommandFailed, resp)}
	}
	return resp.Sum.Sha256, nil
}

// ReadDir reads the directory named by dirname and returns a list of directory entries.
func (fsys *WinFsys) ReadDir(name string) ([]fs.DirEntry, error) {
	name = strings.ReplaceAll(name, "/", "\\")
	resp, err := fsys.rcp.command(fmt.Sprintf("dir %s", fsys.formatPath(name)))
	if err != nil {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fmt.Errorf("%w: readdir: %w: %w", ErrRcpCommandFailed, err, fs.ErrNotExist)}
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

// Remove deletes the named file or (empty) directory.
func (fsys *WinFsys) Remove(name string) error {
	if existing, err := fsys.Stat(name); err == nil && existing.IsDir() {
		return fsys.removeDir(name)
	}

	if err := fsys.conn.Exec(fmt.Sprintf("del %s", fsys.formatPath(name))); err != nil {
		return fmt.Errorf("%w: remove %s: %w", ErrCommandFailed, name, err)
	}
	return nil
}

// RemoveAll deletes the named file or directory and all its child items
func (fsys *WinFsys) RemoveAll(name string) error {
	if existing, err := fsys.Stat(name); err == nil && existing.IsDir() {
		return fsys.removeDirAll(name)
	}

	if err := fsys.conn.Exec(fmt.Sprintf("del %s", fsys.formatPath(name))); err != nil {
		return fmt.Errorf("%w: remove all %s: %w", ErrCommandFailed, name, err)
	}
	return nil
}

func (fsys *WinFsys) removeDir(name string) error {
	if err := fsys.conn.Exec(fmt.Sprintf("rmdir /q %s", fsys.formatPath(name))); err != nil {
		return fmt.Errorf("%w: rmdir %s: %w", ErrCommandFailed, name, err)
	}
	return nil
}

func (fsys *WinFsys) removeDirAll(name string) error {
	if err := fsys.conn.Exec(fmt.Sprintf("rmdir /s /q %s", fsys.formatPath(name))); err != nil {
		return fmt.Errorf("%w: rmdir %s: %w", ErrCommandFailed, name, err)
	}
	return nil
}

// MkDirAll creates a directory named path, along with any necessary parents. The permission bits perm are ignored on Windows.
func (fsys *WinFsys) MkDirAll(path string, _ FileMode) error {
	if err := fsys.conn.Exec(fmt.Sprintf("mkdir -p %s", fsys.formatPath(path))); err != nil {
		return fmt.Errorf("%w: mkdir %s: %w", ErrCommandFailed, path, err)
	}

	return nil
}

// formatPath takes a path, either in windows\format or unix/format and returns a windows\format path.
func (fsys *WinFsys) formatPath(path string) string {
	parts := strings.FieldsFunc(path, func(c rune) bool {
		return c == '\\' || c == '/'
	})

	normalized := strings.Join(parts, "\\")

	if strings.HasPrefix(path, "\\\\") || strings.HasPrefix(path, "//") {
		normalized = "\\\\" + normalized
	} else if strings.HasPrefix(path, "\\") || strings.HasPrefix(path, "/") {
		normalized = "\\" + normalized
	}

	if strings.HasSuffix(path, "\\") || strings.HasSuffix(path, "/") {
		normalized = normalized + "\\"
	}

	if strings.Contains(normalized, " ") {
		return ps.DoubleQuote(normalized)
	}

	return normalized
}
