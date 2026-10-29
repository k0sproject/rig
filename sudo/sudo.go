package sudo

import (
	"strings"

	"github.com/k0sproject/rig/v2/cmd"
	"github.com/k0sproject/rig/v2/log"
	"github.com/k0sproject/rig/v2/sh"
)

// requireTTYMessage is how sudo rejects a command when sudoers sets requiretty
// and the session has no terminal, which rig's sessions never have. sudo
// translates it, so the probe runs under the C locale to keep it matchable.
const requireTTYMessage = "must have a tty"

// Sudo is a DecorateFunc that will wrap the given command in a sudo call.
//
// The command runs through an explicit POSIX shell: sudo execs its argument
// directly, so compound expressions (pipes, redirections, boolean lists) need a
// shell to interpret them, and rig's commands are POSIX, which the target
// user's login shell is not guaranteed to be.
func Sudo(cmd string) string {
	return "sudo -n -- " + sh.Shell(cmd)
}

// RegisterSudo registers a sudo DecorateFunc with the given repository.
func RegisterSudo(repository *Registry) {
	repository.Register(func(runner cmd.Runner) (cmd.Runner, bool) {
		if runner.IsWindows() {
			return nil, false
		}
		// Ungated: a CommandGate that rejects this probe would silently
		// disable sudo rather than surface an error, so it must always run.
		if err := runner.Exec("env LC_ALL=C "+Sudo("true"), cmd.Ungated()); err != nil {
			// The registry has no way to carry a reason, so without this the
			// caller only learns that no sudo method was found.
			if strings.Contains(cmd.StderrOf(err), requireTTYMessage) {
				log.GetLogger(runner).Warn("sudo is unusable because sudoers sets requiretty and rig runs commands without a terminal; exempt this user with 'Defaults:<user> !requiretty'", log.ErrorAttr(err))
			}
			return nil, false
		}
		return cmd.NewExecutor(runner, Sudo), true
	})
}
