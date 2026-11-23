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

// openrcLogFilesScript prints the existing stdout and stderr log files of the
// service named by $svc, stdout first. OpenRC has no log location of its own:
// each service script sets one with output_log/error_log or with --stdout and
// --stderr in the supervisor arguments.
//
// The script follows openrc-run of OpenRC 0.63 so that the result matches what
// the supervisor gets, all in a subshell: the OpenRC helper functions, rc.conf,
// conf.d beside the service script for the base and instance names, then the
// service script. A runlevel conf.d file replaces the plain one when it exists.
// OpenRC 0.64 also reads configuration from the RC_PATH roots, which this
// script does not.
//
// For supervise-daemon and start-stop-daemon the arguments go through eval like
// in OpenRC's supervisor modules, so quoting inside them works, and the last
// --stdout, --stderr, --chroot or --chdir wins. The daemons open the log files
// after they change root and directory, so log paths are inside the chroot and
// relative ones start from the service directory. supervise-daemon falls back
// to start_stop_daemon_args only when supervise_daemon_args is unset. s6
// redirects to output_log and error_log from its service directory before it
// enters the chroot, unless a logger command replaces them. runit uses neither
// setting, so it prints nothing.
const openrcLogFilesScript = `_rig_script=$(rc-service -r "$_rig_svc" 2>/dev/null) || _rig_script=$_rig_root/etc/init.d/$_rig_svc
[ -r "$_rig_script" ] || { echo "service script for $_rig_svc not found" >&2; exit 1; }
[ -n "$_rig_runlevel" ] || _rig_runlevel=$(rc-status -r 2>/dev/null)
(
	RC_SVCNAME=$_rig_svc SVCNAME=$_rig_svc RC_SERVICE=$_rig_script RC_RUNLEVEL=$_rig_runlevel
	_rig_load() { [ -r "$1" ] && . "$1" >/dev/null 2>&1 </dev/null; }
	_rig_confd=${_rig_script%/*}/../conf.d
	_rig_names=$_rig_svc
	if [ "${_rig_svc%%.*}" != "$_rig_svc" ]; then _rig_names="${_rig_svc%%.*} $_rig_svc"; fi
	for _rig_d in "$_rig_root/usr/libexec/rc" "$_rig_root/lib/rc"; do
		if [ -r "$_rig_d/sh/functions.sh" ]; then
			_rig_load "$_rig_d/sh/functions.sh"
			_rig_load "$_rig_d/sh/rc-functions.sh"
			break
		fi
	done
	command -v yesno >/dev/null 2>&1 || yesno() {
		case $1 in
			[Yy][Ee][Ss]|[Tt][Rr][Uu][Ee]|[Oo][Nn]|1) return 0 ;;
			''|*[!A-Za-z0-9_]*|[0-9]*) return 1 ;;
		esac
		eval "_rig_v=\${$1}"
		case $_rig_v in [Yy][Ee][Ss]|[Tt][Rr][Uu][Ee]|[Oo][Nn]|1) return 0 ;; esac
		return 1
	}
	_rig_load "$_rig_root/etc/rc.conf"
	for _rig_f in "$_rig_root"/etc/rc.conf.d/*.conf; do _rig_load "$_rig_f"; done
	for _rig_n in $_rig_names; do
		if [ -n "$_rig_runlevel" ] && [ -e "$_rig_confd/$_rig_n.$_rig_runlevel" ]; then
			_rig_load "$_rig_confd/$_rig_n.$_rig_runlevel"
		else
			_rig_load "$_rig_confd/$_rig_n"
		fi
	done
	_rig_load "$_rig_script"
	_rig_out= _rig_err= _rig_chroot= _rig_dir=
	case $supervisor in
		runit) exit 0 ;;
		s6)
			if [ -n "$output_logger" ] && [ -n "$error_logger" ]; then output_logger=; fi
			if [ -z "$output_logger" ]; then _rig_out=$output_log; fi
			if [ -z "$error_logger" ]; then _rig_err=$error_log; fi
			_rig_abs=
			_rig_rel=${RC_SVCDIR:-/run/openrc}/s6-services/${name:-$_rig_svc}
			;;
		*)
			if [ "$supervisor" = supervise-daemon ]; then
				_rig_args=${supervise_daemon_args-${start_stop_daemon_args}}
			else
				_rig_args=$start_stop_daemon_args
			fi
			eval set -- ${chroot:+--chroot} ${chroot} ${directory:+--chdir} ${directory} ${output_log:+--stdout} ${output_log} ${error_log:+--stderr} ${error_log} $_rig_args
			while [ $# -gt 0 ]; do
				case $1 in
					--stdout|-1) [ $# -ge 2 ] || break; _rig_out=$2; shift ;;
					--stderr|-2) [ $# -ge 2 ] || break; _rig_err=$2; shift ;;
					--chroot|-r) [ $# -ge 2 ] || break; _rig_chroot=$2; shift ;;
					--chdir|-d) [ $# -ge 2 ] || break; _rig_dir=$2; shift ;;
					--stdout=*) _rig_out=${1#*=} ;;
					--stderr=*) _rig_err=${1#*=} ;;
					--chroot=*) _rig_chroot=${1#*=} ;;
					--chdir=*) _rig_dir=${1#*=} ;;
					-1?*) _rig_out=${1#-1} ;;
					-2?*) _rig_err=${1#-2} ;;
					-r?*) _rig_chroot=${1#-r} ;;
					-d?*) _rig_dir=${1#-d} ;;
					--) break ;;
				esac
				shift
			done
			case $_rig_dir in /*) ;; *) _rig_dir=/$_rig_dir ;; esac
			_rig_abs=${_rig_chroot%/}
			_rig_rel=$_rig_abs$_rig_dir
			;;
	esac
	for _rig_f in "$_rig_out" "$_rig_err"; do
		if [ -z "$_rig_f" ] || [ "$_rig_f" = /dev/null ]; then continue; fi
		case $_rig_f in
			/*) _rig_f=$_rig_abs$_rig_f ;;
			*) _rig_f=${_rig_rel%/}/$_rig_f ;;
		esac
		if [ -f "$_rig_f" ]; then printf '%s\n' "$_rig_f"; fi
	done
)`

// openrcLogFilesCommand builds the log file lookup for service s. root prefixes
// the OpenRC configuration and library paths and is empty on a real host. An
// empty runlevel is read from rc-status.
func openrcLogFilesCommand(s, root, runlevel string) string {
	return "_rig_svc=" + shellescape.Quote(s) +
		" _rig_root=" + shellescape.Quote(root) +
		" _rig_runlevel=" + shellescape.Quote(runlevel) + "\n" +
		openrcLogFilesScript
}

// logFiles returns the log files of service s, stdout before stderr, without
// duplicates.
func (i OpenRC) logFiles(ctx context.Context, h cmd.ContextRunner, s string) ([]string, error) {
	out, err := h.ExecOutputContext(ctx, openrcLogFilesCommand(s, "", ""))
	if err != nil {
		return nil, fmt.Errorf("failed to find log files for service %s: %w", s, err)
	}
	var files []string
	seen := make(map[string]struct{})
	for f := range strings.SplitSeq(out, "\n") {
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
		if ctx.Err() != nil {
			return nil //nolint:nilerr // context cancellation is the expected stop signal
		}
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
