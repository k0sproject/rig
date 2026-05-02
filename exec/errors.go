package exec

import "errors"

var (
	// ErrRemote is returned when an action fails on remote host
	ErrRemote = errors.New("remote exec error")
	// ErrSudo is returned when wrapping a command with sudo fails
	ErrSudo = errors.New("sudo error")
)
