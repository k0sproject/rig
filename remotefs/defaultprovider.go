package remotefs

import (
	"sync"

	"github.com/k0sproject/rig/v2/cmd"
)

// DefaultRegistry is the default Repository for remote filesystem implementations.
var DefaultRegistry = sync.OnceValue(NewDefaultFactory)

// FSProvider is a function that returns a remote filesystem implementation given a runner.
type FSProvider func(cmd.Runner) (FS, error)

// DefaultFactory is a factory for remote filesystem implementations.
type DefaultFactory struct{}

// Get returns Windows or Unix FS depending on the remote OS.
// Currently it never returns an error.
func (r *DefaultFactory) Get(c cmd.Runner) (FS, error) {
	if c.IsWindows() {
		return NewWindowsFS(c), nil
	}
	return NewPosixFS(c), nil
}

// NewDefaultFactory returns a new DefaultFactory.
func NewDefaultFactory() *DefaultFactory {
	return &DefaultFactory{}
}
