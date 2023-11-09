// Package initsystem provides an abstraction over several supported init systems.
package initsystem

// Host interface for init system
type Host interface {
	Execf(cmd string, args ...any) error
	ExecOutputf(cmd string, args ...any) (string, error)
	Sudo(cmd string) (string, error)
}
