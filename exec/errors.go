package exec

import "github.com/k0sproject/rig/errstring"

var (
	ErrRemote = errstring.New("remote exec error") // ErrRemote is returned when an action fails on remote host
	ErrSudo   = errstring.New("sudo error")        // ErrSudo is returned when wrapping a command with sudo fails
)
