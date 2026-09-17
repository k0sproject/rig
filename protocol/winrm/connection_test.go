package winrm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/k0sproject/rig/v2/protocol"
)

func Test_isAuthError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "http error 401",
			err:  fmt.Errorf("http error 401: Unauthorized"),
			want: true,
		},
		{
			name: "http error 403",
			err:  fmt.Errorf("http error 403: Forbidden"),
			want: true,
		},
		{
			name: "http response error 401",
			err:  fmt.Errorf("http response error: 401 - %w", errors.New("unauthorized")),
			want: true,
		},
		{
			name: "http response error 403",
			err:  fmt.Errorf("http response error: 403 - %w", errors.New("forbidden")),
			want: true,
		},
		{
			name: "connection refused",
			err:  fmt.Errorf("dial tcp 10.0.0.1:5985: connect: connection refused"),
			want: false,
		},
		{
			name: "http error 500",
			err:  fmt.Errorf("http error 500: Internal Server Error"),
			want: false,
		},
		{
			name: "create shell error wrapping auth",
			err:  fmt.Errorf("create shell: http error 401: Unauthorized"),
			want: true,
		},
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "timeout",
			err:  fmt.Errorf("context deadline exceeded"),
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isAuthError(tt.err)
			if got != tt.want {
				t.Errorf("isAuthError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// Test_authErrorWrapping verifies that the classification/wrapping logic used in
// probe tags auth errors with ErrAuthFailed and leaves other errors untagged,
// and that neither is marked non-retryable -- a credential rejection is the
// remote host's answer and is routinely transient while a host is still being
// provisioned. probe itself is not called here because it requires a live WinRM
// client; see the todo item for WinRM integration tests.
func Test_authErrorWrapping(t *testing.T) {
	authErr := fmt.Errorf("create shell: http error 401: Unauthorized")
	nonAuthErr := fmt.Errorf("dial tcp 10.0.0.1:5985: connect: connection refused")

	tests := []struct {
		name           string
		startErr       error
		wantAuthFailed bool
		wantErrContain string
	}{
		{
			name:           "auth failure is tagged ErrAuthFailed",
			startErr:       authErr,
			wantAuthFailed: true,
			wantErrContain: authErr.Error(),
		},
		{
			name:           "network failure is not tagged",
			startErr:       nonAuthErr,
			wantAuthFailed: false,
			wantErrContain: nonAuthErr.Error(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Exercise the classification logic directly rather than through the full
			// probe method (which requires a real WinRM client). Integration coverage
			// against a real WinRM server is deferred to the todo item for WinRM tests.
			var err error
			if isAuthError(tt.startErr) {
				err = fmt.Errorf("%w: %w", protocol.ErrAuthFailed, tt.startErr)
			} else {
				err = tt.startErr
			}

			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			if got := errors.Is(err, protocol.ErrAuthFailed); got != tt.wantAuthFailed {
				t.Errorf("errors.Is(err, protocol.ErrAuthFailed) = %v, want %v (err: %v)", got, tt.wantAuthFailed, err)
			}
			// Neither case may abort a caller's retry loop.
			if errors.Is(err, protocol.ErrNonRetryable) {
				t.Errorf("err must not be ErrNonRetryable, a rejection can clear once the host finishes provisioning (err: %v)", err)
			}
			if tt.wantErrContain != "" {
				if msg := err.Error(); !strings.Contains(msg, tt.wantErrContain) {
					t.Errorf("err.Error() = %q, want it to contain %q", msg, tt.wantErrContain)
				}
			}
		})
	}
}

func TestConnect_probeClassification(t *testing.T) {
	tests := []struct {
		name           string
		statusCode     int
		wantAuthFailed bool
	}{
		{
			name:           "401 becomes ErrAuthFailed",
			statusCode:     http.StatusUnauthorized,
			wantAuthFailed: true,
		},
		{
			name:           "403 becomes ErrAuthFailed",
			statusCode:     http.StatusForbidden,
			wantAuthFailed: true,
		},
		{
			name:           "500 is not an auth failure",
			statusCode:     http.StatusInternalServerError,
			wantAuthFailed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.statusCode)
			}))
			t.Cleanup(srv.Close)

			// Parse host and port from the test server URL.
			u, parseErr := url.Parse(srv.URL)
			if parseErr != nil {
				t.Fatalf("url.Parse(%q) error = %v", srv.URL, parseErr)
			}
			host, portStr, splitErr := net.SplitHostPort(u.Host)
			if splitErr != nil {
				t.Fatalf("net.SplitHostPort(%q) error = %v", u.Host, splitErr)
			}
			port, convErr := strconv.Atoi(portStr)
			if convErr != nil {
				t.Fatalf("strconv.Atoi(%q) error = %v", portStr, convErr)
			}

			conn, err := NewConnection(Config{
				Address:  host,
				Port:     port,
				User:     "user",
				Password: "pass",
			})
			if err != nil {
				t.Fatalf("NewConnection() error = %v", err)
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			err = conn.Connect(ctx)
			if err == nil {
				t.Fatal("Connect() succeeded against stub server, want error")
			}

			if got := errors.Is(err, protocol.ErrAuthFailed); got != tt.wantAuthFailed {
				t.Errorf("Connect() errors.Is(err, protocol.ErrAuthFailed) = %v, want %v (err: %v)", got, tt.wantAuthFailed, err)
			}

			// No HTTP status from the remote may abort a caller's retry loop: the
			// response can differ once the host finishes provisioning.
			if errors.Is(err, protocol.ErrNonRetryable) {
				t.Errorf("Connect() must not return ErrNonRetryable for an HTTP %d (err: %v)", tt.statusCode, err)
			}

			if conn.IsConnected() {
				t.Error("IsConnected() = true after failed Connect, want false")
			}
		})
	}
}

type errWriter struct{ err error }

func (e errWriter) Write(_ []byte) (int, error) { return 0, e.err }

// blockingWriter parks inside Write until released, announcing on entered
// that it has got there, so a test can be sure a write is in flight.
type blockingWriter struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *blockingWriter) Write(p []byte) (int, error) {
	b.once.Do(func() { close(b.entered) })
	<-b.release
	return len(p), nil
}

func TestCtxReader(t *testing.T) {
	t.Run("passes reads through", func(t *testing.T) {
		r := ctxReader{ctx: context.Background(), reader: strings.NewReader("hello")}

		got, err := io.ReadAll(r)
		if err != nil {
			t.Fatalf("ReadAll() error = %v", err)
		}
		if string(got) != "hello" {
			t.Errorf("ReadAll() = %q, want %q", got, "hello")
		}
	})

	t.Run("stops once the context is done", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		r := ctxReader{ctx: ctx, reader: strings.NewReader("hello")}

		if _, err := r.Read(make([]byte, 5)); !errors.Is(err, context.Canceled) {
			t.Errorf("Read() error = %v, want %v", err, context.Canceled)
		}
	})
}

func TestDetachableWriter(t *testing.T) {
	t.Run("forwards until detached", func(t *testing.T) {
		var buf bytes.Buffer
		w := &detachableWriter{w: &buf}

		if n, err := w.Write([]byte("before")); err != nil || n != 6 {
			t.Fatalf("Write() = %d, %v, want 6, nil", n, err)
		}

		w.detach()

		// A detached writer still reports the write as accepted: the
		// goroutine behind it has to keep draining the command until the
		// shell is torn down, not bail out early.
		if n, err := w.Write([]byte("after")); err != nil || n != 5 {
			t.Fatalf("Write() after detach = %d, %v, want 5, nil", n, err)
		}
		if got := buf.String(); got != "before" {
			t.Errorf("underlying writer = %q, want %q: nothing may reach it after detach", got, "before")
		}
	})

	t.Run("reports underlying errors", func(t *testing.T) {
		failure := errors.New("write failed")
		w := &detachableWriter{w: errWriter{failure}}

		if _, err := w.Write([]byte("x")); !errors.Is(err, failure) {
			t.Errorf("Write() error = %v, want %v", err, failure)
		}
	})

	t.Run("detach is nil safe", func(t *testing.T) {
		var w *detachableWriter
		w.detach()
	})

	t.Run("detach does not wait for a parked write", func(t *testing.T) {
		// ExecReaderContext hands the command an io.PipeWriter, so with
		// nothing consuming the other end a write parks indefinitely. detach
		// still has to return, or Wait is unbounded all over again.
		blocked := &blockingWriter{entered: make(chan struct{}), release: make(chan struct{})}
		w := &detachableWriter{w: blocked}

		go func() { _, _ = w.Write([]byte("parked")) }()
		<-blocked.entered

		detached := make(chan struct{})
		go func() {
			defer close(detached)
			w.detach()
		}()
		select {
		case <-detached:
		case <-time.After(10 * time.Second):
			t.Fatal("detach() blocked behind a parked write")
		}
		close(blocked.release)
	})

	t.Run("detach races with a write in flight", func(t *testing.T) {
		var buf bytes.Buffer
		w := &detachableWriter{w: &buf}

		var wg sync.WaitGroup
		wg.Go(func() {
			for range 100 {
				_, _ = w.Write([]byte("x"))
			}
		})
		wg.Go(w.detach)
		wg.Wait()
	})
}

// fakeWinrmCommand stands in for *winrm.Command, whose real implementation
// talks SOAP to a Windows host. Wait blocks until the command is finished or
// closed, as the real one does.
type fakeWinrmCommand struct {
	finished chan struct{}
	once     sync.Once
	closes   atomic.Int32
	exitCode int
}

func newFakeWinrmCommand(exitCode int) *fakeWinrmCommand {
	return &fakeWinrmCommand{finished: make(chan struct{}), exitCode: exitCode}
}

func (f *fakeWinrmCommand) finish()       { f.once.Do(func() { close(f.finished) }) }
func (f *fakeWinrmCommand) Wait()         { <-f.finished }
func (f *fakeWinrmCommand) ExitCode() int { return f.exitCode }

func (f *fakeWinrmCommand) Close() error {
	f.closes.Add(1)
	f.finish() // the real Close releases Wait too
	return nil
}

type fakeWinrmShell struct{ closes atomic.Int32 }

func (f *fakeWinrmShell) Close() error {
	f.closes.Add(1)
	return nil
}

func newTestCommand(ctx context.Context, sh winrmShell, cmd winrmCommand, stdout, stderr io.Writer) *command {
	return &command{
		ctx:    ctx,
		sh:     sh,
		cmd:    cmd,
		stdout: &detachableWriter{w: stdout},
		stderr: &detachableWriter{w: stderr},
	}
}

func TestCommandWait(t *testing.T) {
	t.Run("returns when the command finishes inside the deadline", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		proc := newFakeWinrmCommand(0)
		proc.finish()
		c := newTestCommand(ctx, &fakeWinrmShell{}, proc, io.Discard, io.Discard)

		if err := c.Wait(); err != nil {
			t.Fatalf("Wait() error = %v, want nil", err)
		}
		if c.stdout.detached.Load() {
			t.Error("the output writers were detached on a command that finished normally")
		}
	})

	t.Run("reports a non-zero exit code", func(t *testing.T) {
		proc := newFakeWinrmCommand(3)
		proc.finish()
		c := newTestCommand(context.Background(), &fakeWinrmShell{}, proc, io.Discard, io.Discard)

		err := c.Wait()
		if !errors.Is(err, errExitCode) {
			t.Fatalf("Wait() error = %v, want %v", err, errExitCode)
		}
		if !strings.Contains(err.Error(), "3") {
			t.Errorf("Wait() error = %q, want it to name exit code 3", err)
		}
	})

	t.Run("gives up when the deadline passes first", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		proc := newFakeWinrmCommand(0) // never finishes on its own
		shell := &fakeWinrmShell{}
		var out bytes.Buffer
		c := newTestCommand(ctx, shell, proc, &out, io.Discard)

		start := time.Now()
		err := c.Wait()

		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Wait() error = %v, want %v", err, context.DeadlineExceeded)
		}
		if elapsed := time.Since(start); elapsed > 10*time.Second {
			t.Fatalf("Wait() took %s, want it to give up promptly", elapsed)
		}

		// The copy goroutines outlive Wait, so the caller's writer must be
		// out of reach by the time it returns.
		if _, err := c.stdout.Write([]byte("late output")); err != nil {
			t.Fatalf("Write() after Wait error = %v", err)
		}
		if out.Len() != 0 {
			t.Errorf("caller's writer received %q after Wait returned", out.String())
		}

		// Teardown runs in the background, so it is only eventually visible.
		deadline := time.Now().Add(10 * time.Second)
		for proc.closes.Load() == 0 || shell.closes.Load() == 0 {
			if time.Now().After(deadline) {
				t.Fatalf("command/shell were not closed: cmd=%d shell=%d", proc.closes.Load(), shell.closes.Load())
			}
			time.Sleep(time.Millisecond)
		}
	})

	t.Run("waits for the output copies before returning", func(t *testing.T) {
		proc := newFakeWinrmCommand(0)
		proc.finish()
		c := newTestCommand(context.Background(), &fakeWinrmShell{}, proc, io.Discard, io.Discard)

		copied := make(chan struct{})
		c.wg.Go(func() {
			time.Sleep(20 * time.Millisecond)
			close(copied)
		})

		if err := c.Wait(); err != nil {
			t.Fatalf("Wait() error = %v", err)
		}
		select {
		case <-copied:
		default:
			t.Error("Wait() returned before the output copies finished")
		}
	})
}
