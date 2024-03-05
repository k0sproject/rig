package sudo

import (
	"github.com/k0sproject/rig/cmd"
	"github.com/k0sproject/rig/sh/shellescape"
)

// Doas is a DecorateFunc that will wrap the given command in a doas call.
func Doas(cmd string) string {
	return `doas -n -- "${SHELL-sh}" -c ` + shellescape.Quote(cmd)
}

// RegisterDoas registers a doas DecorateFunc with the given repository.
func RegisterDoas(repository *Provider) {
	repository.Register(func(c cmd.Runner) (cmd.Runner, bool) {
		if c.IsWindows() {
			return nil, false
		}
		if c.Exec(Doas("true")) != nil {
			return nil, false
		}
		return cmd.NewExecutor(c, Doas), true
	})
}
