package remotefs

import "github.com/k0sproject/rig/exec"

// DefaultRepository is the default Repository for remote filesystem implementations.
var DefaultRepository = NewRepository()

// Repository is a factory for remote filesystem implementations
type Repository struct{}

// Get returns Windows or Unix FS depending on the remote OS
func (r *Repository) Get(c exec.Runner) FS {
	if c.IsWindows() {
		return NewWindowsFS(c)
	}
	return NewPosixFS(c)
}

// NewRepository returns a new Repository
func NewRepository() *Repository {
	return &Repository{}
}
