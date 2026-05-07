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

// streamCase groups parameters for a generic stream test.
type streamCase struct {
	name      string
	streamer  initsystem.ServiceManagerLogStreamer
	cmdSubstr string
}

func streamCases() []streamCase {
	return []streamCase{
		{
			name:      "Systemd",
			streamer:  initsystem.Systemd{},
			cmdSubstr: "journalctl",
		},
		{
			name:      "Upstart",
			streamer:  initsystem.Upstart{},
			cmdSubstr: "tail",
		},
		{
			name:      "Runit",
			streamer:  initsystem.Runit{},
			cmdSubstr: "tail",
		},
		{
			name:      "Launchd",
			streamer:  initsystem.Launchd{},
			cmdSubstr: "log stream",
		},
	}
}

func TestStreamServiceLogsOutput(t *testing.T) {
	for _, tc := range streamCases() {
		t.Run(tc.name, func(t *testing.T) {
			mr := rigtest.NewMockRunner()
			mr.AddCommand(rigtest.Contains(tc.cmdSubstr), func(a *rigtest.A) error {
				_, _ = a.Stdout.Write([]byte("line1\nline2\n"))
				return nil
			})

			var buf bytes.Buffer
			err := tc.streamer.StreamServiceLogs(context.Background(), mr, "svc", &buf)
			require.NoError(t, err)
			require.Equal(t, "line1\nline2\n", buf.String())
		})
	}
}

func TestStreamServiceLogsContextCancel(t *testing.T) {
	for _, tc := range streamCases() {
		t.Run(tc.name, func(t *testing.T) {
			mr := rigtest.NewMockRunner()
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			mr.AddCommand(rigtest.Contains(tc.cmdSubstr), func(a *rigtest.A) error {
				return a.Ctx.Err()
			})

			err := tc.streamer.StreamServiceLogs(ctx, mr, "svc", io.Discard)
			require.NoError(t, err, "context cancellation should not return an error")
		})
	}
}

func TestStreamServiceLogsError(t *testing.T) {
	for _, tc := range streamCases() {
		t.Run(tc.name, func(t *testing.T) {
			mr := rigtest.NewMockRunner()
			execErr := errors.New("exec failed")
			mr.AddCommandFailure(rigtest.Contains(tc.cmdSubstr), execErr)

			err := tc.streamer.StreamServiceLogs(context.Background(), mr, "svc", io.Discard)
			require.ErrorIs(t, err, execErr)
		})
	}
}

func TestSystemdStreamServiceLogsCommand(t *testing.T) {
	mr := rigtest.NewMockRunner()
	mr.AddCommandSuccess(rigtest.Contains("journalctl"))

	_ = initsystem.Systemd{}.StreamServiceLogs(context.Background(), mr, "mysvc", io.Discard)

	require.NoError(t, mr.Received(rigtest.Contains("journalctl -f -u mysvc")))
}

func TestUpstartStreamServiceLogsCommand(t *testing.T) {
	mr := rigtest.NewMockRunner()
	mr.AddCommandSuccess(rigtest.Contains("tail"))

	_ = initsystem.Upstart{}.StreamServiceLogs(context.Background(), mr, "mysvc", io.Discard)

	require.NoError(t, mr.Received(rigtest.Contains("/var/log/upstart/mysvc.log")))
}

func TestRunitStreamServiceLogsCommand(t *testing.T) {
	mr := rigtest.NewMockRunner()
	mr.AddCommandSuccess(rigtest.Contains("tail"))

	_ = initsystem.Runit{}.StreamServiceLogs(context.Background(), mr, "mysvc", io.Discard)

	require.NoError(t, mr.Received(rigtest.Contains("/var/log/mysvc/current")))
}

func TestLaunchdStreamServiceLogsCommand(t *testing.T) {
	mr := rigtest.NewMockRunner()
	mr.AddCommandSuccess(rigtest.Contains("log stream"))

	_ = initsystem.Launchd{}.StreamServiceLogs(context.Background(), mr, "mysvc", io.Discard)

	require.NoError(t, mr.Received(rigtest.Contains("mysvc")))
}

// Compile-time checks.
var (
	_ initsystem.ServiceManagerLogStreamer = initsystem.Systemd{}
	_ initsystem.ServiceManagerLogStreamer = initsystem.Upstart{}
	_ initsystem.ServiceManagerLogStreamer = initsystem.Runit{}
	_ initsystem.ServiceManagerLogStreamer = initsystem.Launchd{}
)
