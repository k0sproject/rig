package remotefs

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
	"sync"
	"sync/atomic"
	"time"

	"github.com/k0sproject/rig/v2/cmd"
	"github.com/k0sproject/rig/v2/log"
	ps "github.com/k0sproject/rig/v2/powershell"
)

//go:embed rigrcp.ps1
var rigrcp string

var (
	_              fs.File = (*winFile)(nil)
	rigRcp                 = ps.CompressedCmd(rigrcp)
	errEnded               = errors.New("rigrcp ended")
	errRemote              = errors.New("remote error")
	errNotReadable         = fmt.Errorf("%w: file is not open for reading", fs.ErrClosed)
	errNotWritable         = fmt.Errorf("%w: file is not open for writing", fs.ErrClosed)
)

// ErrTimeout is returned by operations on a file on a Windows host when the
// rigrcp session backing the file stops making progress for 30 seconds. That
// means the session has died -- typically the host went away mid-transfer
// without the shell ever being torn down on this end -- so the session is
// aborted when it happens: the remote helper is stopped and further
// operations on the file fail too. It wraps context.DeadlineExceeded, so a
// caller that does not want to depend on this sentinel can match that
// instead.
var ErrTimeout = fmt.Errorf("rigrcp session stalled: %w", context.DeadlineExceeded)

type rcpResponse struct {
	Err  string `json:"error"`
	N    int64  `json:"n"`
	Pos  int64  `json:"pos"`
	Size int64  `json:"size"`
}

type winFileDirBase struct {
	withPath
	fs *WinFS
	// closed is atomic because a winFile is marked closed from the goroutine
	// watching the rigrcp command exit and from the watchdog that aborts a
	// stalled session, neither of which runs on the caller's goroutine.
	closed atomic.Bool
}

// Stat returns the FileInfo for the remote file.
func (w *winFileDirBase) Stat() (fs.FileInfo, error) {
	return w.fs.Stat(w.path)
}

// winFile is a file on a Windows target. It implements fs.File.
type winFile struct {
	winFileDirBase
	// pos and size are tracked here rather than asked of the helper: every
	// read and write carries the offset it applies to, so Seek needs no
	// round trip. size is the larger of the file's size when it was opened
	// and the furthest offset this handle has written to.
	pos  int64
	size int64
	// appending sends every write to the end of the file, as O_APPEND does
	// for an os.File, wherever Seek last left the position. The file is not
	// opened at its end, so reading still starts at the beginning.
	appending bool
	// readable and writable follow the access the caller asked for, not the
	// wider access fAccess may open the file with on the host, so a
	// disallowed operation fails here, whatever its size, as it does for
	// PosixFile.
	readable bool
	writable bool
	stdin    io.WriteCloser
	stdout   *bufio.Reader
	// stdinR and stdoutW are the far ends of the pipes behind stdin and
	// stdout, the ones the rigrcp command itself holds. io.Pipe reports a
	// CloseWithError to the opposite end, so closing these is what releases
	// a write to stdin or a read from stdout parked on a dead session.
	stdinR  *io.PipeReader
	stdoutW *io.PipeWriter
	done    chan struct{}
	cancel  context.CancelFunc
	// abortErr records why the session was torn down, if it was. Closing the
	// pipes is what releases a parked transfer, but io.Pipe keeps the first
	// close error it is given, so a transfer that raced the rigrcp command
	// exiting comes back with a plain closed-pipe error instead of the
	// timeout that actually ended it. This is the authority on which it was.
	abortErr atomic.Pointer[error]
}

// abortCause reports why the session ended, preferring the reason it was
// aborted for over whatever the pipes had to say about it.
func (f *winFile) abortCause(err error) error {
	if reason := f.abortErr.Load(); reason != nil {
		return *reason
	}
	return err
}

// abort tears down a rigrcp session that can no longer make progress. It
// releases everything parked on the pipes with err -- the response reader,
// a payload write in Write, a payload read in Read or CopyTo -- and cancels
// the command so the remote helper is not left running behind a file that
// can no longer be used. Safe to call repeatedly and from any goroutine.
func (f *winFile) abort(err error) {
	f.closed.Store(true)
	f.abortErr.CompareAndSwap(nil, &err) // the first reason is the real one
	if f.stdinR != nil {
		_ = f.stdinR.CloseWithError(err)
	}
	if f.stdoutW != nil {
		_ = f.stdoutW.CloseWithError(err)
	}
	if f.cancel != nil {
		f.cancel()
	}
}

// watchdog aborts a winFile session that has gone commandTimeout without
// progress. Everything winFile does on the pipes is unbounded on a dead
// session, and the abort is also what unblocks it: the parked read or write
// returns the abort error instead of never returning at all.
//
// last, not the timer, is what decides: time.Timer.Stop and Reset have no
// effect on an AfterFunc callback that has already begun, and that callback
// can sit waiting for mu while the operation it covers finishes or moves on.
// It therefore re-reads how long it has really been since progress, and
// re-arms rather than tearing down a session that is no longer stalled.
type watchdog struct {
	mu    sync.Mutex
	done  bool
	last  time.Time
	timer *time.Timer
}

// watch starts a watchdog covering what. The caller must stop it once the
// operation it covers has finished.
func (f *winFile) watch(what string) *watchdog {
	dog := &watchdog{last: time.Now()}

	// Hold mu while arming: with a very short commandTimeout the callback can
	// start before AfterFunc has even returned the timer it reads.
	dog.mu.Lock()
	defer dog.mu.Unlock()

	dog.timer = time.AfterFunc(commandTimeout, func() {
		dog.mu.Lock()
		defer dog.mu.Unlock()

		if dog.done {
			return
		}
		if remaining := commandTimeout - time.Since(dog.last); remaining > 0 {
			dog.timer.Reset(remaining) // progress arrived while this was waiting for mu
			return
		}
		dog.done = true
		f.abort(fmt.Errorf("%w: %s", ErrTimeout, what))
	})
	return dog
}

// progress restarts the countdown. The bound is on making no progress at
// all rather than on total duration, because a large transfer legitimately
// takes longer than commandTimeout.
func (w *watchdog) progress() {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.recordProgress()
}

// recordProgress registers a step forward. It must be called with mu held.
func (w *watchdog) recordProgress() {
	if w.done {
		return
	}
	w.last = time.Now()
	w.timer.Reset(commandTimeout)
}

// stop disarms the watchdog. Once it returns, the session will not be
// aborted on this watchdog's account.
func (w *watchdog) stop() {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.done = true
	w.timer.Stop()
}

// payloadChunkSize is how much of a Write payload is handed to the pipe at
// a time, so that progress is reported often enough for the watchdog to
// bound a stall rather than the transfer. It matches the buffer io.Copy
// uses to drain the pipe on the other side, so the chunking costs nothing:
// one chunk is one read there and one request to the host, and a smaller
// value would multiply those requests.
//
// That request is therefore the unit of progress on this path, and the floor
// a write has to clear, as commandTimeout describes. A link that cannot
// manage a chunk in that time has already lost the transfer anyway: the
// WinRM HTTP client gives a single request a minute.
const payloadChunkSize = 32 * 1024

// watchedReader reports read progress to a watchdog.
type watchedReader struct {
	reader io.Reader
	dog    *watchdog
}

func (w watchedReader) Read(p []byte) (int, error) {
	n, err := w.reader.Read(p)
	if n > 0 {
		w.dog.progress()
	}
	return n, err //nolint:wrapcheck // transparent pass-through of the underlying reader
}

// Seek sets the offset for the next Read or Write on the remote file. The whence argument controls the interpretation of offset.
// io.SeekStart = offset from the beginning of file
// io.SeekCurrent = offset from the current position
// io.SeekEnd = offset from the end of file.
//
// Seek does not contact the host. The helper holds the file open without
// sharing, so nothing else can change it meanwhile: its end is its size
// when it was opened, or the furthest offset written since if that is
// further.
func (f *winFile) Seek(offset int64, whence int) (int64, error) {
	if f.closed.Load() {
		return 0, f.pathErr(OpSeek, fs.ErrClosed)
	}
	var base int64
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		base = f.pos
	case io.SeekEnd:
		base = f.size
	default:
		return 0, f.pathErr(OpSeek, fmt.Errorf("%w: invalid whence %d", fs.ErrInvalid, whence))
	}
	pos := base + offset
	if pos < 0 {
		return 0, f.pathErr(OpSeek, fmt.Errorf("%w: negative position %d", fs.ErrInvalid, pos))
	}
	f.pos = pos
	return pos, nil
}

// writeCommandSize is the most payload one w command carries; a larger
// Write goes out as several. The helper reads a command's whole payload
// into memory before writing it, and takes its length as a PowerShell
// [int], so this bounds both. It is also how much CopyFrom reads at a time,
// so an upload costs one command and one completion per this many bytes.
const writeCommandSize = 4 << 20

// Write writes len(p) bytes from p to the remote file.
func (f *winFile) Write(p []byte) (int, error) {
	if f.closed.Load() {
		return 0, f.pathErr(OpWrite, fs.ErrClosed)
	}
	if !f.writable {
		return 0, f.pathErr(OpWrite, errNotWritable)
	}
	written := 0
	for written < len(p) {
		n, err := f.writeCommand(p[written:min(written+writeCommandSize, len(p))])
		written += n
		if err != nil {
			return written, err
		}
	}
	return written, nil
}

// writeCommand sends p to the helper as one w command and waits for it to
// reach the file. p must not be empty: writing nothing must not move the
// end, and at a position past it the size bookkeeping below would grow the
// file on paper only.
func (f *winFile) writeCommand(p []byte) (int, error) {
	pos := f.pos
	if f.appending {
		pos = f.size
	}
	_, err := f.command(fmt.Sprintf("w %d %d", len(p), pos))
	if err != nil {
		return 0, f.pathErr(OpWrite, err)
	}
	// The payload goes down the same pipe the command did and is just as
	// unbounded: on a dead session nothing downstream ever reads it. It can
	// be up to writeCommandSize, so it goes in chunks, letting the watchdog
	// tell a stalled write from a large one.
	dog := f.watch(fmt.Sprintf("writing %d byte payload", len(p)))
	defer dog.stop()
	written := 0
	for written < len(p) {
		n, err := f.stdin.Write(p[written:min(written+payloadChunkSize, len(p))])
		written += n
		if err != nil {
			return written, f.pathErr(OpWrite, f.abortCause(err))
		}
		dog.progress()
	}
	dog.stop()
	// The reply to "w" only said the helper was ready for the payload. This
	// one says the payload reached the file, or why it did not.
	if _, err := f.awaitResponse(f.readResponse(), fmt.Sprintf("completion of %d byte write", len(p))); err != nil {
		// A failed FileStream.Write is not all or nothing, so how much of
		// the payload reached the file, and where that left its end, is
		// unknown. Nothing further can be trusted to land where asked.
		f.abort(err)
		return 0, f.pathErr(OpWrite, err)
	}
	f.pos = pos + int64(written)
	f.size = max(f.size, f.pos)
	return written, nil
}

// Read reads up to len(p) bytes from the remote file.
func (f *winFile) Read(p []byte) (int, error) {
	if f.closed.Load() {
		return 0, f.pathErr(OpRead, fs.ErrClosed)
	}
	if !f.readable {
		return 0, f.pathErr(OpRead, errNotReadable)
	}
	resp, err := f.command(fmt.Sprintf("r %d %d", len(p), f.pos))
	if err != nil {
		if errors.Is(err, io.EOF) {
			return 0, io.EOF // io.Copy tests for io.EOF by identity, so it must not be wrapped
		}
		return 0, f.pathErr(OpRead, err)
	}
	if resp.N == 0 {
		return 0, io.EOF
	}
	dog := f.watch(fmt.Sprintf("reading %d byte response", resp.N))
	defer dog.stop()
	src := watchedReader{reader: f.stdout, dog: dog}
	total := 0
	for total < int(resp.N) {
		n, err := src.Read(p[total:resp.N])
		total += n
		f.pos += int64(n)
		if err != nil {
			err = f.abortCause(err)
			f.abort(err) // the rest of the payload would be read as the next reply
			return total, f.pathErr(OpRead, err)
		}
	}
	return total, nil
}

// CopyTo copies the remote file to the provided io.Writer.
//
// commandTimeout bounds the remote side of this, not dst: a destination that
// blocks forever blocks CopyTo for just as long, since there is no way to
// interrupt a write to an arbitrary io.Writer. Aborting the session releases
// everything this end is waiting on, never the caller's own sink.
func (f *winFile) CopyTo(dst io.Writer) (int64, error) {
	if f.closed.Load() {
		return 0, f.pathErr(OpCopyTo, fs.ErrClosed)
	}
	if !f.readable {
		return 0, f.pathErr(OpCopyTo, errNotReadable)
	}
	resp, err := f.command(fmt.Sprintf("r -1 %d", f.pos))
	if err != nil {
		return 0, f.pathErr(OpCopyTo, fmt.Errorf("read: %w", err))
	}
	if resp.N == 0 {
		return 0, nil
	}
	dog := f.watch(fmt.Sprintf("copying %d bytes", resp.N))
	defer dog.stop()
	src := watchedReader{reader: f.stdout, dog: dog}
	total := int64(0)
	for total < resp.N {
		n, err := io.CopyN(dst, src, resp.N-total)
		total += n
		f.pos += n
		if err != nil {
			// Whether dst refused the data or the stream broke, the rest of
			// the payload is still on its way and would be read as the next
			// command's reply, so the session cannot be used again.
			err = f.abortCause(err)
			f.abort(err)
			return total, f.pathErr(OpCopyTo, fmt.Errorf("copy: %w", err))
		}
	}
	return total, nil
}

// CopyFrom copies the provided io.Reader to the remote file. It reads src in
// batches of up to writeCommandSize, so data reaches the file each time a
// batch fills or src ends, not after every Read src returns.
func (f *winFile) CopyFrom(src io.Reader) (int64, error) {
	if f.closed.Load() {
		return 0, f.pathErr(OpCopyFrom, fs.ErrClosed)
	}
	if !f.writable {
		return 0, f.pathErr(OpCopyFrom, errNotWritable)
	}
	// Not io.Copy: its 32 KiB buffer would make every 32 KiB a command and
	// a completion of its own. Nor src's io.WriterTo, where it has one: an
	// *os.File's writes 32 KiB at a time too.
	buf := make([]byte, writeCommandSize)
	var total int64
	for {
		n, readErr := fill(src, buf)
		if n > 0 {
			written, err := f.Write(buf[:n])
			total += int64(written)
			if err != nil {
				return total, f.pathErr(OpCopyFrom, fmt.Errorf("write: %w", err))
			}
		}
		if errors.Is(readErr, io.EOF) {
			return total, nil
		}
		if readErr != nil {
			return total, f.pathErr(OpCopyFrom, fmt.Errorf("read: %w", readErr))
		}
	}
}

// fill reads from src until buf is full or src returns an error, which,
// unlike from io.ReadFull, is src's own.
func fill(src io.Reader, buf []byte) (int, error) {
	n := 0
	for n < len(buf) {
		m, err := src.Read(buf[n:])
		n += m
		if err != nil {
			return n, err //nolint:wrapcheck // the caller wraps it
		}
	}
	return n, nil
}

// .NET FileAccess values.
const (
	accessRead      = "Read"
	accessWrite     = "Write"
	accessReadWrite = "ReadWrite"
)

// fAccess maps flags to a .NET FileAccess. .NET refuses read-only access
// with the modes that truncate or create exclusively, and appending needs
// write access, so a read-only open that asks for any of those gets
// ReadWrite: it can still read, as it asked to.
func fAccess(flags int) string {
	needsWrite := flags&os.O_APPEND != 0
	switch fMode(flags) {
	case "CreateNew", "Create", "Truncate":
		needsWrite = true
	}
	switch {
	case flags&os.O_WRONLY != 0:
		return accessWrite
	case flags&os.O_RDWR != 0 || needsWrite:
		return accessReadWrite
	default:
		return accessRead
	}
}

// fMode maps flags to a .NET FileMode. O_APPEND is not FileMode.Append,
// which creates a missing file and refuses read access; Write sends each
// write to the end of the file instead.
func fMode(flags int) string {
	switch {
	case flags&(os.O_CREATE|os.O_EXCL) == os.O_CREATE|os.O_EXCL:
		return "CreateNew"
	case flags&(os.O_CREATE|os.O_TRUNC) == os.O_CREATE|os.O_TRUNC:
		return "Create"
	case flags&os.O_CREATE != 0:
		return "OpenOrCreate"
	case flags&os.O_TRUNC != 0:
		return "Truncate"
	default:
		return "Open"
	}
}

func (f *winFile) open(flags int) error {
	if f.closed.Load() {
		return f.pathErr(OpOpen, fs.ErrClosed)
	}

	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()
	stderrR, stderrW := io.Pipe()
	f.stdin = stdinW
	f.stdout = bufio.NewReader(stdoutR)
	f.stdinR = stdinR
	f.stdoutW = stdoutW
	f.done = make(chan struct{})

	ctx, cancel := context.WithCancel(context.Background())
	f.cancel = cancel
	cmd, err := f.fs.Start(ctx, rigRcp, cmd.Stdin(stdinR), cmd.Stdout(stdoutW), cmd.Stderr(stderrW), cmd.LogInput(false), cmd.HideOutput())
	if err != nil {
		return f.pathErr(OpOpen, fmt.Errorf("start file daemon: %w", err))
	}
	go func() {
		_, _ = io.Copy(io.Discard, stderrR)
	}()
	go func() {
		log.Trace(ctx, "rigrcp started")
		err := cmd.Wait()
		log.Trace(ctx, "rigrcp exited", log.KeyError, err)
		close(f.done)
		f.closed.Store(true)
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
		return f.pathErr(OpOpen, fmt.Errorf("remote error: %s", resp.Err)) //nolint:err113
	}
	f.pos = resp.Pos
	f.size = resp.Size
	f.appending = flags&os.O_APPEND != 0
	accessMode := flags & (os.O_WRONLY | os.O_RDWR)
	f.readable = accessMode != os.O_WRONLY
	f.writable = accessMode != os.O_RDONLY

	return nil
}

// commandTimeout bounds how long a rigrcp session may go without making
// progress. It is not a transfer-duration budget: a large file legitimately
// takes much longer than this, so the timer restarts on every step forward.
//
// What counts as a step depends on the direction, because that is where the
// session can be observed. A protocol round-trip completing is one. So is
// each successful read while a payload comes back, which is as fine grained
// as the reads themselves. A payload going out, though, is only visible
// once a whole chunk has been handed to the connection -- io.PipeWriter.Write
// does not return before that -- so on that side the bound is one
// payloadChunkSize per commandTimeout, a floor of roughly a kilobyte a
// second, rather than anything finer.
//
// It exists so that a WinRM session
// that has silently died (the host rebooted mid-transfer and the shell was
// never actually torn down on this end) fails the operation instead of
// blocking forever: nothing else bounds these waits, and f.done only closes
// once cmd.Wait() returns, which itself never returns on a dead connection
// with no deadline. A var, not a const, so tests can lower it instead of
// waiting out the real default.
var commandTimeout = 30 * time.Second

// timedOut aborts the session and returns the error to report for a
// round-trip that ran out of time.
func (f *winFile) timedOut(what string) error {
	err := fmt.Errorf("%w: %s", ErrTimeout, what)
	f.abort(err)
	return err
}

// command runs one rigrcp round-trip. Its errors carry no fs.PathError of
// their own: the public operation that issued the command is the one that
// names itself, so that a stalled read reports "read", not "open".
func (f *winFile) command(cmd string) (*rcpResponse, error) {
	if f.closed.Load() {
		return nil, fs.ErrClosed
	}

	var resp <-chan []byte
	if cmd != "q" {
		resp = f.readResponse()
	}

	timer := time.NewTimer(commandTimeout)
	defer timer.Stop()

	// The write itself can also block forever on a dead connection: f.stdin
	// is an io.Pipe with no internal buffer, so it blocks until something
	// downstream reads it, and that downstream reader is the same network
	// forwarding loop that a dead WinRM session would have stalled. Run it
	// in a goroutine so it can be raced against the timeout too.
	writeErr := make(chan error, 1)
	go func() {
		log.Trace(context.Background(), "rigrcp command", log.KeyCommand, cmd)
		_, err := fmt.Fprintf(f.stdin, "%s\n", cmd)
		writeErr <- err
	}()

	select {
	case <-f.done:
		return nil, errEnded
	case <-timer.C:
		return nil, f.timedOut(fmt.Sprintf("writing rcp command %q", cmd))
	case err := <-writeErr:
		if err != nil {
			return nil, fmt.Errorf("write rcp command: %w", f.abortCause(err))
		}
	}

	if cmd == "q" {
		return &rcpResponse{}, nil
	}

	// The command is on its way, which is progress: the response gets its
	// own full budget rather than whatever the write left of it.
	return f.awaitResponse(resp, fmt.Sprintf("response to rcp command %q", cmd))
}

// readResponse starts reading the helper's next reply. The channel is
// closed without a value if the stream ends first.
func (f *winFile) readResponse() <-chan []byte {
	resp := make(chan []byte, 1)
	go func() {
		b, err := f.stdout.ReadBytes(0)
		if err != nil {
			log.Trace(context.Background(), "failed to read rcp response", log.KeyError, err)
			close(resp)
			return
		}
		resp <- b[:len(b)-1] // drop the zero byte
	}()
	return resp
}

// awaitResponse waits up to commandTimeout for the reply readResponse is
// reading, and decodes it. what names the reply in a timeout error.
func (f *winFile) awaitResponse(resp <-chan []byte, what string) (*rcpResponse, error) {
	timer := time.NewTimer(commandTimeout)
	defer timer.Stop()

	select {
	case <-f.done:
		return nil, errEnded
	case <-timer.C:
		return nil, f.timedOut("waiting for " + what)
	case data, ok := <-resp:
		if !ok {
			return nil, errEnded
		}
		return parseResponse(data)
	}
}

// parseResponse decodes a rigrcp reply, turning a reported error into a Go
// one.
func parseResponse(data []byte) (*rcpResponse, error) {
	out := &rcpResponse{}
	if len(data) == 0 {
		return out, fmt.Errorf("%w: invalid empty response to rcp command", errRemote)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return nil, fmt.Errorf("failed to unmarshal rcp response: %w", err)
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
	_, quitErr := f.command("q")
	log.Trace(context.Background(), "rigrcp quit", log.ErrorAttr(quitErr))
	f.stdin.Close()
	f.closed.Store(true)

	// The file itself is closed by now, so a helper that merely exited ahead
	// of the quit is not a failure. A quit that timed out is: the session
	// died rather than shut down, and the caller would otherwise carry on
	// against a dead host -- Upload goes straight from here to checksumming
	// the file it just wrote.
	if errors.Is(quitErr, ErrTimeout) {
		return f.pathErr(OpClose, quitErr)
	}

	// Writing the quit only got it as far as the local end of the
	// connection: it says the input copier took the bytes, not that the
	// helper acted on them. Wait for the helper to exit before calling the
	// session shut down, or Close reports success over a host that has
	// stopped answering and the caller carries straight on -- Upload goes
	// from here to checksumming the file it just wrote.
	timer := time.NewTimer(commandTimeout)
	defer timer.Stop()

	select {
	case <-f.done:
	case <-timer.C:
		return f.pathErr(OpClose, f.timedOut("waiting for the rigrcp helper to exit"))
	}

	return nil
}
