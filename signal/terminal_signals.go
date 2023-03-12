//go:build !windows
// +build !windows

package signal

import (
	"os"
	"syscall"
)

// TerminalSignals is a list of signals that should be forwarded from local to remote terminals
var TerminalSignals = []os.Signal{
	os.Interrupt,
	syscall.SIGTSTP,
	syscall.SIGWINCH,
}
