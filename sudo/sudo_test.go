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
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/k0sproject/rig/v2/sh/shellescape"
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
// aside on descriptors 3 to 5 around script and put back before sudo, the exit
// status relayed on descriptor 6, and signals passed on by both the wrapper and
// the shell script starts.
func TestTTYGolden(t *testing.T) {
	want := `/bin/sh -c -- 'f=$(mktemp) || exit 1; exec 3<&0 4>&1 5>&2 6>"$f" 7<"$f"; rm -f "$f"; p=; c=; trap '"'"'c=1; [ -z "$p" ] || kill -TERM "$p" 2>/dev/null'"'"' INT TERM HUP; SHELL=/bin/sh script -qec '"'"'p=; c=; trap '"'"'"'"'"'"'"'"'c=1; [ -z "$p" ] || kill -TERM "$p" 2>/dev/null'"'"'"'"'"'"'"'"' INT TERM HUP; sudo -n -- /bin/sh -c -- '"'"'"'"'"'"'"'"'id -un'"'"'"'"'"'"'"'"' <&3 >&4 2>&5 3<&- 4>&- 5>&- 6>&- & p=$!; [ -z "$c" ] || kill -TERM "$p" 2>/dev/null; wait "$p"; s=$?; while kill -0 "$p" 2>/dev/null; do wait "$p"; s=$?; done; echo "$s" >&6'"'"' /dev/null </dev/null >&5 7<&- & p=$!; [ -z "$c" ] || kill -TERM "$p" 2>/dev/null; wait "$p"; s=$?; while kill -0 "$p" 2>/dev/null; do wait "$p"; s=$?; done; st=$(cat <&7); case "$st" in '"'"''"'"'|*[!0-9]*) exit 130;; esac; exit "$st"'`
	assert.Equal(t, want, sudo.TTY("id -un"))
}

// fakeSudo stands in for "sudo -n -- CMD": it runs CMD as the current user, so
// TTY can be run without root. With RIG_TEST_INTERRUPT_SCRIPT set it first
// interrupts the script(1) running it, which makes script exit 0 once its child
// is gone, and then waits to be stopped. Its parent is the shell script
// started, whose parent is script. The pid is read from /proc rather than with
// ps, whose BusyBox version lacks the options for it.
const fakeSudo = `#!/bin/sh
[ "$1" = -n ] && shift
[ "$1" = -- ] && shift
if [ -n "${RIG_TEST_INTERRUPT_SCRIPT:-}" ]; then
	kill -INT "$(sed -n 's/^PPid:[[:space:]]*//p' "/proc/$PPID/status")"
	exec sleep 30
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

	t.Run("removing temporary files does not lose the status", func(t *testing.T) {
		tmp := t.TempDir()
		_, _, code := run(t, `rm -f "$TMPDIR"/*; exit 17`, nil, "TMPDIR="+tmp)
		assert.Equal(t, 17, code)
		entries, err := os.ReadDir(tmp)
		require.NoError(t, err)
		assert.Empty(t, entries, "the status file must not be left behind")
	})

	t.Run("an interrupted command is not a success", func(t *testing.T) {
		_, _, code := run(t, "true", nil, "RIG_TEST_INTERRUPT_SCRIPT=1")
		assert.Equal(t, 143, code, "script exits 0 when interrupted, so the status must come from the relay")
	})

	for _, sig := range []os.Signal{os.Interrupt, syscall.SIGTERM} {
		t.Run("a signal to the wrapper stops the command: "+sig.String(), func(t *testing.T) {
			pidFile := filepath.Join(t.TempDir(), "pid")
			// exec, because dash does not exec the last command of -c itself,
			// and the signal is meant for the wrapper rather than this shell.
			c := exec.CommandContext(t.Context(), "/bin/sh", "-c", "exec "+sudo.TTY("echo $$ > "+shellescape.Quote(pidFile)+"; exec sleep 30"))
			c.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"))
			require.NoError(t, c.Start())

			var pid int
			require.Eventually(t, func() bool {
				data, err := os.ReadFile(pidFile)
				if err != nil {
					return false
				}
				pid, err = strconv.Atoi(strings.TrimSpace(string(data)))
				return err == nil
			}, 5*time.Second, 10*time.Millisecond, "the command did not start")

			require.NoError(t, c.Process.Signal(sig))
			err := c.Wait()
			exitErr, ok := errors.AsType[*exec.ExitError](err)
			require.True(t, ok, "a stopped command must not be reported as a success: %v", err)
			assert.Equal(t, 143, exitErr.ExitCode(), "the command's own status, stopped by SIGTERM")
			assert.Eventually(t, func() bool { return !processRunning(pid) }, 5*time.Second, 10*time.Millisecond, "the command outlived the wrapper")
		})
	}
}

// processRunning reports whether pid is a live process. A zombie does not
// count: an orphan is reaped by whatever runs as pid 1, which in a container
// may never do it.
func processRunning(pid int) bool {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false
	}
	// The state follows the command name, which is in parentheses and may
	// itself contain them.
	i := strings.LastIndex(string(data), ") ")
	return i >= 0 && !strings.HasPrefix(string(data)[i+2:], "Z")
}
