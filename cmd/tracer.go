package cmd

import "time"

// Tracer observes command lifecycle events on an [Executor].
// Register a Tracer via [Trace] (per-call) or [Executor.SetTracer] (runner-wide).
type Tracer interface {
	// CommandFormatted fires just before a process is started, with the final
	// command string after all decorators and OS-specific wrapping.
	CommandFormatted(host, formatted string)
	// ProcessStarted fires after the underlying process starts successfully.
	ProcessStarted(host, formatted string)
	// ProcessFinished fires after the process exits.
	// duration is the wall-clock time since ProcessStarted. err is nil on success.
	ProcessFinished(host, formatted string, duration time.Duration, err error)
}

// OutputTracer extends [Tracer] with per-line stdout and stderr hooks.
// Implement this alongside Tracer to receive output as individual lines.
// Each line is delivered without the trailing newline. A partial final line
// (not terminated by a newline) is flushed when the process exits.
//
// Concurrency: StdoutLine and StderrLine may be called concurrently with each
// other and with [Tracer.ProcessFinished]. Implementations must be safe for
// concurrent use.
type OutputTracer interface {
	Tracer
	// StdoutLine fires for each line written to stdout. The line does not
	// include a trailing newline character.
	StdoutLine(host, line string)
	// StderrLine fires for each line written to stderr. The line does not
	// include a trailing newline character.
	StderrLine(host, line string)
}

// Explanation holds the formatted representations of a command as it would
// appear at each stage of the execution pipeline.
type Explanation struct {
	// Formatted is the command string after all global and per-call
	// decorators, sudo wrapping, and any OS-specific prefixes that can be
	// determined without probing the connection (e.g. "cmd.exe /C"). When
	// OSWrappingKnown is false, OS-specific prefixes are omitted.
	Formatted string
	// Decoded is the human-readable form of Formatted. PowerShell
	// EncodedCommand payloads are decoded so you can read the actual script.
	Decoded string
	// Logged is the command as it appears in debug log lines: Decoded with
	// any strings registered via [Redact] replaced by the redact mask. It is
	// empty when command logging is disabled.
	Logged string
	// CommandLogged reports whether Logged would be emitted by command logging.
	CommandLogged bool
	// OSWrappingKnown reports whether Formatted could include OS-specific
	// command wrapping without probing the connection.
	OSWrappingKnown bool
}
