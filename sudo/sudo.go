package sudo

import (
	"github.com/k0sproject/rig/v2/cmd"
	"github.com/k0sproject/rig/v2/sh/shellescape"
)

// Sudo is a DecorateFunc that will wrap the given command in a sudo call.
func Sudo(cmd string) string {
	return `sudo -n -- "${SHELL-sh}" -c ` + shellescape.Quote(cmd)
}

// RegisterSudo registers a sudo DecorateFunc with the given repository.
func RegisterSudo(repository *Registry) {
	repository.Register(func(c cmd.Runner) (cmd.Runner, bool) {
		if c.IsWindows() {
			return nil, false
		}
		// Ungated: a CommandGate that rejects this probe would silently
		// disable sudo rather than surface an error, so it must always run.
		if c.Exec(Sudo("true"), cmd.Ungated()) != nil {
			return nil, false
		}
		return cmd.NewExecutor(c, Sudo), true
	})
}
