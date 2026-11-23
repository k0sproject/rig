package initsystem

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/k0sproject/rig/v2/cmd"
	"github.com/k0sproject/rig/v2/sh"
	"github.com/k0sproject/rig/v2/sh/shellescape"
)

// OpenRC is found on some linux systems, often installed on Alpine for example.
type OpenRC struct{}

// String returns the name of the init system.
func (OpenRC) String() string { return "openrc" }

const rcservice = sh.CommandBuilder("rc-service")

var rcserviceCmd = rcservice.Args

// StartService starts a service.
func (i OpenRC) StartService(ctx context.Context, h cmd.ContextRunner, s string) error {
	if err := h.ExecContext(ctx, rcserviceCmd(s, "start").String()); err != nil {
		return fmt.Errorf("failed to start service %s: %w", s, err)
	}
	return nil
}

// StopService stops a service.
func (i OpenRC) StopService(ctx context.Context, h cmd.ContextRunner, s string) error {
	if err := h.ExecContext(ctx, rcserviceCmd(s, "stop").String()); err != nil {
		return fmt.Errorf("failed to stop service %s: %w", s, err)
	}
	return nil
}

// ServiceScriptPath returns the path to a service configuration file.
func (i OpenRC) ServiceScriptPath(ctx context.Context, h cmd.ContextRunner, s string) (string, error) {
	out, err := h.ExecOutputContext(ctx, rcserviceCmd("-r", s).String())
	if err != nil {
		return "", fmt.Errorf("failed to get service script path for %s: %w", s, err)
	}
	return strings.TrimSpace(out), nil
}

// RestartService restarts a service.
func (i OpenRC) RestartService(ctx context.Context, h cmd.ContextRunner, s string) error {
	if err := h.ExecContext(ctx, rcserviceCmd(s, "restart").String()); err != nil {
		return fmt.Errorf("failed to restart service %s: %w", s, err)
	}
	return nil
}

// EnableService enables a service.
func (i OpenRC) EnableService(ctx context.Context, h cmd.ContextRunner, s string) error {
	if err := h.ExecContext(ctx, sh.Command("rc-update", "add", s)); err != nil {
		return fmt.Errorf("failed to enable service %s: %w", s, err)
	}
	return nil
}

// DisableService disables a service.
func (i OpenRC) DisableService(ctx context.Context, h cmd.ContextRunner, s string) error {
	if err := h.ExecContext(ctx, sh.Command("rc-update", "del", s)); err != nil {
		return fmt.Errorf("failed to disable service %s: %w", s, err)
	}
	return nil
}

// ServiceIsRunning returns true if a service is running.
func (i OpenRC) ServiceIsRunning(ctx context.Context, h cmd.ContextRunner, s string) bool {
	return h.ExecContext(ctx, rcserviceCmd(s, "status").Pipe("grep", "-q", "status: started").String()) == nil
}

// ServiceEnvironmentPath returns a path to an environment override file path.
func (i OpenRC) ServiceEnvironmentPath(_ context.Context, _ cmd.ContextRunner, s string) (string, error) {
	return path.Join("/etc/conf.d", s), nil
}

// ServiceEnvironmentContent returns a formatted string for a service environment override file.
func (i OpenRC) ServiceEnvironmentContent(env map[string]string) string {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	b.Grow(len(env) * 24)
	for _, k := range keys {
		b.WriteString("export ")
		b.WriteString(shellescape.Quote(k + "=" + env[k]))
		b.WriteByte('\n')
	}
	return b.String()
}

// openrcLogFilesScript prints the existing log files of the service named by
// $svc, one per line. OpenRC has no log location of its own: each service script
// sets one with output_log/error_log or with --stdout/--stderr in the supervisor
// arguments. The conf.d file and the service script are sourced in a subshell
// the way openrc-run does it, so values derived at load time, such as the
// binary name in the k0s script, resolve the same way.
const openrcLogFilesScript = `script=$(rc-service -r "$svc" 2>/dev/null) || script=/etc/init.d/$svc
[ -r "$script" ] || { echo "service script for $svc not found" >&2; exit 1; }
(
	RC_SVCNAME=$svc
	SVCNAME=$svc
	if [ -r "/etc/conf.d/$svc" ]; then . "/etc/conf.d/$svc" >/dev/null 2>&1 </dev/null; fi
	. "$script" >/dev/null 2>&1 </dev/null
	set -f
	{
		printf '%s\n' "$output_log" "$error_log"
		next=
		for a in $supervise_daemon_args $start_stop_daemon_args; do
			if [ -n "$next" ]; then printf '%s\n' "$a"; next=; continue; fi
			case $a in
				--stdout|--stderr|-1|-2) next=1 ;;
				--stdout=*|--stderr=*) printf '%s\n' "${a#*=}" ;;
			esac
		done
	} | while IFS= read -r f; do
		if [ -n "$f" ] && [ "$f" != /dev/null ] && [ -f "$f" ]; then printf '%s\n' "$f"; fi
	done
)`

// logFiles returns the log files of service s in the order the service script
// declares them, stdout before stderr, without duplicates.
func (i OpenRC) logFiles(ctx context.Context, h cmd.ContextRunner, s string) ([]string, error) {
	out, err := h.ExecOutputContext(ctx, "svc="+shellescape.Quote(s)+"\n"+openrcLogFilesScript)
	if err != nil {
		return nil, fmt.Errorf("failed to find log files for service %s: %w", s, err)
	}
	var files []string
	seen := make(map[string]struct{})
	for f := range strings.SplitSeq(out, "\n") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if _, ok := seen[f]; ok {
			continue
		}
		seen[f] = struct{}{}
		files = append(files, f)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("service %s does not log to a file: %w", s, errors.ErrUnsupported)
	}
	return files, nil
}

// ServiceLogs returns the latest log lines of a service. The log files come from
// the service script. When the script sends stdout and stderr to separate files,
// the result holds that many lines from each file, stdout first. Services that
// log only to syslog return an error that wraps [errors.ErrUnsupported].
func (i OpenRC) ServiceLogs(ctx context.Context, h cmd.ContextRunner, s string, lines int) ([]string, error) {
	files, err := i.logFiles(ctx, h, s)
	if err != nil {
		return nil, err
	}
	args := append([]string{"-q", "-n", strconv.Itoa(lines), "--"}, files...)
	out, err := h.ExecOutputContext(ctx, sh.Command("tail", args...))
	if err != nil {
		return nil, fmt.Errorf("failed to get logs for service %s: %w", s, err)
	}
	return strings.Split(out, "\n"), nil
}

// StreamServiceLogs streams new lines from the log files of a service to w until
// ctx is cancelled. The log files come from the service script, as in
// [OpenRC.ServiceLogs]. Lines from separate stdout and stderr files are written
// as they arrive, so their relative order is approximate.
func (i OpenRC) StreamServiceLogs(ctx context.Context, h cmd.ContextRunner, s string, w io.Writer) error {
	files, err := i.logFiles(ctx, h, s)
	if err != nil {
		return err
	}
	args := append([]string{"-q", "-n", "0", "-F", "--"}, files...)
	return streamToWriter(ctx, h, s, sh.Command("tail", args...), w)
}

// RegisterOpenRC registers OpenRC to a repository.
func RegisterOpenRC(repo *Registry) {
	repo.Register(func(c cmd.ContextRunner) (ServiceManager, bool) {
		if c.IsWindows() {
			return nil, false
		}
		if c.ExecContext(context.Background(), `command -v openrc-init > /dev/null 2>&1 || (stat /etc/inittab > /dev/null 2>&1 && (grep ::sysinit: /etc/inittab | grep -q openrc) )`) != nil {
			return nil, false
		}
		return &OpenRC{}, true
	})
}
