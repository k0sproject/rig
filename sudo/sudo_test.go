package sudo_test

import (
	"testing"

	"github.com/k0sproject/rig/v2/sudo"
	"github.com/stretchr/testify/assert"
)

// TestSudoGolden pins the exact command strings produced by the Sudo decorator
// for realistic fixtures. This ensures that changes to quoting or the sudo
// invocation template are caught before release.
func TestSudoGolden(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "simple command with flags",
			input: "apt-get install -y docker.io",
			want:  `sudo -n -- /bin/sh -c -- 'apt-get install -y docker.io'`,
		},
		{
			name:  "command with embedded single-quoted argument",
			input: "bash -c 'systemctl restart k0s'",
			want:  "sudo -n -- /bin/sh -c -- 'bash -c '\"'\"'systemctl restart k0s'\"'\"''",
		},
		{
			name:  "plain path without special characters",
			input: "cat /etc/k0s/k0s.yaml",
			want:  `sudo -n -- /bin/sh -c -- 'cat /etc/k0s/k0s.yaml'`,
		},
		{
			name:  "path with spaces requires quoting",
			input: "cat \"/etc/k0s/my config/k0s.yaml\"",
			want:  `sudo -n -- /bin/sh -c -- 'cat "/etc/k0s/my config/k0s.yaml"'`,
		},
		{
			name:  "k0s install command with full config path",
			input: "k0s install controller --config /etc/k0s/k0s.yaml",
			want:  `sudo -n -- /bin/sh -c -- 'k0s install controller --config /etc/k0s/k0s.yaml'`,
		},
		{
			name:  "write-file compound with a redirect stays inside the sudo shell",
			input: "mkdir -p -- /etc/k0s && umask 0177 && cat >/etc/k0s/k0s.yaml && chmod -- 0600 /etc/k0s/k0s.yaml",
			want:  `sudo -n -- /bin/sh -c -- 'mkdir -p -- /etc/k0s && umask 0177 && cat >/etc/k0s/k0s.yaml && chmod -- 0600 /etc/k0s/k0s.yaml'`,
		},
		{
			name:  "command that already uses shell pipes",
			input: "journalctl -u k0s | tail -20",
			want:  `sudo -n -- /bin/sh -c -- 'journalctl -u k0s | tail -20'`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sudo.Sudo(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}
