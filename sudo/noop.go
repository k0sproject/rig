package sudo

import (
	"github.com/k0sproject/rig/v2/cmd"
)

// Noop is a DecorateFunc that will return the given command unmodified.
func Noop(cmd string) string {
	return cmd
}

// RegisterUID0Noop registers a noop DecorateFunc with the given repository which can be used when the user is root.
func RegisterUID0Noop(repository *Registry) {
	repository.Register(func(c cmd.Runner) (cmd.Runner, bool) {
		if c.IsWindows() {
			return nil, false
		}
		// Ungated: a CommandGate that rejects this probe would silently change
		// sudo detection, so it must always run.
		if c.Exec(`[ "$(id -u)" = 0 ]`, cmd.Ungated()) != nil {
			return nil, false
		}
		return cmd.NewExecutor(c, Noop), true
	})
}
