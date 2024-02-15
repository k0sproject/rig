// Package remotefs provides fs.FS implementations for remote filesystems.
package remotefs

import "github.com/k0sproject/rig/exec"

// NewFS returns a fs.FS compatible implementation for access to remote filesystems.
func NewFS(c exec.Runner) FS {
	if c.IsWindows() {
		return NewWindowsFS(c)
	}
	return NewPosixFS(c)
}
