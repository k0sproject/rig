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
	bin := openrcStubBin(t)

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
				{"etc/init.d/k0scontroller", `supervisor=supervise-daemon
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
			files: []file{{"etc/init.d/quoted", `supervisor=supervise-daemon
supervise_daemon_args="--stdout \"$ROOT/log/my svc.log\""
`}},
			logs: []string{"my svc.log"},
			want: []string{"my svc.log"},
		},
		{
			name: "stdout comes first regardless of flag order",
			svc:  "order",
			files: []file{{"etc/init.d/order", `supervisor=supervise-daemon
supervise_daemon_args="--stderr=$ROOT/log/order.err -1 $ROOT/log/order.log"
`}},
			logs: []string{"order.log", "order.err"},
			want: []string{"order.log", "order.err"},
		},
		{
			name: "supervise-daemon ignores start_stop_daemon_args when its own are set",
			svc:  "sd",
			files: []file{{"etc/init.d/sd", `supervisor=supervise-daemon
supervise_daemon_args=""
start_stop_daemon_args="--stdout $ROOT/log/stale.log"
`}},
			logs: []string{"stale.log"},
		},
		{
			name: "supervise-daemon falls back to start_stop_daemon_args when unset",
			svc:  "sdfb",
			files: []file{{"etc/init.d/sdfb", `supervisor=supervise-daemon
start_stop_daemon_args="--stdout $ROOT/log/fb.log"
`}},
			logs: []string{"fb.log"},
			want: []string{"fb.log"},
		},
		{
			name: "start-stop-daemon ignores supervise_daemon_args and the last flag wins",
			svc:  "ssd",
			files: []file{{"etc/init.d/ssd", `output_log=$ROOT/log/first.log
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
				{"etc/init.d/worker.one", "command=/bin/true\n"},
				{"etc/conf.d/worker", "output_log=$ROOT/log/worker.log\n"},
			},
			logs: []string{"worker.log"},
			want: []string{"worker.log"},
		},
		{
			name:     "runlevel conf.d replaces the plain one",
			svc:      "rl",
			runlevel: "default",
			files: []file{
				{"etc/init.d/rl", "command=/bin/true\n"},
				{"etc/conf.d/rl", "output_log=$ROOT/log/plain.log\n"},
				{"etc/conf.d/rl.default", "error_log=$ROOT/log/rl.err\n[ \"$MODE\" = debug ] && debug=yes\n"},
			},
			logs: []string{"plain.log", "rl.err"},
			want: []string{"rl.err"},
		},
		{
			name: "rc.conf applies to every service",
			svc:  "global",
			files: []file{
				{"etc/init.d/global", "command=/bin/true\n"},
				{"etc/rc.conf", "error_log=$ROOT/log/global.err\n"},
			},
			logs: []string{"global.err"},
			want: []string{"global.err"},
		},
		{
			name: "s6 redirects to output_log and error_log",
			svc:  "s6svc",
			files: []file{{"etc/init.d/s6svc", `supervisor=s6
chroot=$ROOT/jail
output_log=$ROOT/log/s6.log
error_log=$ROOT/log/s6.err
s6_args="--stdout $ROOT/log/ignored.log"
`}},
			logs: []string{"s6.log", "s6.err", "ignored.log"},
			want: []string{"s6.log", "s6.err"},
		},
		{
			name: "s6 loggers replace the log files",
			svc:  "s6logger",
			files: []file{{"etc/init.d/s6logger", `supervisor=s6
output_logger="logger -t s6"
error_logger="logger -t s6"
output_log=$ROOT/log/s6out.log
error_log=$ROOT/log/s6err.log
`}},
			logs: []string{"s6out.log", "s6err.log"},
			want: []string{"s6out.log"},
		},
		{
			name: "runit does not use output_log",
			svc:  "runitsvc",
			files: []file{{"etc/init.d/runitsvc", `supervisor=runit
output_log=$ROOT/log/runit.log
`}},
			logs: []string{"runit.log"},
		},
		{
			name: "attached short option values",
			svc:  "attached",
			files: []file{{"etc/init.d/attached", `supervisor=supervise-daemon
supervise_daemon_args="-1$ROOT/log/att.log -2$ROOT/log/att.err"
`}},
			logs: []string{"att.log", "att.err"},
			want: []string{"att.log", "att.err"},
		},
		{
			name: "log paths are inside the chroot variable",
			svc:  "jailed",
			files: []file{{"etc/init.d/jailed", `supervisor=supervise-daemon
chroot=$ROOT/log/jail/
output_log=/svc.log
error_log=/dev/null
`}},
			logs: []string{"svc.log", "jail/svc.log"},
			want: []string{"jail/svc.log"},
		},
		{
			name: "chroot in the supervisor arguments overrides the variable",
			svc:  "rejailed",
			files: []file{{"etc/init.d/rejailed", `chroot=$ROOT/log/old
output_log=/svc.log
start_stop_daemon_args="-r$ROOT/log/new"
`}},
			logs: []string{"old/svc.log", "new/svc.log"},
			want: []string{"new/svc.log"},
		},
		{
			name: "relative log paths start from the service directory",
			svc:  "reldir",
			files: []file{{"etc/init.d/reldir", `supervisor=supervise-daemon
directory=$ROOT/log/old
output_log=svc.log
supervise_daemon_args="--chdir $ROOT/log/work"
`}},
			logs: []string{"old/svc.log", "work/svc.log"},
			want: []string{"work/svc.log"},
		},
		{
			name: "relative log paths are inside the chroot",
			svc:  "reljail",
			files: []file{{"etc/init.d/reljail", `chroot=$ROOT/log/jail
start_stop_daemon_args="-d/work -2err.log"
`}},
			logs: []string{"jail/work/err.log", "jailerr.log"},
			want: []string{"jail/work/err.log"},
		},
		{
			name: "s6 relative log paths start from its service directory",
			svc:  "s6rel",
			files: []file{{"etc/init.d/s6rel", `supervisor=s6
name=s6name
directory=/elsewhere
chroot=/jail
output_log=out.log
`}},
			logs: []string{"svc/s6-services/s6name/out.log"},
			want: []string{"svc/s6-services/s6name/out.log"},
		},
		{
			name:  "an empty output_log does not swallow error_log",
			svc:   "emptyout",
			files: []file{{"etc/init.d/emptyout", "output_log=\"\"\nerror_log=$ROOT/log/emptyout.err\n"}},
			logs:  []string{"emptyout.err"},
			want:  []string{"emptyout.err"},
		},
		{
			name: "yesno works without the OpenRC helper library",
			svc:  "cond",
			files: []file{
				{"etc/conf.d/cond", "log_enabled=yes\n"},
				{"etc/init.d/cond", `if yesno "$log_enabled"; then output_log=$ROOT/log/cond.log; fi
yesno log_enabled && error_log=$ROOT/log/cond.err
yesno "no" && output_log=$ROOT/log/wrong.log
`},
			},
			logs: []string{"cond.log", "cond.err", "wrong.log"},
			want: []string{"cond.log", "cond.err"},
		},
		{
			name: "OpenRC helper library is loaded before configuration",
			svc:  "helper",
			files: []file{
				{"lib/rc/sh/functions.sh", "log_dir() { echo \"$ROOT/log\"; }\n"},
				{"etc/conf.d/helper", "output_log=$(log_dir)/helper.log\n"},
				{"etc/init.d/helper", "command=/bin/true\n"},
			},
			logs: []string{"helper.log"},
			want: []string{"helper.log"},
		},
		{
			name:  "missing and /dev/null files are skipped",
			svc:   "nolog",
			files: []file{{"etc/init.d/nolog", "output_log=/dev/null\nerror_log=$ROOT/log/absent.err\n"}},
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
				p := filepath.Join(root, "log", l)
				require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
				require.NoError(t, os.WriteFile(p, nil, 0o600))
			}

			out, err := openrcTestLookup(bin, root, tc.svc, tc.runlevel).Output()
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
		out, err := openrcTestLookup(bin, t.TempDir(), "absent", "").CombinedOutput()
		require.Error(t, err)
		require.Contains(t, string(out), "service script for absent not found")
	})
}

// openrcStubBin writes failing rc-service and rc-status commands into a new
// directory. Putting it first in PATH keeps the lookup away from the OpenRC
// installation of the host running the test.
func openrcStubBin(t *testing.T) string {
	t.Helper()
	bin := t.TempDir()
	for _, name := range []string{"rc-service", "rc-status"} {
		require.NoError(t, os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\nexit 1\n"), 0o755)) //nolint:gosec // the stub must be executable
	}
	return bin
}

// openrcTestLookup prepares the lookup for service svc under root, with the
// stubs from bin first in PATH.
func openrcTestLookup(bin, root, svc, runlevel string) *exec.Cmd {
	c := exec.Command("sh", "-c", openrcLogFilesCommand(svc, root, runlevel))
	c.Env = append(os.Environ(), "ROOT="+root, "RC_SVCDIR="+filepath.Join(root, "log", "svc"), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return c
}
