package sudo

import (
	"github.com/alessio/shellescape"
	"github.com/k0sproject/rig/exec"
)

// Doas is a DecorateFunc that will wrap the given command in a doas call.
func Doas(cmd string) string {
	return `doas -n -- "${SHELL-sh}" -c ` + shellescape.Quote(cmd)
}

// RegisterDoas registers a doas DecorateFunc with the given repository.
func RegisterDoas(repository *Provider) {
	repository.Register(func(c exec.SimpleRunner) exec.DecorateFunc {
		if c.IsWindows() {
			return nil
		}
		if c.Exec(Doas("true")) != nil {
			return nil
		}
		return Sudo
	})
}
