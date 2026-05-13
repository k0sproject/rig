package remotefs

import (
	"errors"
	"io"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsTransportClosed(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"io.EOF", io.EOF, true},
		{"net.ErrClosed", net.ErrClosed, true},
		{"net.OpError wrapping net.ErrClosed", &net.OpError{Op: "read", Err: net.ErrClosed}, true},
		{"net.OpError generic — not teardown", &net.OpError{Op: "dial", Err: errors.New("reset")}, false},
		{"use of closed network connection", errors.New("use of closed network connection"), true},
		{"connection reset by peer", errors.New("connection reset by peer"), true},
		{"broken pipe", errors.New("write: broken pipe"), true},
		{"unexpected EOF", errors.New("unexpected EOF"), true},
		{"connection refused — not teardown", errors.New("connection refused"), false},
		{"session closed unexpectedly — not teardown", errors.New("session closed unexpectedly"), false},
		{"unrelated error", errors.New("exit code 1"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, isTransportClosed(tt.err))
		})
	}
}
