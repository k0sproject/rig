package openssh

import (
	"errors"
	"testing"

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
