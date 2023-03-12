// Package fsys provides fs.FS implementations for remote filesystems.
package fsys

import "github.com/k0sproject/rig/exec"

// NewFsys returns a fs.FS implementation for a remote filesystem
func NewFsys(c runner, opts ...exec.Option) Fsys {
	if c.IsWindows() {
		return NewWindowsFsys(c, opts...)
	}
	return NewPosixFsys(c, opts...)
}
