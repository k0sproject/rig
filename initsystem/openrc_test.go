package initsystem_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/k0sproject/rig/v2/initsystem"
	"github.com/k0sproject/rig/v2/rigtest"
	"github.com/stretchr/testify/require"
)

var (
	openrcResolve = rigtest.Contains("rc-service -r")
	openrcTail    = rigtest.HasPrefix("tail ")
)

func mockOpenRCLogFiles(mr *rigtest.MockRunner, files string) {
	mr.AddCommand(openrcResolve, func(a *rigtest.A) error {
		_, _ = a.Stdout.Write([]byte(files))
		return nil
	})
}

func TestOpenRCServiceLogs(t *testing.T) {
	ctx := context.Background()

	t.Run("tails every log file the script declares", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mockOpenRCLogFiles(mr, "/var/log/k0s.log\n/var/log/k0s.err\n/var/log/k0s.log\n")
		mr.AddCommandOutput(openrcTail, "out\nerr")

		lines, err := initsystem.OpenRC{}.ServiceLogs(ctx, mr, "k0scontroller", 10)
		require.NoError(t, err)
		require.Equal(t, []string{"out", "err"}, lines)
		require.NoError(t, mr.Received(rigtest.Contains("svc=k0scontroller")))
		require.NoError(t, mr.Received(rigtest.Equal("tail -q -n 10 -- /var/log/k0s.log /var/log/k0s.err")))
	})

	t.Run("quotes the service name", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mockOpenRCLogFiles(mr, "/var/log/x.log\n")
		mr.AddCommandSuccess(openrcTail)

		_, err := initsystem.OpenRC{}.ServiceLogs(ctx, mr, "a b;c", 1)
		require.NoError(t, err)
		require.NoError(t, mr.Received(rigtest.Contains(`svc='a b;c'`)))
	})

	t.Run("no log file is unsupported", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mockOpenRCLogFiles(mr, "\n")

		_, err := initsystem.OpenRC{}.ServiceLogs(ctx, mr, "syslogged", 10)
		require.ErrorIs(t, err, errors.ErrUnsupported)
		require.Error(t, mr.Received(openrcTail))
	})

	t.Run("resolve failure propagates", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mr.AddCommandFailure(openrcResolve, errExec)

		_, err := initsystem.OpenRC{}.ServiceLogs(ctx, mr, "missing", 10)
		require.ErrorIs(t, err, errExec)
	})

	t.Run("tail failure propagates", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mockOpenRCLogFiles(mr, "/var/log/k0s.log\n")
		mr.AddCommandFailure(openrcTail, errExec)

		_, err := initsystem.OpenRC{}.ServiceLogs(ctx, mr, "k0scontroller", 10)
		require.ErrorIs(t, err, errExec)
	})
}

func TestOpenRCStreamServiceLogs(t *testing.T) {
	t.Run("follows every log file the script declares", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mockOpenRCLogFiles(mr, "/var/log/k0s.log\n/var/log/k0s.err\n")
		mr.AddCommand(openrcTail, func(a *rigtest.A) error {
			_, _ = a.Stdout.Write([]byte("line1\nline2\n"))
			return nil
		})

		var buf bytes.Buffer
		require.NoError(t, initsystem.OpenRC{}.StreamServiceLogs(context.Background(), mr, "k0scontroller", &buf))
		require.Equal(t, "line1\nline2\n", buf.String())
		require.NoError(t, mr.Received(rigtest.Equal("tail -q -n 0 -F -- /var/log/k0s.log /var/log/k0s.err")))
	})

	t.Run("context cancellation is not an error", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mockOpenRCLogFiles(mr, "/var/log/k0s.log\n")
		ctx, cancel := context.WithCancel(context.Background())
		mr.AddCommand(openrcTail, func(_ *rigtest.A) error {
			cancel()
			return context.Canceled
		})

		require.NoError(t, initsystem.OpenRC{}.StreamServiceLogs(ctx, mr, "k0scontroller", io.Discard))
	})

	t.Run("no log file is unsupported", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mockOpenRCLogFiles(mr, "")

		err := initsystem.OpenRC{}.StreamServiceLogs(context.Background(), mr, "syslogged", io.Discard)
		require.ErrorIs(t, err, errors.ErrUnsupported)
	})

	t.Run("tail failure propagates", func(t *testing.T) {
		mr := rigtest.NewMockRunner()
		mockOpenRCLogFiles(mr, "/var/log/k0s.log\n")
		mr.AddCommandFailure(openrcTail, errExec)

		err := initsystem.OpenRC{}.StreamServiceLogs(context.Background(), mr, "k0scontroller", io.Discard)
		require.ErrorIs(t, err, errExec)
	})
}

var (
	_ initsystem.ServiceManagerLogReader   = initsystem.OpenRC{}
	_ initsystem.ServiceManagerLogStreamer = initsystem.OpenRC{}
)
