package cmd

import "context"

// CommandGate is consulted before a command runs, on the [Executor] execution
// path that every Exec/Start call (and thus every remotefs and service call)
// funnels through. Returning a non-nil error aborts the command before it
// starts; the error is propagated to the caller wrapped in [ErrCommandRejected].
//
// A context cancellation or deadline error is the one exception: it is
// propagated without [ErrCommandRejected], so errors.Is reports the context
// error but not a rejection. Callers that treat rejections specially can thus
// distinguish an explicit refusal from a cancelled/expired context.
//
// A gate fires once per command at the outermost runner, so a command that is
// wrapped by sudo (or any other decorator chain) is presented in its final,
// fully wrapped form exactly once. On that path the command argument is the
// human-readable form — decorated, sudo-wrapped, PowerShell-decoded, and with
// any registered secrets redacted — suitable for a confirmation prompt or
// audit log.
//
// The following do not go through this path and are therefore not gated:
//   - [Executor.StartProcess], the low-level primitive used for runner chaining.
//   - The built-in Windows-detection probe (ver.exe), which runs via
//     StartProcess at runner construction to decide command formatting.
//   - Privilege-escalation probes (sudo/doas availability, UID-0 and
//     Windows-admin checks), which run [Ungated] so a rejecting gate surfaces
//     an error rather than silently disabling sudo.
//
// [github.com/k0sproject/rig/v2.Client.ExecInteractive] consults the gate
// separately, with the raw (undecorated, unredacted) command.
//
// AllowCommand may block (for example to prompt on stdin); it should honor the
// supplied context and return its error when the caller cancels.
type CommandGate interface {
	// AllowCommand reports whether the command may run on the named host.
	// A nil return allows the command; a non-nil error aborts it.
	AllowCommand(ctx context.Context, host, command string) error
}

// CommandGateFunc adapts an ordinary function to the [CommandGate] interface.
type CommandGateFunc func(ctx context.Context, host, command string) error

// AllowCommand implements [CommandGate].
func (f CommandGateFunc) AllowCommand(ctx context.Context, host, command string) error {
	return f(ctx, host, command)
}

// CommandGateSetter is implemented by runners that support installing a
// [CommandGate]. [Executor] implements it, allowing a gate configured on a
// [github.com/k0sproject/rig/v2.Client] to be propagated to every runner the
// client derives (sudo, filesystem, services).
type CommandGateSetter interface {
	SetCommandGate(gate CommandGate)
}
