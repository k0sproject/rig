# Copilot instructions

`github.com/k0sproject/rig/v2` is a Go library for managing remote hosts over
SSH, OpenSSH, WinRM and localhost. `AGENTS.md` in the repository root is
authoritative for layout, conventions and code style — follow it rather than
restating it here.

## Context to review with

- The module targets the latest Go version. Language features from recent releases
  are available and correct: ranging over an integer (`for i := range 10`), the
  `min`/`max` builtins, generic type parameters, and `for` loops with
  per-iteration variable scope. Do not report these as compile errors. In fact,
  suggest using them where they make the code clearer.
- **Test files are not linted** (`run.tests: false` in `.golangci.yml`), so
  review of `_test.go` code is genuinely useful here. Real bugs in tests will
  not be caught by anything else.
- `schemas/` is **generated** by `make schemas`. Review the config structs that
  drive it, not the output.
- This library runs commands on hosts it does not control: BusyBox and uutils
  coreutils rather than GNU, BSD and macOS userlands, and Windows with a
  PowerShell 5.1 baseline. Commands are wrapped in a POSIX shell, and the
  operator's login shell may be fish. Findings about missing utilities,
  non-GNU flag behaviour and shell quoting are in scope and welcome.
- `remotefs.PosixFS` and `remotefs.WinFS` mirror each other. A behavioural
  change to one usually needs the same change, or a deliberate difference, in
  the other.
- `rigtest.MockRunner` needs a stub for every command a code path executes. An
  unstubbed command is a common cause of a test that passes for the wrong
  reason.

## Worth flagging

In rough order of how much these matter in this repository:

- **A doc comment or document that contradicts the implementation.** Comments
  are read as specifications: a comment promising "removed only on failure"
  above an unconditional cleanup, a doc claiming an error is "always nil" when
  it is not, or a table in `docs/` that no longer matches the code. This is the
  most valuable review you do here.
- **Format-verb misuse**, especially `%s` applied to an `error` or a `[]string`,
  which renders as `%!s(...)` and makes real failures unreadable.
- **List and multi-file handling that only honours the first element** while the
  documented contract covers all of them.
- **Tests that assert success without asserting the new behaviour** the change
  introduces, and new code paths (a fallback, a retry, an alternate utility)
  left uncovered.
- **Unexported sentinel errors** that callers outside the package cannot match
  with `errors.Is`.
- Aliasing of a caller's slice or map that is retained after construction.
- A shell argument built from a variable without `sh.Command` or
  `shellescape.Quote`.

## Not worth flagging

- Anything `.golangci.yml` already catches or what is disabled intentionally.
- Import order and formatting. `gci`, `gofmt`, `gofumpt` and `goimports` own
  those and will be caught in lint.
- Grammar and wording preferences in comments, documentation or commit
  messages, unless the meaning is actually wrong.

## Comments in the code

Inline comments have to earn their place. Two reasons qualify: a decision
tree whose branching or ordering is genuinely hard to follow, and a
strange-looking line that exists because of an external quirk — BSD `chmod`
wanting `--` before the mode, a uutils flag that behaves unlike GNU's.

Reviewing with that in mind:

- Flag narration a change adds — a comment that restates the line below it or
  the variable being assigned. `// find separator` above the line that finds
  the separator, and `// if there is no separator, the line is invalid` above
  `if idx == -1`, are noise. Whereas `// some versions of ssh output a broken
  line for canonicaldomains that doesn't include a value` is exactly what a
  comment is for.
- Flag a comment that asserts what *another* function does. It rots silently
  when that function changes; the rationale belongs in the callee's doc comment.
- Do not ask for a comment on code that reads fine on its own. Absent narration
  is not a finding.
- A comment that contradicts the code is a different matter — that is a bug,
  and reporting it is the most useful thing you do here.

Doc comments on exported symbols are separate, and are required.

## Review comments

Keep each comment to one finding, and name the concrete failure — the input,
state or platform where the code does the wrong thing. A reviewer acts on "with
`UserKnownHostsFile none` this falls through to the read-only path and accepts
an unknown key"; there is nothing to act on in "consider reviewing the error
handling here". Skip praise and skip the summary wall-of-text.
