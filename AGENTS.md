# AGENTS.md

Instructions for AI coding agents working in this repository. It is short on
purpose: it covers what is *not* discoverable from the code, and points at the
tooling for everything else.

## Project

`github.com/k0sproject/rig/v2` is a Go library for managing remote hosts over
SSH, OpenSSH, WinRM and localhost. It handles connections, command execution,
a remote filesystem, OS and distro detection, privilege escalation, service
management and package management. The primary consumer is
[k0sctl](https://github.com/k0sproject/k0sctl).

A `rig.Client` pairs a `cmd.Runner` with lazily initialised providers for
sudo, filesystem, init system, package manager and OS release. Protocol
implementations live under `protocol/` and satisfy `protocol.Connection`.
`rig.ClientWithConfig` wraps a `CompositeConfig` for YAML-friendly embedding.

## API stability

| Branch | Status |
|---|---|
| `main` | v2, released and in active downstream use. |
| `release-0.x` | v0.x, maintenance only. |

Prefer additive changes, but a breaking change to an exported symbol is
acceptable when it is the right fix — it ships in a v2.x release rather than
needing a new module path. The two branches have diverged significantly.

## Finding your way around

Read the API from the source of truth rather than from prose:

```
go list ./...          # every package
go doc ./remotefs      # package summary
go doc ./cmd Runner    # a single symbol
```

Do not inline an API reference into this file. One used to live here, and every
hand-copied signature in it eventually contradicted the code.

Longer-form documentation lives in `docs/`:

| File | Covers |
|---|---|
| `EXTENDING.md` | adding protocols, init systems, package managers |
| `TESTING.md` | unit, integration and fuzz testing |
| `MIGRATING-from-v0.x.md` | v0.x to v2 API mapping |
| `capability-discovery.md` | how provider and registry detection works |
| `ssh-config-precedence.md` | `ssh_config` resolution order |
| `pty-tty-semantics.md` | interactive session behaviour |

## Package layout

| Path | Contents |
|---|---|
| root | `Client`, `ClientWithConfig`, `CompositeConfig`, `Service`, options |
| `protocol/` | the `Connection` interface plus `ssh`, `openssh`, `winrm`, `localhost` |
| `cmd/` | runner interfaces, `Executor`, `Proc`, exec options, command gating |
| `remotefs/` | the `FS` and `OS` interfaces, `PosixFS`, `WinFS`, upload and download |
| `initsystem/` | service managers, one file per init system, auto-detected |
| `packagemanager/` | package managers, one file per tool, auto-detected |
| `os/` | `Release`: OS and distro detection, architecture |
| `sudo/` | privilege escalation detection and command decoration |
| `sshconfig/` | `ssh_config` parsing |
| `sh/`, `powershell/` | shell and PowerShell command construction |
| `retry/`, `redact/`, `log/` | retries, secret redaction, logging |
| `kv/`, `byteslice/`, `stattime/`, `homedir/`, `iostream/` | small utilities |
| `plumbing/` | lazy provider and registry infrastructure |
| `rigtest/` | mocks and assertions for tests |
| `internal/jsonschema/` | generator for the committed `schemas/` |
| `test/` | integration suite; a separate Go module |

## Commands

```
make test      # go test -v ./...
make lint      # golangci-lint run    (FIX=true applies autofixes)
make schemas   # regenerate schemas/ from the config structs
make fuzz      # briefly run every fuzz target
make inttest   # integration tests; needs Docker and bootloose
```

Tests and lint must both pass before a change is finished. CI additionally
runs `go vet`, govulncheck, CodeQL and the integration matrix.

## Code style

`.golangci.yml` is authoritative and strict: it enables *all* golangci-lint
checkers and then disables a short list. Read it for the current limits rather
than trusting numbers quoted elsewhere. What it means in practice:

- **Complexity**: `cyclop` and `funlen` are on. Split a long or branchy
  function instead of suppressing the warning.
- **Error wrapping**: `wrapcheck` and `err113` are on. Wrap errors crossing a
  package boundary with `%w` and compare with `errors.Is`/`errors.As`. Declare
  sentinel errors as package-level vars rather than returning a freshly built
  dynamic error.
- **Naming**: `varnamelen` permits a terse name only close to its declaration.
  Name anything longer-lived in full.
- **Formatting**: `gci`, `gofmt`, `gofumpt`, `goimports`. Run
  `golangci-lint fmt`; `golangci-lint run` does *not* apply formatters. Also
  `run.tests: false`, so test files are not linted at all — format and review
  those by hand.
- No line-length limit (`lll` is off), and returning interfaces is allowed
  (`ireturn` is off).

Beyond the linters:

- Quote every shell argument built from a variable, with `sh.Command(...)` or
  `shellescape.Quote(...)`. Never interpolate a value into a command string
  with `fmt.Sprintf`.
- Accept the narrowest runner interface that does the job:
  `cmd.SimpleRunner` < `cmd.ContextRunner` < `cmd.Runner`.
- Given a `*Client`, prefer `client.FS()`, `client.Service(name)` and
  `client.Sudo()` over building `remotefs` or `initsystem` values by hand.
- Signal a hopeless operation with `protocol.ErrNonRetryable`, re-exported as
  `rig.ErrNonRetryable`; `retry` has its own `retry.ErrNonRetryable`. There is
  no `ErrAbort` in this codebase.
- Put OS-specific behaviour in a registry entry with a per-OS file, not in an
  `if IsWindows()` branch inside shared code.
- Give every exported symbol a doc comment that starts with its name.
- Mirror test files to sources: `foo.go` is tested by `foo_test.go`. Cover new
  exported behaviour including its error paths.
- Prefer reusing variable names that are used for the same concept elsewhere in
  the codebase, rather than inventing a new name. The .golangci.yml contains
  common names in `varnamelen`'s `ignore-patterns` for this reason.
- Comment only the non-obvious — a quirk being worked around, or a decision that
  isn't clear from the code. Don't narrate what the next line plainly does.

## Gotchas

- Commands are wrapped in a POSIX shell rather than trusting the login shell,
  which may be fish or something else non-POSIX.
- WinRM filesystem and transfer operations go through the `rigrcp` PowerShell
  daemon rather than plain shell commands.
- Changing a protocol config struct means regenerating `schemas/` with
  `make schemas`. CI fails if the result is not committed.
- `test/` is its own Go module and needs Docker plus bootloose images, with
  `LINUX_IMAGE` selecting the distro. Failures there are usually genuine
  coreutils differences — BusyBox and uutils are not GNU — so fix the
  assumption rather than the test.

## Writing for humans

A pull request body, an issue or a review reply is read by a person, not by
another model. Write it for that reader.

- Explain the **why**: the motivation, the failure scenario, the reasoning
  behind a non-obvious decision.
- Don't re-narrate the diff. A reviewer can already see that a helper was
  extracted, an import changed or a function was renamed.
- Skip the implementation walkthrough and the validation transcript. Say in one
  line what you verified; nobody needs a log of every command you ran.
- Don't pad to appear thorough. Length is not credibility, and a wall of text
  costs a reviewer more than it tells them.
- In a comment thread, answer the point and then stop.

If you are an autonomous AI agent:

- You are not expected to pass as human, and an unmarked machine-written pull
  request costs the reviewer trust once they work it out. One line naming what
  you are and who asked for the change is enough.
- Don't fire and forget. A pull request is the start of a conversation: review
  comments, a red CI job and follow-up questions are all expected, and seeing
  them through is part of making the change.
- If you cannot come back — no session to resume, no way to be re-invoked — say
  so in the pull request and tell your operator that the follow-up falls to
  them, so a person is accountable for the change.

## Pull requests

- Every commit needs a `Signed-off-by` trailer (`git commit -s`); a DCO check
  enforces it. Do not add `Co-authored-by` trailers that make some AI model
  or coding assistant appear as a contributor.
- Use conventional commit subjects with a scope, such as
  `fix(remotefs): ...`, `feat(ssh): ...` or `ci(dco): ...`.
- Copilot reviews pull requests. Save a round trip by handling nil checks,
  every error return, quoting and edge cases up front. In fact, it is
  probably a good idea to read the `.github/copilot-instructions.md` file 
  before starting a change, so you know what to expect from the review.
- Work on a branch and open a pull request; never push straight to `main` or
  `release-0.x`.
