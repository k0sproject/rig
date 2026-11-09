package sudo

import (
	"errors"
	"fmt"
	"strings"

	"github.com/k0sproject/rig/v2/cmd"
	"github.com/k0sproject/rig/v2/log"
	"github.com/k0sproject/rig/v2/sh"
	"github.com/k0sproject/rig/v2/sh/shellescape"
)

// requireTTYMessage is how sudo rejects a command when sudoers sets requiretty
// and the session has no terminal. Commands run through Exec, this probe and
// the sudo-wrapped commands after it among them, get none; only
// ExecInteractive asks for one. sudo translates the message, so the probe runs
// under the C locale to keep it matchable.
const requireTTYMessage = "must have a tty"

// errUnexpectedProbeOutput is returned by a TTY probe whose output was not
// its input followed by uid 0.
var errUnexpectedProbeOutput = errors.New("unexpected sudo probe output")

// ttyProbeInput is what the TTY probe sends on stdin. The probe passes
// when it comes back on stdout followed by uid 0, which shows both that the
// command ran as root and that the streams reach it intact.
const ttyProbeInput = "rig-sudo-tty-probe"

// Sudo is a DecorateFunc that will wrap the given command in a sudo call.
//
// The command runs through an explicit POSIX shell: sudo execs its argument
// directly, so compound expressions (pipes, redirections, boolean lists) need a
// shell to interpret them, and rig's commands are POSIX, which the target
// user's login shell is not guaranteed to be.
func Sudo(cmd string) string {
	return "sudo -n -- " + sh.Shell(cmd)
}

// TTY is a DecorateFunc for hosts whose sudoers sets requiretty, which
// makes sudo refuse to run without a terminal. It runs the Sudo command inside
// script(1) from util-linux, which gives sudo the controlling terminal that
// requiretty checks for.
//
// The command itself keeps the caller's stdin, stdout and stderr: they are set
// aside as descriptors 3 to 5 before script starts and put back just before
// sudo runs, so no data passes through the terminal. Binary input, end of
// input and separate stderr all work as they do with Sudo. The command's exit
// status comes back on descriptor 6 rather than as script's own, which is 0
// when script is interrupted; a command that reports no status exits 130.
// Descriptor 6 writes to a temporary file that the wrapper reads back through
// descriptor 7. The file is removed as soon as both are open, so nothing the
// command does to temporary files can lose the status.
//
// A signal sent to the wrapper, as one is when a command's context ends,
// stops the command as it does with Sudo, but not on its own: script puts sudo
// in a session of its own, and sudo does not pass on the hangup it gets when
// script exits. The wrapper passes SIGINT, SIGTERM and SIGHUP on to script as
// SIGTERM, script passes that to the shell it starts, the shell to sudo and
// sudo to the command. To pass signals on, the wrapper and that shell run
// their child in the background, and a POSIX shell starts a background
// command with SIGINT and SIGQUIT ignored, so unlike with Sudo the command
// starts with both ignored. A command whose connection is lost without a
// signal keeps running, as it does with Sudo.
//
// The whole wrapper runs under sh.DefaultShell, like the command does.
func TTY(command string) string {
	inner := forwardSignals(Sudo(command)+" <&3 >&4 2>&5 3<&- 4>&- 5>&- 6>&-") + `; echo "$s" >&6`
	outer := `f=$(mktemp) || exit 1; exec 3<&0 4>&1 5>&2 6>"$f" 7<"$f"; rm -f "$f"; ` +
		forwardSignals("SHELL="+shellescape.Quote(sh.DefaultShell)+" script -qec "+shellescape.Quote(inner)+` /dev/null </dev/null >&5 7<&-`) +
		`; st=$(cat <&7); ` +
		`case "$st" in ''|*[!0-9]*) exit 130;; esac; exit "$st"`
	return sh.Shell(outer)
}

// forwardSignals returns a shell snippet that runs command in the background
// and waits for it, passing SIGINT, SIGTERM and SIGHUP on to it as SIGTERM.
// The snippet leaves the command's exit status in $s.
//
// A signal that arrives before the command's pid is known is recorded in c
// and passed on once it is. wait returns early when a trap runs, so it is
// repeated until the command is gone.
func forwardSignals(command string) string {
	return `p=; c=; trap 'c=1; [ -z "$p" ] || kill -TERM "$p" 2>/dev/null' INT TERM HUP; ` +
		command + ` & p=$!; [ -z "$c" ] || kill -TERM "$p" 2>/dev/null; ` +
		`wait "$p"; s=$?; while kill -0 "$p" 2>/dev/null; do wait "$p"; s=$?; done`
}

// RegisterSudo registers a sudo DecorateFunc with the given repository.
//
// When sudoers sets requiretty, the plain probe fails and the factory falls
// back to TTY, if script(1) works on the host.
func RegisterSudo(repository *Registry) {
	repository.Register(func(runner cmd.Runner) (cmd.Runner, bool) {
		if runner.IsWindows() {
			return nil, false
		}
		// Ungated: a CommandGate that rejects this probe would silently
		// disable sudo rather than surface an error, so it must always run.
		err := runner.Exec("env LC_ALL=C "+Sudo("true"), cmd.Ungated())
		if err == nil {
			return cmd.NewExecutor(runner, Sudo), true
		}
		if !strings.Contains(cmd.StderrOf(err), requireTTYMessage) {
			return nil, false
		}
		if ttyErr := probeTTY(runner); ttyErr != nil {
			// The registry has no way to carry a reason, so without this the
			// caller only learns that no sudo method was found.
			log.GetLogger(runner).Warn("sudo is unusable because sudoers sets requiretty and commands run without a terminal, and providing one through script(1) failed; exempt this user with 'Defaults:<user> !requiretty' or install util-linux", log.ErrorAttr(err), "script_error", ttyErr)
			return nil, false
		}
		log.GetLogger(runner).Warn("sudoers sets requiretty, so sudo commands run inside script(1) to get a terminal; exempting this user with 'Defaults:<user> !requiretty' runs sudo directly")
		return cmd.NewExecutor(runner, TTY), true
	})
}

// probeTTY checks that TTY works on the host: the command runs as root
// and its stdin and stdout pass through.
func probeTTY(runner cmd.Runner) error {
	out, err := runner.ExecOutput(TTY("cat; echo; id -u"), cmd.Stdin(strings.NewReader(ttyProbeInput)), cmd.Ungated())
	if err != nil {
		return err //nolint:wrapcheck // logged with its own context by the caller
	}
	if fields := strings.Fields(out); len(fields) != 2 || fields[0] != ttyProbeInput || fields[1] != "0" {
		return fmt.Errorf("%w: %q", errUnexpectedProbeOutput, out)
	}
	return nil
}
