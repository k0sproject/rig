package remotefs

import (
	"errors"
	"sync"

	"github.com/k0sproject/rig/v2/cmd"
	"github.com/k0sproject/rig/v2/plumbing"
)

var (
	// DefaultRegistry is the default registry of remote filesystem implementations.
	DefaultRegistry = sync.OnceValue(func() *Registry {
		r := NewRegistry()
		RegisterWindows(r)
		RegisterPosix(r)
		return r
	})

	// ErrNoFS is returned when no supported remote filesystem implementation is found.
	ErrNoFS = errors.New("no supported remote filesystem implementation found")
)

// FSProvider is a function that returns a remote filesystem implementation given a runner.
type FSProvider func(cmd.Runner) (FS, error)

// Factory is a type alias for plumbing.Factory specialized for FS.
type Factory = plumbing.Factory[cmd.Runner, FS]

// Registry is a type alias for plumbing.Provider specialized for FS.
type Registry = plumbing.Provider[cmd.Runner, FS]

// NewRegistry creates a new Registry.
func NewRegistry() *Registry {
	return plumbing.NewProvider[cmd.Runner, FS](ErrNoFS)
}

// RegisterWindows registers the Windows filesystem implementation.
func RegisterWindows(r *Registry) {
	r.Register(func(c cmd.Runner) (FS, bool) {
		if !c.IsWindows() {
			return nil, false
		}
		return NewWindowsFS(c), true
	})
}

// RegisterPosix registers the POSIX filesystem implementation.
func RegisterPosix(r *Registry) {
	r.Register(func(c cmd.Runner) (FS, bool) {
		if c.IsWindows() {
			return nil, false
		}
		return NewPosixFS(c), true
	})
}
