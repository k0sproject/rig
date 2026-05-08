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

func TestStreamServiceLogsCommand(t *testing.T) {
	cases := []struct {
		name     string
		streamer initsystem.ServiceManagerLogStreamer
		setup    rigtest.CommandMatcher
		expect   rigtest.CommandMatcher
	}{
		{
			name:     "Systemd",
			streamer: initsystem.Systemd{},
			setup:    rigtest.Contains("journalctl"),
			expect:   rigtest.Contains("journalctl -n 0 -f -u mysvc"),
		},
		{
			name:     "Upstart",
			streamer: initsystem.Upstart{},
			setup:    rigtest.Contains("tail"),
			expect:   rigtest.Contains("/var/log/upstart/mysvc.log"),
		},
		{
			name:     "Runit",
			streamer: initsystem.Runit{},
			setup:    rigtest.Contains("tail"),
			expect:   rigtest.Contains("/var/log/mysvc/current"),
		},
		{
			name:     "Launchd",
			streamer: initsystem.Launchd{},
			setup:    rigtest.Contains("log stream"),
			expect:   rigtest.Contains("mysvc"),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mr := rigtest.NewMockRunner()
			mr.AddCommandSuccess(tc.setup)
			_ = tc.streamer.StreamServiceLogs(context.Background(), mr, "mysvc", io.Discard)
			require.NoError(t, mr.Received(tc.expect))
		})
	}
}

// Compile-time checks.
var (
	_ initsystem.ServiceManagerLogStreamer = initsystem.Systemd{}
	_ initsystem.ServiceManagerLogStreamer = initsystem.Upstart{}
	_ initsystem.ServiceManagerLogStreamer = initsystem.Runit{}
	_ initsystem.ServiceManagerLogStreamer = initsystem.Launchd{}
)
