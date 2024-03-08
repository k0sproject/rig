package sudo

import (
	"github.com/k0sproject/rig/v2/cmd"
)

// Noop is a DecorateFunc that will return the given command unmodified.
func Noop(cmd string) string {
	return cmd
}

// RegisterUID0Noop registers a noop DecorateFunc with the given repository which can be used when the user is root.
func RegisterUID0Noop(repository *Provider) {
	repository.Register(func(c cmd.Runner) (cmd.Runner, bool) {
		if c.IsWindows() {
			return nil, false
		}
		if c.Exec(`[ "$(id -u)" = 0 ]`) != nil {
			return nil, false
		}
		return cmd.NewExecutor(c, Noop), true
	})
}
