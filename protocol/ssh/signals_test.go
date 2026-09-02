//go:build !windows

package ssh

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// errWriter fails every write, standing in for a session whose stdin pipe has
// already gone away.
type errWriter struct{}

func (errWriter) Write([]byte) (int, error) { return 0, errors.New("pipe closed") }

func TestWriteControlChar(t *testing.T) {
	t.Run("sends the character the terminal would send", func(t *testing.T) {
		var buf bytes.Buffer
		writeControlChar(context.Background(), &buf, "\x03", "interrupt")
		require.Equal(t, "\x03", buf.String(),
			"the remote pty's line discipline is what turns this into SIGINT, so the byte must go through unchanged")
	})

	t.Run("a write failure is not fatal", func(t *testing.T) {
		// There is nobody to return an error to from a signal handler, and the
		// command is still running, so a failed relay must not take anything down.
		require.NotPanics(t, func() {
			writeControlChar(context.Background(), errWriter{}, "\x1a", "suspend")
		})
	})
}

func TestRelayWindowChange(t *testing.T) {
	t.Run("an unreadable size is not reported at all", func(t *testing.T) {
		// A nil session panics the moment anything is sent on it, which is the
		// assertion: a size that cannot be read must stop the relay before it
		// reaches the session, rather than inventing a geometry to send.
		f, err := os.Open(os.DevNull)
		require.NoError(t, err)
		t.Cleanup(func() { _ = f.Close() })

		require.NotPanics(t, func() { relayWindowChange(context.Background(), nil, f) })
	})
}

func TestCaptureSignalsWithoutPTY(t *testing.T) {
	// Without a pty there is no line discipline to read control characters as
	// anything but data, so nothing may be relayed into the caller's stdin.
	var buf bytes.Buffer
	stop := captureSignals(context.Background(), &buf, nil, nil)
	require.NotNil(t, stop, "the stop function must always be callable")
	stop()
	require.Zero(t, buf.Len(), "no pty means nothing is relayed")

	// Calling it with a discarding writer must be just as inert.
	require.NotPanics(t, func() { captureSignals(context.Background(), io.Discard, nil, nil)() })
}
