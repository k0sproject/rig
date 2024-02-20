package exec

import (
	"github.com/k0sproject/rig/shellescape"
)

// Quote returns a shell escaped string.
// This is a wrapper around shellescape.Quote and
// it is here to avoid importing shellescape separately.
//
// Example:
//
//	c.Exec(fmt.Sprintf("echo %s", exec.Quote("hello world"))
func Quote(s string) string {
	return shellescape.Quote(s)
}

// Command returns a shell escaped command string.
//
// Example:
//
//	c.Exec(exec.Command("echo", "hello world"))
//	// resulting command: echo 'hello world'
func Command(cmd string, args ...string) string {
	if len(args) == 0 {
		return shellescape.Quote(cmd)
	}
	parts := make([]string, len(args)+1)
	parts[0] = cmd
	copy(parts[1:], args)
	return shellescape.Join(parts...)
}
