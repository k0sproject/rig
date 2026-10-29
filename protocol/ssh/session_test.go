package ssh

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	ssh "golang.org/x/crypto/ssh"
)

// sessionBehavior decides how a test server answers a session channel.
type sessionBehavior int

const (
	// sessionRun writes "hello" and exits with status 0.
	sessionRun sessionBehavior = iota
	// sessionInterruptible runs until it is signalled or closed, then exits
	// with status 130.
	sessionInterruptible
)

type exitStatusMsg struct {
	Status uint32
}

// freezingProxy forwards a TCP connection until it is frozen, after which it
// stops forwarding in both directions without closing anything. That is what
// a connection that has gone silent looks like to the client: x/crypto's
// server side answers a channel close on its own, so a server that merely
// stops responding cannot model it.
type freezingProxy struct {
	frozen chan struct{}
	stop   chan struct{}
	once   sync.Once
}

func (p *freezingProxy) freeze() { p.once.Do(func() { close(p.frozen) }) }

type gateWriter struct {
	w io.Writer
	p *freezingProxy
}

func (g gateWriter) Write(b []byte) (int, error) {
	select {
	case <-g.p.frozen:
		<-g.p.stop
		return 0, io.ErrClosedPipe
	default:
	}
	return g.w.Write(b) //nolint:wrapcheck // test pass-through
}

// startFreezingProxy starts a proxy to target and returns its address.
func startFreezingProxy(t *testing.T, target string) (string, *freezingProxy) {
	t.Helper()
	p := &freezingProxy{frozen: make(chan struct{}), stop: make(chan struct{})}
	t.Cleanup(func() { close(p.stop) })
	addr := startRawServer(t, func(c net.Conn) {
		upstream, err := net.Dial("tcp", target)
		if err != nil {
			return
		}
		defer upstream.Close()
		go func() {
			_, _ = io.Copy(gateWriter{w: upstream, p: p}, c)
			_ = upstream.Close()
		}()
		_, _ = io.Copy(gateWriter{w: c, p: p}, upstream)
	})
	return addr, p
}

// startSessionServer starts an SSH server that serves session channels with
// the given behavior, behind a proxy that can be frozen, and returns a
// Connection connected to it through the proxy.
func startSessionServer(t *testing.T, behavior sessionBehavior) (*Connection, *freezingProxy) {
	t.Helper()
	withConfigParser(t, "")
	t.Setenv("SSH_AUTH_SOCK", "")

	hostSigner := newHostSigner(t)
	cfg := &ssh.ServerConfig{NoClientAuth: true, ServerVersion: "SSH-2.0-test-linux"}
	cfg.AddHostKey(hostSigner)

	serverAddr := startRawServer(t, func(c net.Conn) {
		sconn, chans, reqs, err := ssh.NewServerConn(c, cfg)
		if err != nil {
			return
		}
		defer sconn.Close()
		go ssh.DiscardRequests(reqs)
		for newChan := range chans {
			go serveSession(newChan, behavior)
		}
	})
	addr, proxy := startFreezingProxy(t, serverAddr)
	pinHostKey(t, addr, hostSigner)

	host, port := splitHostPort(t, addr)
	conn, err := NewConnection(Config{
		Address:     host,
		Port:        port,
		User:        "test",
		AuthMethods: []ssh.AuthMethod{ssh.Password("any")},
	})
	require.NoError(t, err)
	t.Cleanup(conn.Disconnect)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, conn.Connect(ctx))
	return conn, proxy
}

func serveSession(newChan ssh.NewChannel, behavior sessionBehavior) {
	channel, reqs, err := newChan.Accept()
	if err != nil {
		return
	}
	defer channel.Close()

	for req := range reqs {
		switch req.Type {
		case "exec":
			_ = req.Reply(true, nil)
			if behavior == sessionRun {
				_, _ = channel.Write([]byte("hello"))
				_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(exitStatusMsg{Status: 0}))
				return
			}
		case "signal":
			_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(exitStatusMsg{Status: 130}))
			return
		default:
			_ = req.Reply(false, nil)
		}
	}
	// The client closed the channel without a signal.
	_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(exitStatusMsg{Status: 130}))
}

func TestStartProcessRunsCommand(t *testing.T) {
	conn, _ := startSessionServer(t, sessionRun)

	var stdout bytes.Buffer
	waiter, err := conn.StartProcess(context.Background(), "true", nil, &stdout, nil)
	require.NoError(t, err)
	require.NoError(t, waiter.Wait())
	require.Equal(t, "hello", stdout.String())
}

func TestStartProcessCancelReportsCommandResult(t *testing.T) {
	conn, _ := startSessionServer(t, sessionInterruptible)

	ctx, cancel := context.WithCancel(context.Background())
	waiter, err := conn.StartProcess(ctx, "sleep 60", strings.NewReader(""), nil, nil)
	require.NoError(t, err)
	cancel()

	// The client sends the interrupt and the channel close back to back, and
	// x/crypto's server side answers the close on its own, so whether the exit
	// status makes it in first is a race in this server. Either way the result
	// is the session's own, not the context error.
	err = waiter.Wait()
	require.Error(t, err)
	require.NotErrorIs(t, err, context.Canceled, "a command that ends when interrupted reports its own result")
	var exitErr *ssh.ExitError
	var missingErr *ssh.ExitMissingError
	require.True(t, errors.As(err, &exitErr) || errors.As(err, &missingErr), "unexpected error: %v", err)
}

func TestStartProcessWaitBoundedOnSilentConnection(t *testing.T) {
	conn, proxy := startSessionServer(t, sessionInterruptible)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdout bytes.Buffer
	waiter, err := conn.StartProcess(ctx, "sleep 60", nil, &stdout, nil)
	require.NoError(t, err)
	proxy.freeze()
	cancel()

	start := time.Now()
	err = waiter.Wait()
	require.ErrorIs(t, err, context.Canceled)
	require.Less(t, time.Since(start), cancelGrace+5*time.Second)
}

func TestStartProcessSessionOpenBoundedOnSilentConnection(t *testing.T) {
	conn, proxy := startSessionServer(t, sessionRun)
	proxy.freeze()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := conn.StartProcess(ctx, "true", nil, nil, nil)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Less(t, time.Since(start), 5*time.Second)
}

func TestAwait(t *testing.T) {
	t.Run("returns the result", func(t *testing.T) {
		v, err := await(context.Background(), func() (int, error) { return 1, nil }, nil)
		require.NoError(t, err)
		require.Equal(t, 1, v)
	})

	t.Run("returns the error", func(t *testing.T) {
		failure := errors.New("failure")
		_, err := await(context.Background(), func() (int, error) { return 0, failure }, nil)
		require.ErrorIs(t, err, failure)
	})

	t.Run("done context is not run", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		ran := false
		_, err := await(ctx, func() (int, error) { ran = true; return 1, nil }, nil)
		require.ErrorIs(t, err, context.Canceled)
		require.False(t, ran)
	})

	t.Run("late result is handed over", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		release := make(chan struct{})
		late := make(chan int, 1)
		_, err := await(ctx, func() (int, error) {
			// Cancel only once the call is under way, so await takes the
			// abandon path rather than its already-done fast path.
			cancel()
			<-release
			return 7, nil
		}, func(v int) { late <- v })
		require.ErrorIs(t, err, context.Canceled)
		close(release)
		select {
		case v := <-late:
			require.Equal(t, 7, v)
		case <-time.After(5 * time.Second):
			t.Fatal("late result was not handed over")
		}
	})
}

func TestDetachableWriter(t *testing.T) {
	var buf bytes.Buffer
	w := &detachableWriter{w: &buf}
	n, err := w.Write([]byte("a"))
	require.NoError(t, err)
	require.Equal(t, 1, n)
	w.detach()
	n, err = w.Write([]byte("bc"))
	require.NoError(t, err)
	require.Equal(t, 2, n, "a detached write is accepted")
	require.Equal(t, "a", buf.String(), "and dropped")

	var nilWriter *detachableWriter
	require.NotPanics(t, nilWriter.detach)
}

func TestCtxReader(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	r := ctxReader{ctx: ctx, reader: strings.NewReader("hello")}
	p := make([]byte, 2)
	n, err := r.Read(p)
	require.NoError(t, err)
	require.Equal(t, "he", string(p[:n]))
	cancel()
	_, err = r.Read(p)
	require.ErrorIs(t, err, context.Canceled)
}
