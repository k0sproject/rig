package openssh

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/k0sproject/rig/v2/protocol"
	"github.com/stretchr/testify/require"
)

func Test_isAuthError(t *testing.T) {
	tests := []struct {
		name   string
		stderr string
		want   bool
	}{
		{
			name:   "empty stderr",
			stderr: "",
			want:   false,
		},
		{
			name:   "publickey rejected",
			stderr: "test@127.0.0.1: Permission denied (publickey).\r\n",
			want:   true,
		},
		{
			name:   "several methods rejected",
			stderr: "test@127.0.0.1: Permission denied (publickey,gssapi-keyex,password).\r\n",
			want:   true,
		},
		{
			name:   "verbose output before the rejection",
			stderr: "debug1: Next authentication method: password\ntest@host: Permission denied (password,keyboard-interactive).\n",
			want:   true,
		},
		{
			name:   "remote command permission denied is not an auth failure",
			stderr: "cat: /etc/shadow: Permission denied\n",
			want:   false,
		},
		{
			name:   "unreadable local key file is not an auth failure",
			stderr: "Load key \"/home/test/.ssh/id_ed25519\": Permission denied\n",
			want:   false,
		},
		{
			name:   "empty method list is not a rejection",
			stderr: "Permission denied ()\n",
			want:   false,
		},
		{
			name:   "unterminated method list is not a rejection",
			stderr: "Permission denied (publickey",
			want:   false,
		},
		{
			name:   "host key failure is not an auth failure",
			stderr: "Host key verification failed.\r\n",
			want:   false,
		},
		{
			name:   "connection refused is not an auth failure",
			stderr: "ssh: connect to host 10.0.0.1 port 22: Connection refused\r\n",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, isAuthError(tt.stderr), "isAuthError(%q)", tt.stderr)
		})
	}
}

func Test_isHostKeyError(t *testing.T) {
	tests := []struct {
		name   string
		stderr string
		want   bool
	}{
		{
			name:   "empty stderr",
			stderr: "",
			want:   false,
		},
		{
			name:   "verification failed",
			stderr: "Host key verification failed.\r\n",
			want:   true,
		},
		{
			name:   "identification changed",
			stderr: "@@@@@@@\nWARNING: REMOTE HOST IDENTIFICATION HAS CHANGED!\n",
			want:   true,
		},
		{
			name:   "auth failure is not a host key error",
			stderr: "test@127.0.0.1: Permission denied (publickey).\r\n",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, isHostKeyError(tt.stderr), "isHostKeyError(%q)", tt.stderr)
		})
	}
}

func Test_classifyConnectError(t *testing.T) {
	errExit := errors.New("exit status 255")

	tests := []struct {
		name         string
		stderr       string
		wantMessage  string
		wantSentinel error
	}{
		{
			name:         "host key failure",
			stderr:       "Host key verification failed.\r\n",
			wantMessage:  protocol.ErrNonRetryable.Error() + ": failed to connect: host key verification failed: exit status 255 (Host key verification failed.)",
			wantSentinel: protocol.ErrNonRetryable,
		},
		{
			name:         "credential rejection",
			stderr:       "test@127.0.0.1: Permission denied (publickey).\r\n",
			wantMessage:  protocol.ErrAuthFailed.Error() + ": failed to connect: exit status 255 (test@127.0.0.1: Permission denied (publickey).)",
			wantSentinel: protocol.ErrAuthFailed,
		},
		{
			name:        "unclassified failure",
			stderr:      "ssh: connect to host 10.0.0.1 port 22: Connection refused\r\n",
			wantMessage: "failed to connect: exit status 255 (ssh: connect to host 10.0.0.1 port 22: Connection refused)",
		},
		{
			name:        "empty stderr is not appended",
			stderr:      "",
			wantMessage: "failed to connect: exit status 255",
		},
		{
			name:        "blank stderr is not appended",
			stderr:      "\r\n \n",
			wantMessage: "failed to connect: exit status 255",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := classifyConnectError(errExit, tt.stderr, "failed to connect")
			require.EqualError(t, err, tt.wantMessage)
			require.ErrorIs(t, err, errExit, "the underlying error must stay unwrappable")
			if tt.wantSentinel != nil {
				require.ErrorIs(t, err, tt.wantSentinel)
			}
			// only a host key failure is fatal; everything else stays retryable.
			if !errors.Is(tt.wantSentinel, protocol.ErrNonRetryable) {
				require.NotErrorIs(t, err, protocol.ErrNonRetryable)
			}
		})
	}
}

// controlMasterLifetime is how long the fake client's stand-in control master
// lingers, and connectBudget is how long Connect may take. The two are set an
// order of magnitude apart: a leaked stderr pipe blocks Connect for the master's
// full lifetime, while a Connect that correctly returns with the foreground ssh
// takes well under a second even though it forks twice (control master, then the
// Windows probe). The budget is loose because "go test ./..." runs packages in
// parallel, and fork latency on a saturated machine stretches Connect to a few
// seconds.
const (
	controlMasterLifetime = 30 * time.Second
	connectBudget         = 10 * time.Second
)

// fakeSSHScript stands in for the openssh client and reproduces what
// "ssh -N -f" does on a successful connect: fork a background process that
// inherits stderr and outlives the foreground process, then exit 0. The
// backgrounded sleep stands in for a control master lingering for
// ControlPersist. It is a plain bounded sleep rather than something the test
// signals, so that a regression fails the test instead of deadlocking it --
// cleanup cannot run while the test goroutine is blocked inside Connect. Its pid
// is recorded so that the test can reap it once Connect has returned instead of
// leaving it to time out.
const fakeSSHScript = `#!/bin/sh
echo "$@" >> "$RIG_TEST_ARGV_LOG"
case " $* " in
*" -f "*) ( sleep "$RIG_TEST_MASTER_LIFETIME" & echo "$!" >> "$RIG_TEST_MASTER_PID_LOG" ) ;;
esac
echo 'fake ssh: control master forked' >&2
exit 0
`

// killRecordedMasters reaps the stand-in control masters whose pids the fake ssh
// client recorded in path. It is best-effort throughout: the file is absent when
// nothing daemonized, and the kill only means anything while the sleep is still
// alive. A regression keeps Connect blocked until the sleep exits on its own, so
// there the pid is stale by cleanup time -- but that run has already failed the
// elapsed-time assertion and left nothing behind to reap.
func killRecordedMasters(path string) {
	recorded, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for token := range strings.FieldsSeq(string(recorded)) {
		pid, err := strconv.Atoi(token)
		// A pid of 0 or below addresses a process group rather than the sleep, so
		// an unexpected token is skipped instead of signalled.
		if err != nil || pid <= 0 {
			continue
		}
		proc, err := os.FindProcess(pid)
		if err != nil {
			continue
		}
		_ = proc.Kill()
	}
}

// TestConnectDoesNotWaitForControlMaster covers the hang described in #405: the
// daemonized control master inherits the stderr it was handed, so passing
// os/exec an in-memory writer made cmd.Wait block until ControlPersist expired
// even though the connection was already up.
func TestConnectDoesNotWaitForControlMaster(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake ssh client is a POSIX shell script")
	}

	binDir := t.TempDir()
	argvLog := filepath.Join(binDir, "argv.log")
	pidLog := filepath.Join(binDir, "master.pid")
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "ssh"), []byte(fakeSSHScript), 0o755)) //nolint:gosec // the fake client has to be executable
	// Prepend rather than replace: the fake shadows the real ssh while leaving
	// the sleep it backgrounds resolvable.
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("RIG_TEST_ARGV_LOG", argvLog)
	t.Setenv("RIG_TEST_MASTER_PID_LOG", pidLog)
	t.Setenv("RIG_TEST_MASTER_LIFETIME", strconv.Itoa(int(controlMasterLifetime.Seconds())))
	// Reap the stand-in master once Connect has returned; otherwise it lingers for
	// its full lifetime after the test has finished.
	t.Cleanup(func() { killRecordedMasters(pidLog) })

	conn, err := NewConnection(Config{Address: "127.0.0.1"})
	require.NoError(t, err)

	// Long enough not to cut the pre-fix hang short: the point of the test is
	// that Connect returns on its own, not that the context rescues it.
	ctx, cancel := context.WithTimeout(context.Background(), 2*controlMasterLifetime)
	defer cancel()

	start := time.Now()
	require.NoError(t, conn.Connect(ctx))
	elapsed := time.Since(start)

	argv, readErr := os.ReadFile(argvLog)
	require.NoError(t, readErr)
	// Guard against the test passing vacuously: without -f nothing daemonizes,
	// so the hang could no longer be observed here at all.
	require.Contains(t, string(argv), "-N -f", "control master should still be started with -N -f")
	// A missing pid log would make the cleanup a silent no-op and leave the
	// stand-in master running for its full lifetime.
	require.FileExists(t, pidLog, "the fake client should have recorded the stand-in master's pid")
	require.Less(t, elapsed, connectBudget, "Connect waited for the backgrounded control master to exit")
	require.True(t, conn.IsConnected(), "connection should report as established after Connect")
}
