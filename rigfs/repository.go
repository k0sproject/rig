package rigfs

import "github.com/k0sproject/rig/exec"

// DefaultRepository is the default Repository for fsys implementations.
var DefaultRepository = NewRepository()

// Repository is a factory for Fsys implementations
type Repository struct{}

// Get returns Windows or Unix Fsys depending on the OS
func (r *Repository) Get(c exec.Runner) Fsys {
	if c.IsWindows() {
		return NewWindowsFsys(c)
	}
	return NewFsys(c)
}

// NewRepository returns a new Repository
func NewRepository() *Repository {
	return &Repository{}
}
