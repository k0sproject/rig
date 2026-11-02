package sudo_test

import (
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/k0sproject/rig/v2/sudo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// TestTTYGolden pins the command the TTY decorator produces: the streams set
// aside on descriptors 3 to 5 around script and put back before sudo, and the
// exit status relayed on descriptor 6.
func TestTTYGolden(t *testing.T) {
	want := `/bin/sh -c -- '{ st=$(SHELL=/bin/sh script -qec '"'"'sudo -n -- /bin/sh -c -- '"'"'"'"'"'"'"'"'id -un'"'"'"'"'"'"'"'"' <&3 >&4 2>&5 3<&- 4>&- 5>&-; echo $? >&6'"'"' /dev/null 6>&1 </dev/null >&5); } 3<&0 4>&1 5>&2; case "$st" in '"'"''"'"'|*[!0-9]*) exit 130;; esac; exit "$st"'`
	assert.Equal(t, want, sudo.TTY("id -un"))
}

// fakeSudo stands in for "sudo -n -- CMD": it runs CMD as the current user, so
// TTY can be run without root. With RIG_TEST_INTERRUPT_SCRIPT set it first
// interrupts the script(1) running it, as a cancelled session would, before the
// command can report a status. Its parent is the shell script started, whose
// parent is script. The pid is read from /proc rather than with ps, whose
// BusyBox version lacks the options for it.
const fakeSudo = `#!/bin/sh
[ "$1" = -n ] && shift
[ "$1" = -- ] && shift
if [ -n "${RIG_TEST_INTERRUPT_SCRIPT:-}" ]; then
	kill -INT "$(sed -n 's/^PPid:[[:space:]]*//p' "/proc/$PPID/status")"
	sleep 30
fi
exec "$@"
`

// TestTTYExecution runs what TTY produces with the real script(1) and fakeSudo,
// covering the stream handling and the exit status relay.
func TestTTYExecution(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("needs script(1) from util-linux")
	}
	if out, err := exec.Command("script", "-V").CombinedOutput(); err != nil || !strings.Contains(string(out), "util-linux") {
		t.Skip("needs script(1) from util-linux")
	}
	bin := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(bin, "sudo"), []byte(fakeSudo), 0o755)) //nolint:gosec // must be executable

	run := func(t *testing.T, command string, stdin io.Reader, env ...string) (string, string, int) {
		t.Helper()
		c := exec.CommandContext(t.Context(), "/bin/sh", "-c", sudo.TTY(command))
		c.Env = append(os.Environ(), append([]string{"PATH=" + bin + ":" + os.Getenv("PATH")}, env...)...)
		c.Stdin = stdin
		var stdout, stderr bytes.Buffer
		c.Stdout, c.Stderr = &stdout, &stderr
		err := c.Run()
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return stdout.String(), stderr.String(), exitErr.ExitCode()
		}
		require.NoError(t, err)
		return stdout.String(), stderr.String(), 0
	}

	t.Run("exit status and separate streams", func(t *testing.T) {
		stdout, stderr, code := run(t, "echo out; echo err >&2; exit 17", nil)
		assert.Equal(t, 17, code)
		assert.Equal(t, "out\n", stdout)
		assert.Equal(t, "err\n", stderr)
	})

	t.Run("binary stdin reaches the command intact", func(t *testing.T) {
		data := make([]byte, 1<<20)
		_, err := rand.Read(data)
		require.NoError(t, err)
		stdout, _, code := run(t, "cat", bytes.NewReader(data))
		require.Equal(t, 0, code)
		assert.True(t, bytes.Equal(data, []byte(stdout)), "the data must not pass through the terminal")
	})

	t.Run("an interrupted command is not a success", func(t *testing.T) {
		_, _, code := run(t, "true", nil, "RIG_TEST_INTERRUPT_SCRIPT=1")
		assert.Equal(t, 130, code, "script exits 0 when interrupted, so the status must come from the relay")
	})
}
