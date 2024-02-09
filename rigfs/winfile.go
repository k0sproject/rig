package rigfs

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"

	"github.com/k0sproject/rig/exec"
	ps "github.com/k0sproject/rig/powershell"
)

//go:embed rigrcp.ps1
var rigrcp string

var (
	_         fs.File = (*winFile)(nil)
	rigRcp            = ps.CompressedCmd(rigrcp)
	errEnded          = errors.New("rigrcp ended")
	errRemote         = errors.New("remote error")
)

type rcpResponse struct {
	Err string `json:"error"`
	N   int64  `json:"n"`
	Pos int64  `json:"pos"`
}

type winFileDirBase struct {
	withPath
	fsys   *WinFsys
	closed bool
}

// Stat returns the FileInfo for the remote file.
func (w *winFileDirBase) Stat() (fs.FileInfo, error) {
	return w.fsys.Stat(w.path)
}

// winFile is a file on a Windows target. It implements fs.File.
type winFile struct {
	winFileDirBase
	stdin  io.WriteCloser
	stdout *bufio.Reader
	done   chan struct{}
	cancel context.CancelFunc
}

// Seek sets the offset for the next Read or Write on the remote file. The whence argument controls the interpretation of offset.
// io.SeekStart = offset from the beginning of file
// io.SeekCurrent = offset from the current position
// io.SeekEnd = offset from the end of file
func (f *winFile) Seek(offset int64, whence int) (int64, error) {
	if f.closed {
		return 0, f.pathErr(OpSeek, fs.ErrClosed)
	}
	var seekOrigin string
	switch whence {
	case io.SeekStart:
		seekOrigin = "Begin"
	case io.SeekCurrent:
		seekOrigin = "Current"
	case io.SeekEnd:
		seekOrigin = "End"
	default:
		return 0, f.pathErr(OpSeek, fmt.Errorf("%w: invalid whence %d", fs.ErrInvalid, whence))
	}
	resp, err := f.command(fmt.Sprintf("s %d %s", offset, seekOrigin))
	if err != nil {
		return 0, f.pathErr(OpSeek, err)
	}
	return resp.Pos, nil
}

// Write writes len(p) bytes from p to the remote file.
func (f *winFile) Write(p []byte) (int, error) {
	if f.closed {
		return 0, f.pathErr(OpWrite, fs.ErrClosed)
	}
	_, err := f.command(fmt.Sprintf("w %d", len(p)))
	if err != nil {
		return 0, f.pathErr(OpWrite, err)
	}
	n, err := f.stdin.Write(p)
	if err != nil {
		return n, err //nolint:wrapcheck
	}
	f.fsys.Log().Tracef("wrote %d bytes", n)
	return n, nil
}

// Read reads up to len(p) bytes from the remote file.
func (f *winFile) Read(p []byte) (int, error) {
	if f.closed {
		return 0, f.pathErr(OpRead, fs.ErrClosed)
	}
	resp, err := f.command(fmt.Sprintf("r %d", len(p)))
	if err != nil {
		return 0, err
	}
	if resp.N == 0 {
		return 0, io.EOF
	}
	total := 0
	for total < int(resp.N) {
		n, err := f.stdout.Read(p[total:resp.N])
		if err != nil {
			return total, err //nolint:wrapcheck
		}
		total += n
	}
	f.fsys.Log().Tracef("read %d bytes", total)
	return total, nil
}

// CopyTo copies the remote file to the provided io.Writer.
func (f *winFile) CopyTo(dst io.Writer) (int64, error) {
	if f.closed {
		return 0, f.pathErr(OpCopyTo, fs.ErrClosed)
	}
	resp, err := f.command("r -1")
	if err != nil {
		return 0, f.pathErr(OpCopyTo, fmt.Errorf("read: %w", err))
	}
	if resp.N == 0 {
		return 0, f.pathErr(OpCopyTo, io.EOF)
	}
	total := int64(0)
	for total < resp.N {
		n, err := io.CopyN(dst, f.stdout, resp.N-total)
		total += n
		if err != nil {
			return total, f.pathErr(OpCopyTo, fmt.Errorf("copy: %w", err))
		}
	}
	return total, nil
}

// CopyFrom copies the provided io.Reader to the remote file.
func (f *winFile) CopyFrom(src io.Reader) (int64, error) {
	n, err := io.Copy(f, src)
	if err != nil {
		return n, f.pathErr(OpCopyFrom, fmt.Errorf("io.copy: %w", err))
	}
	return n, nil
}

func fAccess(flags int) string {
	switch {
	case flags&(os.O_WRONLY|os.O_TRUNC|os.O_APPEND) != 0:
		return "Write"
	case flags&os.O_RDWR != 0:
		return "ReadWrite"
	default:
		return "Read"
	}
}

func fMode(flags int) string {
	switch {
	case flags&os.O_CREATE != 0:
		if flags&os.O_EXCL != 0 {
			return "CreateNew"
		}
		return "OpenOrCreate"
	case flags&os.O_TRUNC != 0:
		return "Truncate"
	case flags&os.O_APPEND != 0:
		return "Append"
	default:
		return "Open"
	}
}

func (f *winFile) open(flags int) error {
	if f.closed {
		return f.pathErr(OpOpen, fs.ErrClosed)
	}

	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()
	stderrR, stderrW := io.Pipe()
	f.stdin = stdinW
	f.stdout = bufio.NewReader(stdoutR)
	f.done = make(chan struct{})

	ctx, cancel := context.WithCancel(context.Background())
	f.cancel = cancel
	cmd, err := f.fsys.Start(ctx, rigRcp, exec.Stdin(stdinR), exec.Stdout(stdoutW), exec.Stderr(stderrW), exec.LogInput(false), exec.HideOutput())
	if err != nil {
		return f.pathErr(OpOpen, fmt.Errorf("start file daemon: %w", err))
	}
	go func() {
		_, _ = io.Copy(io.Discard, stderrR)
	}()
	go func() {
		f.fsys.Log().Debugf("rigrcp started, waiting for exit")
		err := cmd.Wait()
		close(f.done)
		f.fsys.Log().Debugf("rigrcp ended")
		if err != nil {
			f.fsys.Log().Errorf("rigrcp exited with error: %v", err)
		}
		f.closed = true
		_ = stdinR.Close()
		_ = stdinW.Close()
		_ = stdoutR.Close()
		_ = stdoutW.Close()
		_ = stderrR.Close()
		_ = stderrW.Close()
	}()

	resp, err := f.command(fmt.Sprintf("o %s %s %s", fMode(flags), fAccess(flags), f.path))
	if err != nil {
		cancel()
		return f.pathErr(OpOpen, err)
	}
	if resp.Err != "" {
		cancel()
		return f.pathErr(OpOpen, fmt.Errorf("remote error: %s", resp.Err)) //nolint:goerr113
	}

	return nil
}

func (f *winFile) command(cmd string) (*rcpResponse, error) { //nolint:cyclop
	if f.closed {
		return nil, f.pathErr(OpOpen, fs.ErrClosed)
	}
	resp := make(chan []byte, 1)
	if cmd != "q" {
		go func() {
			b, err := f.stdout.ReadBytes(0)
			if err != nil {
				f.fsys.Log().Errorf("failed to read response: %v", err)
				close(resp)
				return
			}
			resp <- b[:len(b)-1] // drop the zero byte
		}()
	}
	f.fsys.Log().Debugf("rigrcp command: %s", cmd)
	_, err := fmt.Fprintf(f.stdin, "%s\n", cmd)
	if err != nil {
		return nil, f.pathErr(OpOpen, fmt.Errorf("write command: %w", err))
	}
	if cmd == "q" {
		return &rcpResponse{}, nil
	}
	select {
	case <-f.done:
		return nil, errEnded
	case data, ok := <-resp:
		out := &rcpResponse{}
		if !ok || data == nil || len(data) == 0 {
			return out, nil
		}
		if err := json.Unmarshal(data, out); err != nil {
			return nil, fmt.Errorf("failed to unmarshal response: %w", err)
		}
		if e := out.Err; e != "" {
			if strings.HasPrefix(e, "eof") {
				return nil, io.EOF
			}
			if strings.Contains(e, "does not exist") {
				return nil, fs.ErrNotExist
			}
			return nil, fmt.Errorf("%w: %s", errRemote, e)
		}
		return out, nil
	}
}

func (f *winFile) Close() error {
	defer f.cancel()
	resp, err := f.command("c")
	if err != nil {
		return f.pathErr(OpClose, err)
	}
	if resp.Err != "" {
		return f.pathErr(OpClose, fmt.Errorf("%w: %s", errRemote, resp.Err))
	}
	if resp.Pos != -1 {
		return f.pathErr(OpClose, fmt.Errorf("%w: failed to close file", errRemote))
	}
	_, err = f.command("q")
	f.fsys.Log().Tracef("rigrcp quit: %v", err)
	f.stdin.Close()
	f.closed = true

	return nil
}
