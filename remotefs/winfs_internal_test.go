package remotefs

import (
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

type stubCloser struct{ err error }

func (s stubCloser) Close() error { return s.err }

func TestCloseWithSession(t *testing.T) {
	sessionGone := fmt.Errorf("close: %w", ErrTimeout)

	t.Run("reports a session that timed out", func(t *testing.T) {
		var err error
		closeWithSession(stubCloser{err: sessionGone}, &err)
		require.ErrorIs(t, err, ErrTimeout)
	})

	t.Run("leaves the operation's own error alone", func(t *testing.T) {
		own := errors.New("read failed")
		err := own
		closeWithSession(stubCloser{err: sessionGone}, &err)
		require.Equal(t, own, err, "the operation's error is the one the caller wants to see")
	})

	t.Run("ignores an ordinary close failure", func(t *testing.T) {
		var err error
		closeWithSession(stubCloser{err: io.ErrClosedPipe}, &err)
		require.NoError(t, err, "only a dead session makes an otherwise good operation a failure")
	})
}
