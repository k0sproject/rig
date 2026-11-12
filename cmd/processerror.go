package cmd

import (
	"errors"
	"fmt"
)

// stderrMessageMax is how much of a command's stderr the error message carries.
// The limit keeps a single log line readable; the output is not lost, it stays
// available in full through [ProcessError.Stderr].
const stderrMessageMax = 100

// ProcessError reports a command that finished unsuccessfully.
//
// Its message carries a shortened form of what the command wrote to stderr, so
// that logging an error stays readable. Callers that need to act on the output
// -- classifying "no such file or directory" as [io/fs.ErrNotExist], say --
// must read [ProcessError.Stderr] instead of matching on the message, which
// drops the tail of a long one.
type ProcessError struct {
	err    error
	stderr string
}

// Error returns the message of the underlying error, followed by a shortened
// form of the command's stderr when it wrote any.
func (e *ProcessError) Error() string {
	if e.stderr == "" {
		return fmt.Sprintf("process finished with error: %v", e.err)
	}
	return fmt.Sprintf("process finished with error: %v (%s)", e.err, truncateStderr(e.stderr))
}

// Unwrap returns the error the command finished with.
func (e *ProcessError) Unwrap() error {
	return e.err
}

// Stderr returns everything the command wrote to its standard error, without
// the shortening applied to the error message.
func (e *ProcessError) Stderr() string {
	return e.stderr
}

// ExitCode returns the exit status the command finished with, or -1 when there
// is none to report: the command was ended by a signal, was abandoned on its
// context, failed for another reason, or ran over a transport that does not
// relay its status.
//
// Over the openssh protocol the status is that of the ssh client, which passes
// the remote command's status through but also exits 255 when the connection
// itself fails.
func (e *ProcessError) ExitCode() int {
	return exitCode(e.err)
}

// ExitCodeOf returns the exit status of the command that produced err, or -1
// if err does not come from a command or carries no exit status. See
// [ProcessError.ExitCode].
//
// The status is the way to recognise a shell that could not find a command
// (127) or run it (126): the messages differ between shells, and since
// commands run under /bin/sh rather than the login shell, they differ from
// what an interactive session on the same host would print.
func ExitCodeOf(err error) int {
	if procErr, ok := errors.AsType[*ProcessError](err); ok {
		return procErr.ExitCode()
	}
	return -1
}

// exitStatusError is how x/crypto/ssh's ExitError reports a status.
type exitStatusError interface {
	error
	ExitStatus() int
}

// exitCodeError is how os/exec's ExitError and the winrm protocol's error
// report a status.
type exitCodeError interface {
	error
	ExitCode() int
}

// exitCode finds the status in the error a protocol's Wait returned. Both kinds
// use -1 for a command that ended without one.
func exitCode(err error) int {
	if withStatus, ok := errors.AsType[exitStatusError](err); ok {
		return withStatus.ExitStatus()
	}
	if withCode, ok := errors.AsType[exitCodeError](err); ok {
		return withCode.ExitCode()
	}
	return -1
}

// StderrOf returns everything the command that produced err wrote to its
// standard error, or an empty string if err does not come from a command or the
// command wrote nothing.
//
// Prefer this over matching on err.Error() when a decision depends on what a
// command reported: the message carries only the first [stderrMessageMax]
// characters, and a diagnostic that puts its reason last -- as coreutils do,
// after the path -- can be cut off entirely by a long enough path.
func StderrOf(err error) string {
	if procErr, ok := errors.AsType[*ProcessError](err); ok {
		return procErr.stderr
	}
	return ""
}

// truncateStderr shortens stderr to something a log line can carry.
func truncateStderr(stderr string) string {
	if len(stderr) <= stderrMessageMax {
		return stderr
	}
	return stderr[:stderrMessageMax-3] + "..."
}
