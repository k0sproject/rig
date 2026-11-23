package initsystem

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestOpenRCLogFilesScript runs the log file lookup under the local sh against
// a temporary OpenRC configuration tree.
func TestOpenRCLogFilesScript(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the lookup runs on OpenRC hosts only")
	}

	type file struct{ path, content string }
	cases := []struct {
		name     string
		svc      string
		runlevel string
		files    []file
		logs     []string
		want     []string
	}{
		{
			name: "k0s supervise-daemon script",
			svc:  "k0scontroller",
			files: []file{
				{"bin/k0s", ""},
				{"init.d/k0scontroller", `supervisor=supervise-daemon
command=$ROOT/bin/k0s
name=$(basename $(readlink -f $command))
supervise_daemon_args="--stdout $ROOT/log/${name}.log --stderr $ROOT/log/${name}.err"
echo noise
`},
			},
			logs: []string{"k0s.log", "k0s.err"},
			want: []string{"k0s.log", "k0s.err"},
		},
		{
			name: "quoted path with a space",
			svc:  "quoted",
			files: []file{{"init.d/quoted", `supervisor=supervise-daemon
supervise_daemon_args="--stdout \"$ROOT/log/my svc.log\""
`}},
			logs: []string{"my svc.log"},
			want: []string{"my svc.log"},
		},
		{
			name: "stdout comes first regardless of flag order",
			svc:  "order",
			files: []file{{"init.d/order", `supervisor=supervise-daemon
supervise_daemon_args="--stderr=$ROOT/log/order.err -1 $ROOT/log/order.log"
`}},
			logs: []string{"order.log", "order.err"},
			want: []string{"order.log", "order.err"},
		},
		{
			name: "supervise-daemon ignores start_stop_daemon_args when its own are set",
			svc:  "sd",
			files: []file{{"init.d/sd", `supervisor=supervise-daemon
supervise_daemon_args=""
start_stop_daemon_args="--stdout $ROOT/log/stale.log"
`}},
			logs: []string{"stale.log"},
		},
		{
			name: "supervise-daemon falls back to start_stop_daemon_args when unset",
			svc:  "sdfb",
			files: []file{{"init.d/sdfb", `supervisor=supervise-daemon
start_stop_daemon_args="--stdout $ROOT/log/fb.log"
`}},
			logs: []string{"fb.log"},
			want: []string{"fb.log"},
		},
		{
			name: "start-stop-daemon ignores supervise_daemon_args and the last flag wins",
			svc:  "ssd",
			files: []file{{"init.d/ssd", `output_log=$ROOT/log/first.log
error_log=$ROOT/log/ssd.err
supervise_daemon_args="--stdout $ROOT/log/other.log"
start_stop_daemon_args="--stdout $ROOT/log/last.log"
`}},
			logs: []string{"first.log", "other.log", "last.log", "ssd.err"},
			want: []string{"last.log", "ssd.err"},
		},
		{
			name: "instance inherits the base conf.d",
			svc:  "worker.one",
			files: []file{
				{"init.d/worker.one", "command=/bin/true\n"},
				{"conf.d/worker", "output_log=$ROOT/log/worker.log\n"},
			},
			logs: []string{"worker.log"},
			want: []string{"worker.log"},
		},
		{
			name:     "runlevel conf.d replaces the plain one",
			svc:      "rl",
			runlevel: "default",
			files: []file{
				{"init.d/rl", "command=/bin/true\n"},
				{"conf.d/rl", "output_log=$ROOT/log/plain.log\n"},
				{"conf.d/rl.default", "error_log=$ROOT/log/rl.err\n"},
			},
			logs: []string{"plain.log", "rl.err"},
			want: []string{"rl.err"},
		},
		{
			name: "rc.conf applies to every service",
			svc:  "global",
			files: []file{
				{"init.d/global", "command=/bin/true\n"},
				{"rc.conf", "error_log=$ROOT/log/global.err\n"},
			},
			logs: []string{"global.err"},
			want: []string{"global.err"},
		},
		{
			name: "s6 supervisor keeps its own logs",
			svc:  "s6svc",
			files: []file{{"init.d/s6svc", `supervisor=s6
output_log=$ROOT/log/s6.log
`}},
			logs: []string{"s6.log"},
		},
		{
			name:  "missing and /dev/null files are skipped",
			svc:   "nolog",
			files: []file{{"init.d/nolog", "output_log=/dev/null\nerror_log=$ROOT/log/absent.err\n"}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Join(root, "log"), 0o755))
			for _, f := range tc.files {
				p := filepath.Join(root, f.path)
				require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
				require.NoError(t, os.WriteFile(p, []byte(f.content), 0o600))
			}
			for _, l := range tc.logs {
				require.NoError(t, os.WriteFile(filepath.Join(root, "log", l), nil, 0o600))
			}

			c := exec.Command("sh", "-c", openrcLogFilesCommand(tc.svc, root, tc.runlevel))
			c.Env = append(os.Environ(), "ROOT="+root)
			out, err := c.Output()
			require.NoError(t, err)

			var want []string
			for _, w := range tc.want {
				want = append(want, filepath.Join(root, "log", w))
			}
			var got []string
			if trimmed := strings.TrimSuffix(string(out), "\n"); trimmed != "" {
				got = strings.Split(trimmed, "\n")
			}
			require.Equal(t, want, got)
		})
	}

	t.Run("missing service script is an error", func(t *testing.T) {
		c := exec.Command("sh", "-c", openrcLogFilesCommand("absent", t.TempDir(), ""))
		out, err := c.CombinedOutput()
		require.Error(t, err)
		require.Contains(t, string(out), "service script for absent not found")
	})
}
