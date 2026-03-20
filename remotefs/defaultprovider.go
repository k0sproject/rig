package remotefs

import (
	"sync"

	"github.com/k0sproject/rig/v2/cmd"
)

// DefaultRegistry is the default Repository for remote filesystem implementations.
var DefaultRegistry = sync.OnceValue(NewRegistry)

// RemoteFSFactory is a factory for remote filesystem implementations.
type RemoteFSFactory interface {
	Get(runner cmd.Runner) (FS, error)
}

// Registry is a factory for remote filesystem implementations.
type Registry struct{}

// Get returns Windows or Unix FS depending on the remote OS.
// Currently it never returns an error.
func (r *Registry) Get(c cmd.Runner) (FS, error) {
	if c.IsWindows() {
		return NewWindowsFS(c), nil
	}
	return NewPosixFS(c), nil
}

// NewRegistry returns a new Registry.
func NewRegistry() *Registry {
	return &Registry{}
}
