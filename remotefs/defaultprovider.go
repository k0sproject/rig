package remotefs

import "github.com/k0sproject/rig/exec"

// DefaultProvider is the default Repository for remote filesystem implementations.
var DefaultProvider = NewRepository()

// Provider is a factory for remote filesystem implementations
type Provider struct{}

// Get returns Windows or Unix FS depending on the remote OS.
// Currently it never returns an error.
func (r *Provider) Get(c exec.Runner) (FS, error) {
	if c.IsWindows() {
		return NewWindowsFS(c), nil
	}
	return NewPosixFS(c), nil
}

// NewRepository returns a new Repository
func NewRepository() *Provider {
	return &Provider{}
}
