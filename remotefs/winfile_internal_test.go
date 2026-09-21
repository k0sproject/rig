package remotefs

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// rcpSession wires a winFile to in-process pipes standing in for the remote
// rigrcp helper, so that a test can decide exactly how far the session gets
// before it goes silent -- the situation observed in practice when a Windows
// host reboots mid-transfer and the shell backing the session is never
// actually torn down on this end. See k0sproject/rig#472.
type rcpSession struct {
	file *winFile
	// commands is the helper's end of stdin. Nothing reads it unless a test
	// asks for it, which is what makes a write park.
	commands *bufio.Reader
	// responses is the helper's end of stdout, for answering commands.
	responses *io.PipeWriter
	// payloads is the file's own end of stdout, read directly rather than
	// through file.stdout: the bufio.Reader there belongs to whichever
	// command goroutine is parked on it.
	payloads *io.PipeReader
	// cancelled closes when the file aborts the session, which is how the
	// remote helper is stopped rather than left running.
	cancelled chan struct{}
}

// newRCPSession returns a session with commandTimeout lowered to timeout for
// the duration of the test.
func newRCPSession(t *testing.T, timeout time.Duration) *rcpSession {
	t.Helper()

	original := commandTimeout
	commandTimeout = timeout
	t.Cleanup(func() { commandTimeout = original })

	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()
	cancelled := make(chan struct{})

	// Tests deliberately leave the session parked, so release whatever is
	// still waiting on the pipes rather than leaking it for the rest of the
	// run.
	t.Cleanup(func() {
		_ = stdinR.Close()
		_ = stdoutW.Close()
	})

	return &rcpSession{
		file: &winFile{
			stdin:   stdinW,
			stdout:  bufio.NewReader(stdoutR),
			stdinR:  stdinR,
			stdoutW: stdoutW,
			done:    make(chan struct{}), // never closed: the session never reports having ended
			cancel:  sync.OnceFunc(func() { close(cancelled) }),
		},
		commands:  bufio.NewReader(stdinR),
		responses: stdoutW,
		payloads:  stdoutR,
		cancelled: cancelled,
	}
}

// answer reads one command per entry of responses and replies with it.
func (s *rcpSession) answer(responses []string) bool {
	for _, resp := range responses {
		if _, err := s.commands.ReadString('\n'); err != nil {
			return false
		}
		if _, err := s.responses.Write(append([]byte(resp), 0)); err != nil {
			return false
		}
	}
	return true
}

// serve answers each command with the matching entry of responses and then
// goes silent, still holding the pipes open. The returned channel closes
// once the last response is out -- anything else a test does to the pipes
// waits for that, so that the helper's single stdin stream has one reader
// at a time, as the real one does, and s.responses one writer.
func (s *rcpSession) serve(responses ...string) <-chan struct{} {
	served := make(chan struct{})
	go func() {
		defer close(served)
		_ = s.answer(responses)
	}()
	return served
}

// serveThenExit answers each command and then does what the helper does on a
// quit: it keeps reading, and closes done -- the signal that the rigrcp
// command has exited -- once the quit arrives.
func (s *rcpSession) serveThenExit(responses ...string) {
	go func() {
		if !s.answer(responses) {
			return
		}
		for {
			line, err := s.commands.ReadString('\n')
			if err != nil {
				return
			}
			if strings.HasPrefix(line, "q") {
				close(s.file.done)
				return
			}
		}
	}()
}

// serveAndDrain answers each command and then keeps reading, so nothing the
// file writes afterwards parks for want of a reader.
func (s *rcpSession) serveAndDrain(responses ...string) {
	go func() {
		if s.answer(responses) {
			_, _ = io.Copy(io.Discard, s.commands)
		}
	}()
}

// requireAborted asserts that the session was torn down rather than left
// half-open: the command context cancelled so the remote helper stops, both
// pipes closed so nothing stays parked on them, and the file marked closed
// so later operations fail instead of talking to a dead session.
func (s *rcpSession) requireAborted(t *testing.T) {
	t.Helper()

	select {
	case <-s.cancelled:
	case <-time.After(time.Second):
		t.Fatal("session was not aborted: the rigrcp command context was never cancelled")
	}

	_, err := s.file.stdin.Write([]byte("x"))
	require.ErrorIs(t, err, ErrTimeout, "stdin must be closed with the timeout, releasing any parked write")
	_, err = s.payloads.Read(make([]byte, 1))
	require.ErrorIs(t, err, ErrTimeout, "stdout must be closed with the timeout, releasing any parked read")

	require.True(t, s.file.closed.Load(), "the file must be marked closed")
	_, err = s.file.Read(make([]byte, 1))
	require.ErrorIs(t, err, fs.ErrClosed)
}

// TestWinFileCommandTimesOutWaitingForResponse verifies that command()
// returns a bounded error instead of blocking forever when the helper takes
// the command but never answers it.
func TestWinFileCommandTimesOutWaitingForResponse(t *testing.T) {
	s := newRCPSession(t, 50*time.Millisecond)
	s.serveAndDrain() // the command goes out fine; nothing ever answers it

	start := time.Now()
	_, err := s.file.command("o r w somefile")

	require.ErrorIs(t, err, ErrTimeout)
	require.ErrorIs(t, err, context.DeadlineExceeded, "ErrTimeout must also match the standard deadline error")
	require.Less(t, time.Since(start), time.Second, "command() must return promptly once commandTimeout elapses, not hang")
	s.requireAborted(t)
}

// TestWinFileCommandTimesOutWritingCommand verifies the other half of a round
// trip: when nothing downstream reads the pipe, the write of the command
// itself parks, and the timeout has to release it too.
func TestWinFileCommandTimesOutWritingCommand(t *testing.T) {
	s := newRCPSession(t, 50*time.Millisecond) // nothing ever reads s.commands

	start := time.Now()
	_, err := s.file.command("o r w somefile")

	require.ErrorIs(t, err, ErrTimeout)
	require.Less(t, time.Since(start), time.Second, "command() must return promptly once commandTimeout elapses, not hang")
	s.requireAborted(t)
}

// TestWinFileWritePayloadTimesOut covers the payload write, which happens
// after the round trip that announces it and used to be unbounded even once
// the round trips themselves were not: the helper accepts "w N" and then
// stops reading, so the payload parks.
func TestWinFileWritePayloadTimesOut(t *testing.T) {
	s := newRCPSession(t, 50*time.Millisecond)
	s.serve(`{"n":4}`) // answer the "w 4", then stop reading

	start := time.Now()
	_, err := s.file.Write([]byte("data"))

	require.ErrorIs(t, err, ErrTimeout)
	require.Less(t, time.Since(start), time.Second, "Write() must return promptly once commandTimeout elapses, not hang")
	s.requireAborted(t)
}

// TestWinFileReadPayloadTimesOut is the read-side counterpart: the helper
// promises four bytes and then never sends them.
func TestWinFileReadPayloadTimesOut(t *testing.T) {
	s := newRCPSession(t, 50*time.Millisecond)
	s.serve(`{"n":4}`) // answer the "r 4", then send no payload

	start := time.Now()
	_, err := s.file.Read(make([]byte, 4))

	require.ErrorIs(t, err, ErrTimeout)
	require.Less(t, time.Since(start), time.Second, "Read() must return promptly once commandTimeout elapses, not hang")
	s.requireAborted(t)
}

// TestWinFileCopyToSurvivesSlowTransfer verifies that commandTimeout bounds
// a lack of progress rather than the duration of a transfer: a copy that
// keeps trickling in must not be killed just for taking longer than the
// timeout in total.
func TestWinFileCopyToSurvivesSlowTransfer(t *testing.T) {
	// Each interval is a quarter of the timeout, so the test needs three
	// quarters of it in scheduler jitter before it starts flaking, while the
	// six of them still add up to more than the timeout.
	const (
		timeout = 200 * time.Millisecond
		chunks  = 6
	)

	s := newRCPSession(t, timeout)
	served := s.serve(`{"n":6}`)
	go func() {
		<-served // s.responses is serve's until the response is out
		for range chunks {
			time.Sleep(timeout / 4)
			if _, err := s.responses.Write([]byte("x")); err != nil {
				return
			}
		}
	}()

	var dst bytes.Buffer
	n, err := s.file.CopyTo(&dst)

	require.NoError(t, err)
	require.Equal(t, int64(chunks), n)
	require.Equal(t, "xxxxxx", dst.String())
}

// TestWinFileWriteSurvivesSlowTransfer is the write-side counterpart to
// TestWinFileCopyToSurvivesSlowTransfer: a payload larger than one chunk,
// drained steadily but slowly enough that the whole write outlasts
// commandTimeout, must still go through.
func TestWinFileWriteSurvivesSlowTransfer(t *testing.T) {
	// Same margin as TestWinFileCopyToSurvivesSlowTransfer: a quarter of the
	// timeout per chunk, six chunks, so the transfer outlasts the timeout
	// without any single gap coming close to it.
	const (
		timeout = 200 * time.Millisecond
		chunks  = 6
	)

	s := newRCPSession(t, timeout)
	served := s.serve(fmt.Sprintf(`{"n":%d}`, chunks*payloadChunkSize))
	drained := make(chan int64, 1)
	go func() {
		<-served // the stdin stream is serve's until it has read the command
		var total int64
		for range chunks {
			time.Sleep(timeout / 4)
			n, err := io.CopyN(io.Discard, s.commands, payloadChunkSize)
			total += n
			if err != nil {
				break
			}
		}
		drained <- total
	}()

	start := time.Now()
	n, err := s.file.Write(bytes.Repeat([]byte("x"), chunks*payloadChunkSize))

	require.NoError(t, err)
	require.Equal(t, chunks*payloadChunkSize, n)
	require.Greater(t, time.Since(start), timeout, "the write must outlast commandTimeout for this to prove anything")
	require.Equal(t, int64(chunks*payloadChunkSize), <-drained)
}

// TestWinFileWatchdogStopBeatsALateExpiry pins the photo finish: a watchdog
// expiring at the same moment as the operation it covers must not tear the
// session down once stop() has returned. time.Timer.Stop alone does not
// give that, because it does not wait for a callback that has already begun.
func TestWinFileWatchdogStopBeatsALateExpiry(t *testing.T) {
	for range 200 {
		s := newRCPSession(t, 0) // expires immediately: stop() and the callback collide
		dog := s.file.watch("photo finish")
		dog.stop()

		// Whatever the outcome of the race, it is settled by the time stop()
		// returns: the session must not be aborted after that.
		settled := s.file.closed.Load()
		time.Sleep(time.Millisecond)
		require.Equal(t, settled, s.file.closed.Load(), "the watchdog aborted the session after stop() returned")
	}
}

// TestWinFileWatchdogProgressBeatsALateExpiry pins the other half of the
// photo finish: an expiry that has already begun, and is queued behind the
// watchdog lock, must not tear the session down once progress has arrived
// ahead of it. Neither Stop nor Reset can call such a callback off.
func TestWinFileWatchdogProgressBeatsALateExpiry(t *testing.T) {
	s := newRCPSession(t, 10*time.Millisecond)
	dog := s.file.watch("photo finish")
	// The callback reads commandTimeout, which the session restores on
	// cleanup, so the watchdog has to be disarmed before the test ends.
	defer dog.stop()

	dog.mu.Lock()
	time.Sleep(50 * time.Millisecond) // the watchdog expires; its callback queues on mu

	// Progress lands first, and extends the budget well past the queued
	// callback. Taking the lock is what progress() does around this.
	commandTimeout = time.Minute
	dog.recordProgress()
	dog.mu.Unlock()

	time.Sleep(50 * time.Millisecond)
	require.False(t, s.file.closed.Load(), "the watchdog aborted a session that had just made progress")
}

// TestWinFileCloseReportsATimedOutQuit verifies that a quit which times out
// is reported rather than logged and swallowed. The file is closed by then,
// but the session is dead, and Upload goes straight from here to
// checksumming the file it just wrote.
func TestWinFileCloseReportsATimedOutQuit(t *testing.T) {
	s := newRCPSession(t, 50*time.Millisecond)
	s.serve(`{"pos":-1}`) // answer the close, then stop reading so the quit parks

	err := s.file.Close()

	require.ErrorIs(t, err, ErrTimeout)
}

// TestWinFileCloseSucceedsOnACleanQuit verifies the other side of that: a
// quit the helper acts on still closes the file without an error.
func TestWinFileCloseSucceedsOnACleanQuit(t *testing.T) {
	s := newRCPSession(t, time.Second)
	s.serveThenExit(`{"pos":-1}`)

	require.NoError(t, s.file.Close())
	require.True(t, s.file.closed.Load())
}

// TestWinFileCloseReportsAHelperThatNeverExits covers the gap between the two:
// the quit goes out and is even read, but the helper never exits, so the
// session is gone even though nothing about the write said so.
func TestWinFileCloseReportsAHelperThatNeverExits(t *testing.T) {
	s := newRCPSession(t, 50*time.Millisecond)
	s.serveAndDrain(`{"pos":-1}`) // the quit is accepted, but done never closes

	err := s.file.Close()

	require.ErrorIs(t, err, ErrTimeout)
}

// TestWinFileAbortCausePrefersTheTornDownReason pins why the abort reason is
// recorded rather than read back off the pipes: io.Pipe keeps the first close
// error it is given, so a transfer that raced the rigrcp command exiting
// reports a plain closed pipe, and Upload would not recognise the timeout it
// has to skip its cleanup for.
func TestWinFileAbortCausePrefersTheTornDownReason(t *testing.T) {
	s := newRCPSession(t, time.Minute)

	require.ErrorIs(t, s.file.abortCause(io.ErrClosedPipe), io.ErrClosedPipe, "with no abort, the pipe's own error stands")

	require.NoError(t, s.file.stdinR.Close()) // the command exits first
	s.file.abort(fmt.Errorf("%w: writing payload", ErrTimeout))

	_, err := s.file.stdin.Write([]byte("x"))
	require.ErrorIs(t, err, io.ErrClosedPipe, "the pipe reports the close it saw first")
	require.NotErrorIs(t, err, ErrTimeout)
	require.ErrorIs(t, s.file.abortCause(err), ErrTimeout, "so a payload transfer has to prefer the recorded reason")
}

// TestWinFileCommandRespondsBeforeTimeout verifies the happy path is
// unaffected: a prompt, well-formed response still succeeds.
func TestWinFileCommandRespondsBeforeTimeout(t *testing.T) {
	s := newRCPSession(t, time.Second)
	s.serve(`{"n":4}`)

	resp, err := s.file.command("r 4")

	require.NoError(t, err)
	require.Equal(t, int64(4), resp.N)
}

// TestWinFileCommandEndedSession verifies that a session ending (f.done
// closed) while waiting for a response is still reported as errEnded, not
// masked by the timeout.
func TestWinFileCommandEndedSession(t *testing.T) {
	s := newRCPSession(t, time.Second)
	close(s.file.done)

	_, err := s.file.command("r 4")

	require.ErrorIs(t, err, errEnded)
}
