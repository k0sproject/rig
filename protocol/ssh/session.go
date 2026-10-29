package ssh

import (
	"context"
	"fmt"
	"io"
	"sync/atomic"
	"time"

	ssh "golang.org/x/crypto/ssh"
)

// cancelGrace is how long Wait keeps waiting for a command after its context
// is done. On a live connection the interrupt and channel close make the
// command end well within it, and Wait then reports the command's own result.
const cancelGrace = 2 * time.Second

// await runs call in a goroutine and returns its result, or ctx's error if
// ctx is done first. Opening a channel and sending a request both wait for the
// server to reply, which a connection that has gone silent never does, and
// x/crypto offers no way to cancel those waits. An abandoned call therefore
// keeps running, and late is called with whatever it eventually returns so
// that a resource it opened can be released.
func await[T any](ctx context.Context, call func() (T, error), late func(T)) (T, error) {
	var zero T
	if err := ctx.Err(); err != nil {
		return zero, err //nolint:wrapcheck // callers wrap
	}

	type result struct {
		value T
		err   error
	}
	res := make(chan result, 1)
	go func() {
		v, err := call()
		res <- result{value: v, err: err}
	}()

	select {
	case r := <-res:
		return r.value, r.err
	case <-ctx.Done():
		go func() {
			if r := <-res; r.err == nil && late != nil {
				late(r.value)
			}
		}()
		return zero, ctx.Err() //nolint:wrapcheck // callers wrap
	}
}

// newSession opens a session on client, giving up once ctx is done.
func newSession(ctx context.Context, client *ssh.Client) (*ssh.Session, error) {
	return await(ctx, client.NewSession, func(s *ssh.Session) { _ = s.Close() })
}

// startSession starts cmd on session, giving up once ctx is done. The session
// is closed if the command does not start. That happens in the background,
// because closing writes to a connection that may no longer be accepting data.
func startSession(ctx context.Context, session *ssh.Session, cmd string) error {
	_, err := await(ctx, func() (struct{}, error) { return struct{}{}, session.Start(cmd) }, nil)
	if err != nil {
		go func() { _ = session.Close() }()
	}
	return err
}

// detachableWriter forwards writes to w until it is detached, after which it
// accepts and drops them. x/crypto copies a session's output to the caller's
// writers in goroutines that end only when the channel does, so a Wait that
// gives up on a silent connection detaches them to keep a late write out of a
// writer the caller may already have recycled. A write already in progress
// cannot be interrupted.
type detachableWriter struct {
	detached atomic.Bool
	// w is set once, at construction, and only read afterwards.
	w io.Writer
}

func (d *detachableWriter) Write(p []byte) (int, error) {
	if d.detached.Load() {
		return len(p), nil
	}
	return d.w.Write(p) //nolint:wrapcheck // transparent pass-through to the caller's writer
}

func (d *detachableWriter) detach() {
	if d == nil {
		return
	}
	d.detached.Store(true)
}

// ctxReader stops feeding a command once ctx is done, so that the goroutine
// copying a caller's stdin does not keep reading it for a command Wait has
// abandoned. A read already in progress cannot be interrupted.
type ctxReader struct {
	ctx    context.Context //nolint:containedctx // an io.Reader has nowhere else to carry it
	reader io.Reader
}

func (c ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, fmt.Errorf("stop reading command input: %w", err)
	}
	return c.reader.Read(p) //nolint:wrapcheck // transparent pass-through to the caller's reader
}

// sessionWaiter is the protocol.Waiter for a command started on a session.
type sessionWaiter struct {
	ctx     context.Context //nolint:containedctx // needed to bound Wait, which takes no context of its own
	session *ssh.Session
	stdout  *detachableWriter
	stderr  *detachableWriter
	grace   time.Duration
	done    chan struct{}
	// err is written once, before done is closed.
	err error
}

// newSessionWaiter waits for the started session in the background and
// interrupts it once ctx is done, whether or not anyone calls Wait.
func newSessionWaiter(ctx context.Context, session *ssh.Session, stdout, stderr *detachableWriter) *sessionWaiter {
	waiter := &sessionWaiter{
		ctx:     ctx,
		session: session,
		stdout:  stdout,
		stderr:  stderr,
		grace:   cancelGrace,
		done:    make(chan struct{}),
	}
	go func() {
		waiter.err = session.Wait()
		close(waiter.done)
	}()
	go func() {
		select {
		case <-ctx.Done():
			_ = session.Signal(ssh.SIGINT)
			_ = session.Close()
		case <-waiter.done:
		}
	}()
	return waiter
}

// Wait blocks until the command finishes and returns an error if it did not
// exit with status zero.
//
// Once ctx is done the command is interrupted and given a short grace period
// to end. A connection that has gone silent never confirms that it did, so
// after the grace period Wait detaches the output writers and returns ctx's
// error instead, leaving the session to be cleaned up when the connection is
// closed.
func (w *sessionWaiter) Wait() error {
	select {
	case <-w.done:
		return w.err
	case <-w.ctx.Done():
	}

	timer := time.NewTimer(w.grace)
	defer timer.Stop()
	select {
	case <-w.done:
		return w.err
	case <-timer.C:
		w.stdout.detach()
		w.stderr.detach()
		return fmt.Errorf("%w: command did not complete", w.ctx.Err())
	}
}
