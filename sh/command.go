// Package sh provides tools to build shell commands.
package sh

import (
	"github.com/k0sproject/rig/v2/sh/shellescape"
)

// CommandBuilder is a builder for shell commands.
type CommandBuilder string

// String returns the command as a string.
func (c CommandBuilder) String() string {
	return string(c)
}

// Pipe the command to another command. The target command is shell escaped.
func (c CommandBuilder) Pipe(cmd string, args ...string) CommandBuilder {
	return CommandBuilder(c.String() + " | " + Command(cmd, args...))
}

// Arg adds an argument to the command. The argument is shell escaped.
func (c CommandBuilder) Arg(arg string) CommandBuilder {
	return CommandBuilder(c.String() + " " + shellescape.Quote(arg))
}

// Args adds multiple arguments to the command. The arguments are shell escaped.
func (c CommandBuilder) Args(args ...string) CommandBuilder {
	for _, arg := range args {
		c = c.Arg(arg)
	}
	return c
}

// ErrToNull redirects the command's stderr to /dev/null.
func (c CommandBuilder) ErrToNull() CommandBuilder {
	return CommandBuilder(c.String() + " 2>/dev/null")
}

// OutToNull redirects the command's stdout to /dev/null.
func (c CommandBuilder) OutToNull() CommandBuilder {
	return CommandBuilder(c.String() + " >/dev/null")
}

// ErrToOut redirects the command's stderr to stdout.
func (c CommandBuilder) ErrToOut() CommandBuilder {
	return CommandBuilder(c.String() + " 2>&1")
}

// OutToFile redirects the command's stdout to a file.
func (c CommandBuilder) OutToFile(file string) CommandBuilder {
	return CommandBuilder(c.String() + " >" + shellescape.Quote(file))
}

// ErrToFile redirects the command's stderr to a file.
func (c CommandBuilder) ErrToFile(file string) CommandBuilder {
	return CommandBuilder(c.String() + " 2>" + shellescape.Quote(file))
}

// AppendOutToFile appends the command's stdout to a file.
func (c CommandBuilder) AppendOutToFile(file string) CommandBuilder {
	return CommandBuilder(c.String() + " >>" + shellescape.Quote(file))
}

// AppendErrToFile appends the command's stderr to a file.
func (c CommandBuilder) AppendErrToFile(file string) CommandBuilder {
	return CommandBuilder(c.String() + " 2>>" + shellescape.Quote(file))
}

// Raw adds a raw string to the command without shell escaping.
func (c CommandBuilder) Raw(arg string) CommandBuilder {
	return CommandBuilder(c.String() + " " + arg)
}

// Quote returns a shell escaped string.
// This is a wrapper around shellescape.Quote and
// it is here to avoid importing shellescape separately.
//
// Example:
//
//	c.Exec(fmt.Sprintf("echo %s", sh.Quote("hello world"))
func Quote(s string) string {
	return shellescape.Quote(s)
}

// Command returns a shell escaped command string.
//
// Example:
//
//	c.Exec(sh.Command("echo", "hello world"))
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
