package sudo

import (
	"github.com/k0sproject/rig/exec"
	"github.com/k0sproject/rig/sh/shellescape"
)

// Sudo is a DecorateFunc that will wrap the given command in a sudo call.
func Sudo(cmd string) string {
	return `sudo -n -- "${SHELL-sh}" -c ` + shellescape.Quote(cmd)
}

// RegisterSudo registers a sudo DecorateFunc with the given repository.
func RegisterSudo(repository *Provider) {
	repository.Register(func(c exec.Runner) (exec.Runner, bool) {
		if c.IsWindows() {
			return nil, false
		}
		if c.Exec(Sudo("true")) != nil {
			return nil, false
		}
		return exec.NewHostRunner(c, Sudo), true
	})
}
