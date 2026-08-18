// Package sh provides tools to build and manipulate shell commands.
package sh

import (
	"github.com/k0sproject/rig/v2/sh/shellescape"
)

// DefaultShell is the shell rig uses to run commands on non-Windows hosts.
// An absolute path is used on purpose: the whole point of imposing a shell is
// to not depend on the remote environment, and that includes PATH.
const DefaultShell = "/bin/sh"

// CommandBuilder is a builder for shell commands. It is based on string and can be
// converted to one using string(CommandBuilder("foo")) or calling the String method.
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

// Raw adds a raw string to the command without shell escaping. This
// is needed when you want to use shell operators, globbing or variables.
func (c CommandBuilder) Raw(arg string) CommandBuilder {
	return CommandBuilder(c.String() + " " + arg)
}

// Quote returns a shell escaped string.
// This is a wrapper around shellescape.Quote and
// it is here to avoid importing shellescape separately.
func Quote(s string) string {
	return shellescape.Quote(s)
}

// Shell returns the command wrapped in an explicit invocation of [DefaultShell]:
//
//	sh.Shell("echo foo | tee bar")
//	// resulting command: /bin/sh -c -- 'echo foo | tee bar'
//
// Use this when a command must be interpreted by a POSIX shell, either because
// it is a compound expression handed to something that execs directly (like
// sudo) or because whatever runs it may not be a POSIX shell, such as a remote
// user's fish login shell.
//
// This is the one place a rig command is quoted for a shell nobody chose, so the
// quoting comes from [shellescape.QuoteForLoginShell], which covers POSIX shells
// and fish. A csh login shell can still reject a command containing ! or an
// embedded newline.
func Shell(command string) string {
	return ShellWith(DefaultShell, command)
}

// ShellWith is [Shell] with an explicit shell. An empty shell means [DefaultShell].
func ShellWith(shell, command string) string {
	if shell == "" {
		shell = DefaultShell
	}
	// The option terminator keeps a command that starts with a dash from being
	// read as shell options: `sh -c -x` reports an invalid option instead of
	// running anything.
	return shellescape.QuoteForLoginShell(shell) + " -c -- " + shellescape.QuoteForLoginShell(command)
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
