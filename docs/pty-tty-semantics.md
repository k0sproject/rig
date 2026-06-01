# PTY / TTY Session Semantics

This document specifies what rig's `ExecInteractive` can support across each protocol
implementation. It is the design reference for future work such as expect-style
interaction, explicit PTY control, and terminal-resize support.

The **consistent guaranteed contract** (intersection of all protocols) is narrow:

- stdin, stdout, stderr are forwarded to the remote process
- context cancellation terminates the session
- empty `cmd` falls back to a protocol-appropriate default shell

Everything beyond that is protocol-specific. The rest of this document audits the
six dimensions of interactive-session behaviour for each protocol.

---

## Protocol matrix

### PTY allocation

| Protocol   | Behaviour |
|------------|-----------|
| Native SSH | Automatic when stdin is a `*os.File` and `term.IsTerminal(fd)` is true. PTY type is `xterm` (hardcoded). Terminal modes: `{ECHO: 1}` only. Not requested for non-terminal stdin (pipes, buffers). |
| OpenSSH    | Not automatic. `ExecInteractive` delegates to `StartProcess` which passes streams directly to `exec.Cmd` (the `ssh` binary) with no `-t` / `-tt` flags. Callers can opt in by setting `Options{"RequestTTY": "force"}`, which becomes `-o RequestTTY=force`. The `ssh` binary's own `RequestTTY auto` default applies otherwise. |
| WinRM      | None. WinRM is a text-based RPC protocol; no PTY / ConPTY concept. |
| Localhost  | None. `os.StartProcess` inherits the provided file descriptors. If a real terminal FD is passed, the child can access it as a character device, but no PTY is allocated by rig. |

### Terminal sizing

| Protocol   | Behaviour |
|------------|-----------|
| Native SSH | Initial size read from stdin fd at PTY-setup time via `term.GetSize`. Subsequent resizes forwarded as SSH `window-change` requests on SIGWINCH (POSIX only). Note: `termSizeWNCH()` reads `os.Stdin.Fd()`, not the passed-in stdin fd (see Gaps). |
| OpenSSH    | Delegated entirely to the `ssh` binary. When stdin is a real terminal the binary handles SIGWINCH itself via its own raw-mode loop. |
| WinRM      | None. |
| Localhost  | None managed by rig. Child inherits terminal dimensions if the passed FDs happen to be a terminal. |

### Signal forwarding

| Protocol         | Behaviour |
|------------------|-----------|
| Native SSH (POSIX) | SIGINT → `\x03` written to stdin pipe; SIGTSTP → `\x1a` written to stdin pipe; SIGWINCH → SSH `window-change` request. (TODO in source: use `session.Signal()` and `session.WindowChange()` instead of raw bytes/manual request.) |
| Native SSH (Windows) | SIGINT → `\x03` written to stdin pipe only. SIGTSTP and SIGWINCH are not handled. |
| OpenSSH          | Delegated to `ssh` binary. When stdin is a terminal the binary installs its own signal handlers. |
| WinRM            | None. WinRM has no signal channel. |
| Localhost        | None forwarded by rig. Child process may receive OS signals directly if in the same process group. |

### Raw mode

| Protocol   | Behaviour |
|------------|-----------|
| Native SSH | Local terminal put into raw mode via `term.MakeRaw` when PTY is requested. Restored (deferred) when session ends. |
| OpenSSH    | Managed by the `ssh` binary when it determines a PTY is needed. |
| WinRM      | N/A. |
| Localhost  | None. The calling process's terminal is not modified. |

### stdin / stdout / stderr

| Protocol   | Behaviour |
|------------|-----------|
| Native SSH | `nil` → `os.Stdin` / `os.Stdout` / `os.Stderr`. stdin forwarded through `session.StdinPipe()` via goroutine copy; stdout/stderr attached directly to session. |
| OpenSSH    | Passed verbatim to `exec.Cmd.Stdin/Stdout/Stderr`. `nil` follows `exec.Cmd` semantics: nil Stdin = `/dev/null`, nil Stdout/Stderr = output discarded. This differs from native SSH. |
| WinRM      | Passed verbatim to `RunWithContextWithInput`. Behaviour for `nil` stdin is undefined by rig and depends on the underlying WinRM client library. |
| Localhost  | `nil` → `os.Stdin` / `os.Stdout` / `os.Stderr`. Non-`*os.File` streams bridged through `os.Pipe` goroutines; `*os.File` streams attached directly as process file descriptors. |

### Cancellation

| Protocol   | Behaviour |
|------------|-----------|
| Native SSH | Context cancellation → `session.Close()` via goroutine watching `ctx.Done()`. Goroutine is cleaned up with a `watchDone` channel when the function returns normally. |
| OpenSSH    | `exec.CommandContext` kills the `ssh` subprocess on cancellation. Returns `ctx.Err()`. |
| WinRM      | `RunWithContextWithInput` accepts ctx; returns `ctx.Err()` on cancellation. |
| Localhost  | `proc.Kill()` (SIGKILL) on ctx cancellation. Goroutine is cleaned up with a `watchDone` channel. |

---

## Gaps and follow-up work

The following issues were identified during this audit. They are listed here as
candidates for future implementation rather than fixed in this card.

1. **nil-stream defaulting inconsistency** — Native SSH and localhost default `nil`
   streams to the process's stdio; OpenSSH does not (follows `exec.Cmd` semantics);
   WinRM behaviour is undefined. All four should agree.

2. **PTY only in native SSH** — No automatic PTY allocation in OpenSSH, localhost,
   or WinRM. OpenSSH at least supports opt-in via `Options{"RequestTTY": "force"}`;
   localhost and WinRM have no equivalent.

3. **No cross-protocol API to request PTY** — There is no way for a caller to
   express "allocate a PTY if the protocol supports it" without writing
   protocol-specific code. A `PTYExecer` interface or `SessionOptions` struct
   would give callers a single knob.

4. **`xterm` terminal type hardcoded** — Should default to `$TERM` or be
   configurable via `SessionOptions`.

5. **Minimal `TerminalModes`** — Only `ECHO: 1` is set. Common modes such as
   `ICRNL`, `OPOST`, and `ISIG` are not configured, which may cause surprising
   behaviour (e.g. no newline translation).

6. **`SIGWINCH` reads `os.Stdin`, not the passed-in stdin** — `termSizeWNCH()`
   hardcodes `os.Stdin.Fd()` for the size query. If the caller passes a different
   stdin file the reported window size will be wrong.

7. **Raw control bytes instead of SSH signal channel** — `captureSignals` writes
   `\x03` / `\x1a` directly to the stdin pipe instead of using `session.Signal()`,
   and sends a raw `window-change` request instead of `session.WindowChange()`.
   Both TODO comments exist in the source. Using the SSH signal channel is more
   correct and avoids conflating control bytes with data.

8. **Localhost cancellation uses SIGKILL** — `proc.Kill()` gives the child no
   opportunity to clean up. Interactive shells and programs that catch SIGINT
   or SIGTERM would benefit from a graceful-then-forceful shutdown sequence.

9. **OpenSSH `ExecInteractive` nil-stream safety** — nil stdin/stdout/stderr are
   not defaulted before being handed to `exec.Cmd`, unlike native SSH. This can
   silently swallow output or fail if the library assumes non-nil.

10. **WinRM nil stdin** — `RunWithContextWithInput` receives nil stdin directly;
    behaviour depends on the masterzen/winrm library internals and is not specified.

11. **Signal forwarding absent on Windows native SSH** — SIGTSTP and SIGWINCH
    are not forwarded on Windows even when PTY is supported.

---

## What expect-style interaction can rely on today

An expect implementation built on `ExecInteractive` can only portably assume:

- stdout and stderr arrive as `io.Writer` streams
- stdin can be written to as an `io.Reader`/`io.Writer` (after plumbing)
- context cancellation will terminate the remote process

PTY-specific features (line-discipline control, terminal sizing, signal delivery)
are only available on native SSH over POSIX. Any expect harness that needs a PTY
must gate on the protocol and OS, or require callers to provide a `PTYExecer`
implementation explicitly.
