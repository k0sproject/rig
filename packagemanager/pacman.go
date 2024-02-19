package packagemanager

import (
	"context"

	"github.com/k0sproject/rig/exec"
)

// NewPacman creates a new pacman package manager.
func NewPacman(c exec.ContextRunner) PackageManager {
	return newUniversalPackageManager(c, "pacman", "pacman", "-S --noconfirm", "-R --noconfirm", "-Syu --noconfirm")
}

// RegisterPacman registers the pacman package manager to a repository.
func RegisterPacman(repository *Provider) {
	repository.Register(func(c exec.ContextRunner) (PackageManager, bool) {
		if c.IsWindows() {
			return nil, false
		}
		if c.ExecContext(context.Background(), "command -v pacman") != nil {
			return nil, false
		}
		return NewPacman(c), true
	})
}
