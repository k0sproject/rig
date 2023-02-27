package exec

import "errors"

var (
	ErrRemote = errors.New("remote exec error") // ErrRemote is returned when an action fails on remote host
	ErrSudo   = errors.New("sudo error")        // ErrSudo is returned when wrapping a command with sudo fails
)
