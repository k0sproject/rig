# Migrating from rig v0.x to v2

This guide covers the breaking changes between rig v0.x and v2, with before/after
examples for each change. It is written with k0sctl's migration in mind but applies
to any consumer.

---

## Design shifts

Three architectural shifts drive most of the API changes:

**1. Composable `Client` replaces monolithic `Connection`**

v0.x had a single `rig.Connection` struct that held everything. v2 has a `rig.Client`
composed of pluggable lazy-init service providers (filesystem, init system, package
manager, OS detection). Each provider is initialized on first use and cached. There is
no longer a single "connect and you get everything" struct — instead, you get a client
and ask it for the services you need.

**2. Sudo is a client clone, not an exec option**

v0.x threaded `exec.Sudo(h)` as a variadic option through every privileged call site.
v2 collapses this: `h.Sudo()` returns a memoized clone of the client whose runner
wraps every command with the detected privilege escalation mechanism. Pass the sudo
client around instead of passing an option.

**3. `remotefs.OS` replaces the per-OS Configurer interface**

v0.x routed filesystem and OS operations through a `Configurer` interface implemented
separately per Linux distro, Windows, Darwin, etc. v2 provides `remotefs.OS` — a
single interface backed by OS-aware implementations that select the right
implementation automatically. Operations like `FileExist`, `WriteFile`, `Hostname`,
`LookPath`, and `TempDir` are now direct method calls on the FS handle.

---

## Module import path

```
github.com/k0sproject/rig        →  github.com/k0sproject/rig/v2
github.com/k0sproject/rig/exec   →  github.com/k0sproject/rig/v2/cmd
```

Update your `go.mod` and all import statements.

---

## Host embedding: `rig.Connection` → `rig.ClientWithConfig`

v0.x exposed a single `rig.Connection` struct that was embedded inline into host types.
v2 replaces it with `rig.ClientWithConfig`, which works the same way from a YAML
perspective but wires up the richer `Client` underneath.

**v0.x**
```go
import "github.com/k0sproject/rig"

type Host struct {
    rig.Connection `yaml:",inline"`
    Role           string `yaml:"role"`
}
```

**v2**
```go
import rig "github.com/k0sproject/rig/v2"

type Host struct {
    rig.ClientWithConfig `yaml:",inline"`
    Role                 string `yaml:"role"`
}
```

The inline YAML field layout is unchanged — `ssh`, `winRM`, `openSSH`, and `localhost`
surface at the same level in the YAML document.

### Programmatic construction

`ClientWithConfig` is designed for YAML-driven workflows. For purely programmatic
use — e.g. constructing a connection from CLI flags — prefer `rig.NewClient` with
a concrete connection object from the protocol sub-package:

**v0.x**
```go
import "github.com/k0sproject/rig"

host := &Host{
    Connection: rig.Connection{
        SSH: &rig.SSH{Address: addr, Port: port},
    },
}
```

**v2 — direct client (no YAML needed)**
```go
import (
    rig "github.com/k0sproject/rig/v2"
    "github.com/k0sproject/rig/v2/protocol/ssh"
)

client, err := rig.NewClient(rig.WithConnection(&ssh.Connection{
    Config: ssh.Config{Address: addr, Port: port},
}))
```

If you are still embedding `ClientWithConfig` in a Host struct and need to populate
it programmatically (e.g. in tests or from parsed CLI args), assign the config fields
and call `Connect`:

```go
host.ConnectionConfig.SSH = &ssh.Config{Address: addr, Port: port}
if err := host.Connect(ctx); err != nil { ... }
```

### Config type renames

| v0.x | v2 |
|---|---|
| `rig.SSH` | `ssh.Config` (`protocol/ssh`) |
| `rig.OpenSSH` | `openssh.Config` (`protocol/openssh`) |
| `rig.WinRM` | `winrm.Config` (`protocol/winrm`) |
| `rig.Localhost` (struct) | `rig.LocalhostConfig` (bool, same YAML key) |

---

## Context support: opt-in `*Context` method variants

The core exec methods are **unchanged** — `Exec`, `ExecOutput`, `ExecReader`, and
`StartBackground` keep their v0.x signatures (no `context.Context`). This means the
bulk of a migration's exec call sites need no edits at all. k0sctl's migration kept
all ~50 of its `Exec`/`ExecOutput` calls exactly as they were.

**v0.x and v2 — identical**
```go
out, err := h.ExecOutput("echo hello")
err = h.Exec("systemctl restart k0s")
```

What v2 *adds* is a context-aware variant of each method, named with a `Context`
suffix, for when you need cancellation or a deadline:

```go
out, err := h.ExecOutputContext(ctx, "echo hello")
err = h.ExecContext(ctx, "systemctl restart k0s")
waiter, err := h.Start(ctx, "long-running-thing")   // streaming, context-aware
```

The two families come from two interfaces in `cmd`: `SimpleRunner`
(`Exec`/`ExecOutput`/`ExecReader`/`StartBackground`) and `ContextRunner`
(`ExecContext`/`ExecOutputContext`/`ExecReaderContext`/`Start`). `Runner` embeds
both, so a `Client` exposes all of them. Reach for the `*Context` form only where you
actually thread a context; otherwise the plain form is the simplest drop-in.

---

## Sudo: `exec.Sudo(h)` → `h.Sudo()`

The `exec.Sudo(h)` option pattern is replaced by `h.Sudo()`, which returns a memoized
clone of the client whose runner wraps every command with the detected privilege
escalation mechanism. The clone is cached — calling `h.Sudo()` multiple times returns
the same instance.

**v0.x** (~100 call sites, `exec.Sudo(h)` passed as a variadic option)
```go
import "github.com/k0sproject/rig/exec"

out, err := h.ExecOutput(h.Configurer.K0sCmdf("token create %s", flags), exec.Sudo(h))
err = h.Exec("systemctl start k0s", exec.Sudo(h))
```

**v2** (clone the client once per logical sudo scope, then use it)
```go
sudo := h.Sudo()
out, err := sudo.ExecOutput(h.Configurer.K0sCmdf("token create %s", flags))
err = sudo.Exec("systemctl start k0s")
```

For one-off sudo calls:
```go
err = h.Sudo().Exec("systemctl start k0s")
```

### Verifying privilege escalation works

v0.x's `Configurer.CheckPrivilege` is replaced by `Client.CheckSudo(ctx)`, which
eagerly confirms that the detected escalation mechanism actually grants root. Use it
in a validation phase instead of probing manually with `Sudo().Exec("true")`:

```go
if err := h.CheckSudo(ctx); err != nil {
    return fmt.Errorf("%s: sudo not available: %w", h, err)
}
```

### Exec options: package renamed, `Sudo` removed

The `exec` sub-package is renamed to `cmd`. Most exec option functions still exist
under their new import path — update the import and drop the `exec.Sudo(h)` option:

```go
// v0.x
import "github.com/k0sproject/rig/exec"
h.ExecOutput(cmd, exec.Sudo(h), exec.Redact(secret))

// v2
import "github.com/k0sproject/rig/v2/cmd"
h.Sudo().ExecOutput(cmdStr, cmd.Redact(secret))
```

Available options include: `cmd.Redact`, `cmd.HideOutput`, `cmd.HideCommand`,
`cmd.Stdin`, `cmd.Stdout`, `cmd.Stderr`, `cmd.LogError`, `cmd.Sensitive`,
`cmd.StreamOutput`, `cmd.Trace`, and others. `exec.Sudo` has no equivalent option —
use `h.Sudo()` instead.

---

## Upload: new signature

**v0.x**
```go
err = h.Upload(localSrc, remoteDst, 0o600, exec.Sudo(h))
```

**v2**
```go
import "github.com/k0sproject/rig/v2/remotefs"

err = remotefs.Upload(h.Sudo().FS(), localSrc, remoteDst, remotefs.WithPermissions(0o600))
```

Without privilege escalation:
```go
err = remotefs.Upload(h.FS(), localSrc, remoteDst, remotefs.WithPermissions(0o600))
```

`remotefs.Upload` writes to a temporary file and renames atomically, and verifies
the checksum after transfer. It does not take a `context.Context` — the underlying
FS operations are not context-sensitive. To cancel a transfer, cancel the connection
itself via the client's context.

There is **no** sudo option on `Upload` — privilege is determined entirely by *which
FS you hand it*. Pass `h.Sudo().FS()` to write as root, or `h.FS()` to write as the
login user. Both are normal in one codebase: k0sctl uploads most files via
`h.Sudo().FS()` but deliberately restores user-owned files via plain `h.FS()`.

---

## Filesystem: `SudoFsys()` → `h.Sudo().FS()`

`h.SudoFsys()` is replaced by `h.Sudo().FS()`. The returned `remotefs.FS` implements
`fs.FS` and the richer `remotefs.OS` interface.

**v0.x**
```go
h.SudoFsys().MkDirAll(dir, 0o755)
f, err := h.SudoFsys().OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
fi, err := h.SudoFsys().Stat(path)
fs.WalkDir(h.SudoFsys(), pattern, fn)
```

**v2**
```go
fsys := h.Sudo().FS()
err = fsys.MkdirAll(dir, 0o755)         // note: MkdirAll (stdlib casing)
f, err := fsys.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
fi, err := fsys.Stat(path)
fs.WalkDir(fsys, pattern, fn)
```

`h.FS()` (without sudo) is the unprivileged equivalent of the former `h.Fsys()`.

### OS-level operations via `remotefs.OS`

`remotefs.FS` embeds `remotefs.OS`, which provides all the OS-level operations that
were previously scattered across `Configurer` implementations. You can call these
directly without going through a configurer:

| v0.x (via Configurer) | v2 (via `h.FS()` or `h.Sudo().FS()`) |
|---|---|
| `c.FileExist(h, path)` | `fsys.FileExist(path)` |
| `c.WriteFile(h, path, data, perm)` | `fsys.WriteFile(path, data, perm)` |
| `c.MkDir(h, path, ...)` | `fsys.MkdirAll(path, perm)` |
| `c.DeleteFile(h, path)` | `fsys.Remove(path)` |
| `c.MoveFile(h, src, dst)` | `fsys.Rename(src, dst)` |
| `c.Chmod(h, path, mode, ...)` | `fsys.Chmod(path, mode)` |
| `c.LookPath(h, cmd)` | `fsys.LookPath(cmd)` |
| `c.Hostname(h)` | `fsys.Hostname()` |
| `c.TempDir(h)` | `fsys.TempDir()` |
| `c.CommandExist(h, cmd)` | `fsys.CommandExist(cmd)` |
| `c.HostPath(h, path)` (Windows path conversion) | `fsys.NativePath(path)` |
| `c.Quote(str)` (OS-aware shell quoting) | `fsys.ShellQuote(str)` |
| `c.DownloadURL(h, url, dst)` | `fsys.DownloadURL(url, dst)` |

This is where the bulk of the migration savings come from: k0sctl's `Configurer`
interface shrank from over 50 methods to 16, because most of them were thin wrappers
that `remotefs.OS` now provides directly. No new methods were added to the interface —
it is a strict subset of the old one.

### Atomic operations with stdlib `os.O_*` flags

`fsys.OpenFile` honours the standard `os.O_*` flag semantics over the wire, so atomic
create-or-fail patterns work remotely. For example, a lock file that must fail if it
already exists:

```go
f, err := h.Sudo().FS().OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
if err != nil {
    return fmt.Errorf("lock already held: %w", err)   // O_EXCL → fails if present
}
```

This replaces v0.x's `Configurer.UpsertFile`. Pair it with `fsys.Remove(lockPath)` on
every error and exit path to release the lock.

### Silent traps the compiler will not catch

A few translations compile cleanly but are wrong at runtime — they will pass `go build`
and `go vet` and only surface against a real host. Watch for these specifically:

- **Dropping sudo on a *read*.** In v0.x a privileged read was `c.Stat(h, path)` where the
  `h` carried `exec.Sudo`. The mechanical v2 translation is `h.Sudo().FS().Stat(path)` — but
  it is very easy to write `h.FS().Stat(path)` and lose the privilege. Statting a root-owned
  file without sudo does not error loudly; it fails the existence/identity check, which in
  k0sctl manifested as **spurious re-uploads of root-owned files**. The compiler cannot tell a
  privileged read from an unprivileged one — both type-check. k0sctl hit this at exactly two
  sites (`phase/lock.go` tryLock and `host.go` FileChanged), both of which must use
  `h.Sudo().FS()`. Audit every read that was privileged in v0.x.
- **`h.Sudo()` is a `*Client`, not an FS.** `h.Sudo().Stat(...)` does not exist; you want
  `h.Sudo().FS().Stat(...)`. The rule of thumb: reads that need root use `h.Sudo().FS()`,
  ordinary reads use `h.FS()`, and *every mutation that was privileged in v0.x* uses
  `h.Sudo().FS()`.
- **Re-implemented shell helpers that silently change semantics.** See the package-manager and
  environment notes below — a hand-rolled replacement can compile and run yet behave
  differently (e.g. append-instead-of-replace).

The general lesson: the build verifies the API swap, not the *privilege* and not the
*behavior*. Budget a validation pass against a real target for anything that was privileged or
OS-specific in v0.x.

---

## Services: Configurer → `h.Sudo().Service(name)`

Init system operations are now accessed through `h.Sudo().Service(name)`, which
returns a `*rig.Service` backed by the detected init system (systemd, OpenRC, etc.).

**v0.x** (via Configurer)
```go
h.Configurer.UpdateServiceEnvironment(h, h.K0sServiceName(), h.Environment)
h.Configurer.DaemonReload(h)
h.Configurer.StartService(h, h.K0sServiceName())
h.Configurer.StopService(h, h.K0sServiceName())
```

**v2**
```go
svc, err := h.Sudo().Service(h.K0sServiceName())
if err != nil {
    return err
}
if err := svc.SetEnvironment(ctx, h.Environment); err != nil {
    return err
}
if err := svc.Start(ctx); err != nil {
    return err
}
```

`Service` methods: `Start`, `Stop`, `Restart`, `Enable`, `Disable`, `IsRunning`,
`SetEnvironment`.

To **clear** a service's environment, call `SetEnvironment` with an empty map — there
is no separate cleanup method:

```go
if err := svc.SetEnvironment(ctx, map[string]string{}); err != nil { ... }
```

### Daemon reload

`SetEnvironment` and `Start` reload the init system internally when it requires it
(e.g. systemd after a unit/env change), so you do **not** need a `DaemonReload` call
around those operations.

If you need an explicit, standalone reload (k0sctl keeps one as its own phase), the
capability is opt-in via a type assertion rather than a method on `Service`:

```go
mgr, err := h.Sudo().ServiceManager()   // the detected init system
if err != nil {
    return err
}
reloader, ok := mgr.(initsystem.ServiceManagerReloader)
if !ok {
    return nil   // init system doesn't support reload (e.g. not systemd) — nothing to do
}
return reloader.DaemonReload(ctx, h.Sudo())
```

---

## OS detection: `h.OSVersion` → `h.OS()`

`h.OSVersion` (of type `*rig.OSVersion`) is replaced by `h.OS()`, which returns
`(*os.Release, error)`. The release is lazily initialized and cached after the first
call.

**v0.x**
```go
// nil check used to test whether detection has run
if h.OSVersion == nil {
    return errors.New("OS not detected")
}
id := h.OSVersion.ID
version := h.OSVersion.Version

// IDLike was a space-separated string; split manually
for id := range strings.SplitSeq(h.OSVersion.IDLike, " ") {
    // fallback resolution
}
```

**v2**
```go
release, err := h.OS()
if err != nil {
    return err
}
id := release.ID
version := release.Version

// IDLike is already []string
for _, id := range release.IDLike {
    // fallback resolution
}
```

### Architecture

**v0.x** (via Configurer)
```go
arch := h.Configurer.Arch(h)  // returns string, e.g. "x86_64"
```

**v2** (normalized to GOARCH)
```go
release, err := h.OS()
if err != nil {
    return err
}
arch, err := release.Arch()  // returns "amd64", "arm64", "arm", "386"
if err != nil {
    return err
}
```

`Release.Arch()` normalizes the architecture string to GOARCH values.

### When you need to override or cache the detected OS

v0.x let you *mutate* `h.OSVersion.ID` — k0sctl's distro fallback resolution relied on
this. v2's `h.OS()` is lazily detected and memoized; you cannot reach in and change the
detected ID. There are two ways to handle this:

**1. Override at construction** — rig provides `rig.WithOSIDOverride(id)`, which forces
the detected `Release.ID` (useful when detection is unreliable or you want to pin a
distro):

```go
client, err := rig.NewClient(
    rig.WithConnection(conn),
    rig.WithOSIDOverride("ubuntu"),
)
```

**2. Keep your own release field** — if you need to *synthesize* or progressively
refine a release during a detection phase (k0sctl's case: resolving an unknown distro
through its `IDLike` chain), cache a `*os.Release` on your own host struct and populate
it once. This is a consumer-side pattern, not a rig API — rig's `h.OS()` gives you the
detected release; what you do with it afterwards is yours. k0sctl, for instance, exposes
its own `h.Arch()` that reads a cached release first and falls back to the live
`h.OSRelease()` provider, so repeated arch lookups don't re-run detection.

> Note the two distinct things named "OSRelease": rig's `Client.OSRelease()` is the
> provider *method* returning `(*os.Release, error)`; a `h.OSRelease` *field* (as in
> k0sctl) is a consumer's own cache. They are not the same symbol.

---

## Error sentinels

**v0.x**
```go
import "github.com/k0sproject/rig"

if errors.Is(err, rig.ErrCantConnect) {
    return errors.Join(retry.ErrAbort, err)
}
// Workaround: OpenSSH did not wrap host key errors with ErrCantConnect
if strings.Contains(err.Error(), "host key mismatch") {
    return errors.Join(retry.ErrAbort, err)
}
```

**v2**
```go
import rig "github.com/k0sproject/rig/v2"

if errors.Is(err, rig.ErrNonRetryable) {
    return errors.Join(retry.ErrAbort, err)
}
```

(`retry.ErrAbort` here is *your own* retry-loop sentinel — the symbol that tells your
retry code to stop. The rig side of the check is `rig.ErrNonRetryable`.) The same rig
sentinel is reachable two ways — pick whichever avoids an extra import:

- `rig.ErrNonRetryable` — re-exported from the root package
- `protocol.ErrNonRetryable` — the canonical definition in `protocol`

| v0.x | v2 |
|---|---|
| `rig.ErrCantConnect` | `rig.ErrNonRetryable` (alias of `protocol.ErrNonRetryable`) |

### Host key mismatch

rig v2's native SSH wraps host-key-mismatch errors with `ErrNonRetryable`
automatically, so the `errors.Is(err, rig.ErrNonRetryable)` check above is sufficient
in principle — the v0.x string-match (`strings.Contains(err.Error(), "host key mismatch")`)
is no longer *required*.

In practice you may still want to keep the string check as a defensive fallback, OR'd
with the sentinel — k0sctl chose to:

```go
if errors.Is(err, rig.ErrNonRetryable) || strings.Contains(err.Error(), "host key mismatch") {
    return errors.Join(retry.ErrAbort, err)
}
```

---

## Retry

rig v2 ships a `retry` package (`retry.Do`/`retry.DoWithContext`, `retry.DoFor`,
`retry.Get`, with options `retry.Delay`, `retry.MaxRetries`, `retry.Backoff`,
`retry.If`). You are not obligated
to use it, and it differs from a typical v0.x hand-rolled loop in two ways worth
knowing before you adopt it:

- **It uses backoff, not a fixed interval.** If your existing logic depends on a
  constant retry interval within a fixed timeout budget (e.g. "every 5s for 2 minutes"),
  rig's backoff will change the effective attempt count. k0sctl tried delegating to
  rig's retry and then reverted to its own constant-interval loop for exactly this
  reason — be deliberate about the switch.
- **There is no abort sentinel.** Instead of joining a `retry.ErrAbort`, you supply an
  `retry.If(func(error) bool)` predicate that decides whether an error is retryable:

```go
err := retry.DoWithContext(ctx, func(ctx context.Context) error {
    return doThing(ctx)
}, retry.If(func(err error) bool {
    return !errors.Is(err, rig.ErrNonRetryable)   // stop on non-retryable
}))
```

If your retry semantics are simple and already correct, keeping your own loop is a
perfectly valid migration choice.

---

## Connect / Disconnect / Reconnect

The `Connect` and `Disconnect` methods remain on `Client` and `ClientWithConfig`.
`Connect` now takes a `context.Context`.

**v0.x**
```go
if err := h.Connect(); err != nil { ... }
h.Disconnect()
if err := h.Connect(); err != nil { ... }
```

**v2**
```go
if err := h.Connect(ctx); err != nil { ... }
h.Disconnect()
if err := h.Connect(ctx); err != nil { ... }
```

After reconnect, all lazily-initialized services (FS, OS, PackageManager, InitSystem)
reinitialize automatically on first use.

### Protocol check before reconnect

`Protocol()` returns the protocol family: `"SSH"` for **both** native SSH and OpenSSH,
`"WinRM"`, or `"Local"`. If you need to distinguish native SSH from OpenSSH, use
`ProtocolName()` instead, which returns the specific implementation name.

k0sctl reconnects after writing `/etc/environment` only for the native SSH
implementation, because a new TCP connection is needed to inherit the updated
environment. Use `ProtocolName()` for this check:

```go
// v0.x — h.Connection.Protocol() returned "SSH" for native SSH only
if h.Connection.Protocol() == "SSH" { ... }

// v2 — Protocol() matches both SSH and OpenSSH; use ProtocolName() to target native SSH only
if h.ProtocolName() == "SSH" {
    h.Disconnect()
    if err := h.Connect(ctx); err != nil { ... }
}
```

---

## Package manager

**v0.x** (via Configurer, OS-specific)
```go
h.Configurer.InstallPackage(h, "curl")
```

**v2**
```go
pm := h.Sudo().PackageManager()        // package installation needs root
if err := pm.Update(ctx); err != nil { // refresh indexes first (apt update, etc.)
    return err
}
if err := pm.Install(ctx, "curl", "tar", "gzip"); err != nil {  // variadic — one batched call
    return err
}
```

Access it through `h.Sudo()` — installing packages requires privilege. The provider
lazy-initializes from the default registry (apt, yum, dnf, apk, chocolatey, etc.).
`Install` is variadic, so a single call replaces v0.x's per-package install loops.

> **This is a "may need reimplementation," not a guaranteed drop-in.** v0.x's distro
> install logic lived in rig's `os` modules, which v2 **dropped**. `pm.Install` covers the
> common case, but if you depended on distro-specific flags or non-interactive switches that
> the rig provider doesn't apply, you must re-add them yourself. k0sctl did exactly this —
> rather than rely solely on `pm.Install`, it re-implemented per-distro install commands
> inline on its own configurers, e.g. Debian/Ubuntu `DEBIAN_FRONTEND=noninteractive apt-get
> install -y -q`, Alpine `apk add --update`, Arch `pacman -S --noconfirm --noprogressbar`,
> EL `yum install -y`, SLES/openSUSE `zypper refresh` + `zypper -n install -y`, and explicit
> "not supported" errors for Flatcar/CoreOS and Windows. Audit whether the provider's default
> command matches what your old configurer emitted before deleting the per-distro code.

### Environment files: replace-or-append semantics

A related drop-in trap: v0.x's `Configurer.UpdateEnvironment` / `LineIntoFile` had
**replace-or-append** semantics (an existing `KEY=` line is replaced, not duplicated). A
naive reimplementation with `grep -qxF || echo >>` only *appends when absent* and will **not**
replace an existing key — a silent regression k0sctl introduced and then fixed. Use
`remotefs.PatchFile` with `ReplaceOrAppend(ByPrefix(key+"="))` to preserve the original
behavior. Like the package-manager case, this compiles and runs but behaves differently — it
needs a real-host check.

---

## Shell command construction: the `sh` package

v2 provides a `sh` package for building shell command strings safely, plus
`sh/shellescape` for low-level quoting and splitting. Together they replace
hand-rolled `fmt.Sprintf` + manual quoting and any vendored shell-escaping helpers
(k0sctl deleted its `internal/shell` package and its third-party `shellescape`
dependency in favour of these).

```go
import (
    "github.com/k0sproject/rig/v2/sh"
    "github.com/k0sproject/rig/v2/sh/shellescape"
)

// sh.Command quotes each argument and joins them into one safe command string
cmd := sh.Command("ip", "-o", "addr", "show", iface)   // args are escaped for you
out, err := h.ExecOutput(cmd)

// sh.Quote escapes a single value for interpolation
line := "export FOO=" + sh.Quote(value)

// shellescape covers parsing/quoting primitives
fields, err := shellescape.Split(rawArgs)   // split a command line into argv
quoted := shellescape.Join(fields)          // and back again
```

Prefer `sh.Command(name, args...)` over `fmt.Sprintf("%s %s", name, arg)` — it is the
single biggest reducer of quoting bugs in a migration. For multi-stage commands,
`sh.CommandBuilder` chains `.Arg`, `.Pipe`, `.OutToFile`, `.ErrToNull`, etc.

---

## HTTP reachability checks: `remotefs.HTTPStatusInsecure`

v0.x's `Configurer.HTTPStatus` is replaced by `remotefs.HTTPStatusInsecure`, which runs
an HTTP request *from the remote host* (POSIX `curl -k`, Windows
`Invoke-WebRequest -SkipCertificateCheck`) and returns the status code:

```go
import "github.com/k0sproject/rig/v2/remotefs"

code, err := remotefs.HTTPStatusInsecure(ctx, h.FS(), "https://localhost:6443/readyz")
if err != nil {
    return err
}
if code != 200 {
    return fmt.Errorf("not ready: HTTP %d", code)
}
```

`HTTPStatusInsecure` is the **only** variant — there is intentionally no secure
`HTTPStatus`. The reason is the common case it serves: probing services with
self-signed certificates, such as a freshly started kube-apiserver. A secure variant
would fail with a certificate error (curl exit 60) against exactly the endpoints you
most want to health-check, so the insecure behaviour is the default and the name makes
that explicit.

---

## Remote filesystem and the stdlib `fs` package

`remotefs.FS` implements the standard library `fs.FS` interface, which means the
entire `io/fs` ecosystem works against remote hosts without any adapters:

```go
fsys := h.FS()

// stdlib functions work directly
entries, err := fs.ReadDir(fsys, "/etc")
data, err := fs.ReadFile(fsys, "/etc/os-release")
err = fs.WalkDir(fsys, "/var/log", func(path string, d fs.DirEntry, err error) error {
    // ...
    return nil
})
matches, err := fs.Glob(fsys, "/etc/*.conf")
```

The `remotefs.OS` interface that is embedded in `remotefs.FS` is modelled directly
after the stdlib `os` package — the method signatures for `MkdirAll`, `Remove`,
`Rename`, `Chmod`, `WriteFile`, etc. match the stdlib equivalents. Code written
against `remotefs.OS` will look familiar to anyone who has used the `os` package.

Path handling is OS-aware: `fsys.Join`, `fsys.Dir`, and `fsys.Base` use the correct
separator for the remote host (backslash on Windows, slash everywhere else), so the
same code works on both Linux and Windows targets.

---

## SSH host key handling

v2's host key behaviour follows OpenSSH conventions and is controlled by the same
fields in `~/.ssh/config`:

- **`StrictHostKeyChecking no`** disables host key verification (permissive mode).
  Rig reads this from the parsed ssh_config and passes it through.
- **`UserKnownHostsFile`** selects which known_hosts file to use. The first valid
  entry in the list is used.
- **`HashKnownHosts yes`** causes new entries to be stored as hashed values.
- The `SSH_KNOWN_HOSTS` environment variable, if set, overrides the config file path.
  Setting it to an empty string disables host key checking entirely.

Host key mismatch errors are automatically wrapped with `ErrNonRetryable`, so the
v0.x string-match workaround (`strings.Contains(err.Error(), "host key mismatch")`) is
no longer required — an `errors.Is(err, rig.ErrNonRetryable)` check covers it. See
[Error sentinels](#host-key-mismatch) above for why you might still keep it defensively.

For programmatic key pinning, use `hostkey.StaticKeyCallback`:

```go
import "github.com/k0sproject/rig/v2/protocol/ssh/hostkey"

cb := hostkey.StaticKeyCallback(trustedPublicKey)
// pass cb as an AuthMethod or wire it into a custom connection factory
```

---

## SSH config file coverage

The `sshconfig` package parses the full OpenSSH `ssh_config` spec, but what gets
consumed depends on which protocol implementation you use.

### Native SSH (`protocol/ssh`)

The native SSH implementation uses `golang.org/x/crypto/ssh` under the hood and
consumes these fields from the parsed config:

| Field | Effect |
|---|---|
| `Hostname` | Resolves host aliases to real addresses (see below) |
| `Port` | Port number |
| `User` | Login user |
| `IdentityFile` | Private key paths |
| `StrictHostKeyChecking` | Permissive host key checking when set to `no` |
| `UserKnownHostsFile` | Known hosts file path |
| `HashKnownHosts` | Hash new known-hosts entries |

All other fields (algorithm selection such as `KexAlgorithms`, `Ciphers`, `MACs`,
`HostKeyAlgorithms`; proxy settings `ProxyJump`, `ProxyCommand`; `Compression`;
`ConnectTimeout`; etc.) are parsed correctly but are **not applied** to the
underlying Go SSH client. The parser is complete; the consumer is not yet.

If you rely on any of these fields in your `~/.ssh/config`, use the **OpenSSH
protocol** (`openSSH` in YAML, `openssh.Config` in Go) instead. The OpenSSH
implementation delegates to the system `ssh` binary, which applies the full config
natively and inherits every option including `ProxyJump`, algorithm selection, and
GSSAPI.

### Host alias resolution

`ssh.Config.Address` can be a Host alias from `~/.ssh/config`, not just a raw IP or
DNS name. When the alias matches a `Host` block that has a `Hostname` directive, rig
resolves it automatically:

```
# ~/.ssh/config
Host prod
    Hostname 10.0.1.50
    User admin
    IdentityFile ~/.ssh/prod_key
```

```go
// Address "prod" is resolved to 10.0.1.50, user and key file are picked up too
cfg := ssh.Config{Address: "prod"}
```

The original alias is kept as the display name in logs; the resolved hostname is used
for the actual TCP connection.

### OpenSSH (`protocol/openssh`)

The OpenSSH implementation does not use the `sshconfig` parser at all — it passes the
target host to the system `ssh` binary with `-F` pointing to the default config files,
so the binary applies every option it knows about natively. The rig `openssh.Config`
struct exposes only the fields that rig needs to construct the command line; everything
else is left to `~/.ssh/config`.

---

## Streaming I/O: `ExecStreams` → `h.Proc()`

`h.ExecStreams(cmd, stdin, stdout, stderr, opts...)` is removed. Use `h.Proc(cmd)`
to get a `*cmd.Proc` (modelled after `os/exec.Cmd`), wire the streams, and start it:

**v0.x**
```go
runner, err := h.ExecStreams("kubectl apply -f -", io.NopCloser(manifest), &stdout, &stderr, exec.Sudo(h))
if err != nil {
    return err
}
err = runner.Wait()
```

**v2**
```go
proc := h.Sudo().Proc("kubectl apply -f -")
proc.Stdin = manifest          // io.Reader, not io.ReadCloser
proc.Stdout = &stdout
proc.Stderr = &stderr
waiter, err := proc.Start(ctx)
if err != nil {
    return err
}
err = waiter.Wait()
```

`Proc.Stdin` is `io.Reader` — drop any `io.NopCloser` wrappers. `Start` returns a
`protocol.Waiter`; call `Wait()` on it to block until the command exits.

---

## OS module registry

v0.x shipped a `rig/os/registry` package that maintained a global map of OS module
builders keyed on `rig.OSVersion`. Callers registered distro-specific configurers
into this registry and retrieved them via `registry.GetOSModuleBuilder(*h.OSVersion)`.

**v2 removes this package entirely.** The `rig/v2/os` package contains only `Release`
(the OS detection result) and detection helpers — no `Host` interface, no `Linux` type,
no registry.

Consumers that used the registry pattern must own their own configurer registry. This
is the shape k0sctl shipped — note the mutex (registration happens from `init()`
functions across many files and can race) and the nil-release guard:

```go
// configurer/registry.go
package configurer

import (
    "sync"

    rigos "github.com/k0sproject/rig/v2/os"
)

type osModuleEntry struct {
    match   func(*rigos.Release) bool
    builder func() any
}

var (
    mu              sync.RWMutex
    osModuleBuilders []osModuleEntry
)

func RegisterOSModule(match func(*rigos.Release) bool, builder func() any) {
    mu.Lock()
    defer mu.Unlock()
    osModuleBuilders = append(osModuleBuilders, osModuleEntry{match, builder})
}

func ResolveOSModule(release *rigos.Release) (func() any, bool) {
    if release == nil {
        return nil, false
    }
    mu.RLock()
    defer mu.RUnlock()
    for _, entry := range osModuleBuilders {
        if entry.match(release) {
            return entry.builder, true
        }
    }
    return nil, false
}
```

Matcher predicates now receive `*rigos.Release` (the full struct) rather than a plain
ID string, which allows matching on `Name`, `IDLike`, or any other field from
`/etc/os-release`.

Note that `IDLike` is `[]string` in v2 (split on whitespace), whereas v0.x delivered
it as a single space-separated string. Update matchers accordingly.

---

## Logging

v0.x had a global `rig.SetLogger(l)` that routed all rig internals through the
provided logger. **v2 removes the global setter.** Logging is slog-based and
configured per client, via the `rig.WithLogger(logger)` client option.

The cleanest place to inject it is at **client construction** — this is what k0sctl
does:

```go
import (
    "log/slog"
    rig "github.com/k0sproject/rig/v2"
)

client, err := rig.NewClient(
    rig.WithConnectionFactory(&h.CompositeConfig),  // or WithConnection(conn)
    rig.WithLogger(slog.New(yourHandler)),
)
// ...then later:
err = client.Connect(ctx)
```

> **Watch the two `Connect` signatures.** Client options can *only* be passed at
> construction or to `ClientWithConfig.Connect(ctx, opts...)`. The bare
> `Client.Connect(ctx)` takes **no** options — if you hold a `*Client` and try
> `client.Connect(ctx, rig.WithLogger(...))` it won't compile. Inject the logger when
> you build the client.

If you embed `rig.ClientWithConfig` in a host struct, you can alternatively override
`Connect` (which *does* accept options) to inject the logger consistently:

```go
func (h *Host) Connect(ctx context.Context) error {
    return h.ClientWithConfig.Connect(ctx, rig.WithLogger(yourAppLogger))
}
```

To bridge rig's slog output into an existing logrus setup, use a slog handler that
forwards to logrus (e.g. `github.com/samber/slog-logrus/v2`):

```go
import sloglogrus "github.com/samber/slog-logrus/v2"

rigLogger := slog.New(sloglogrus.Option{
    Level:  slog.LevelDebug,
    Logger: log.StandardLogger(),  // the logrus singleton
}.NewLogrusHandler())
```

This pattern ensures that any level or hook changes applied to the logrus standard
logger are reflected automatically, since the handler holds a pointer to the singleton.

---

## Test and mock migration

Getting production code to compile is only half the job. In k0sctl the test suite was a
substantial second effort that the API-swap sections above do not cover — budget for it
separately, because a green `go build` with a red `go test` is the normal mid-migration state.

**Mocks change shape.** v0.x tests mocked a `rig.Connection` (or an `exec`-based host). v2
tests build a `*rig.Client` around a mock connection/runner from the `rigtest` package:

```go
import "github.com/k0sproject/rig/v2/rigtest"

mr := rigtest.NewMockRunner()                       // or rigtest.NewMockConnection()
mr.AddCommandOutput(rigtest.Contains("date -u +%s"), "1700000000")  // match by substring/regex
client, _ := rig.NewClient(
    rig.WithConnection(mr.MockConnection),
    rig.WithRemoteFSProvider(/* ... */),
)
h.Client = client                                   // inject into your host struct
```

`MockRunner` matches commands with matchers (`rigtest.Contains`, `rigtest.Equal`, regexp) and
returns canned output/exit codes — there is no `exec.Sudo`-style option to assert on anymore,
since sudo is now a runner decorator.

**The high-value gotcha: relocated work breaks output-based mocks silently.** When a
`Configurer` method becomes an `h.FS()` call, the *command that actually runs* changes, so any
mock keyed on the old command stops matching and the test exercises a default/empty result.
The concrete example from k0sctl: `SystemTime` injection stopped working because production now
calls `h.FS().SystemTime()`, which shells out to `date -u +%s` — the test had to be rewritten to
feed `MockRunner` a faked timestamp via `AddCommandOutput(rigtest.Contains("date -u +%s"), ...)`.
Re-derive the underlying command for any relocated method and re-key its mock.

**Expect orphaned tests and imports.** Removing thin Configurer wrappers (LookPath, Hostname,
Stat, …) deletes the methods their unit tests targeted. Those tests and their now-unused imports
must be removed, not just left to fail — `golangci-lint` will flag the orphaned imports. In
k0sctl this hollowed out whole files (`configurer/windows_test.go` collapsed to a single test;
`configurer/linux/linux_test.go` lost its LookPath tests). The compiler catches the dead
references; the linter catches the leftover imports.

---

## Summary of renamed / moved symbols

| v0.x | v2 |
|---|---|
| `rig.Connection` | `rig.ClientWithConfig` (embed); `rig.CompositeConfig` (config only) |
| `rig.SSH` | `ssh.Config` (`protocol/ssh`) |
| `rig.OpenSSH` | `openssh.Config` (`protocol/openssh`) |
| `rig.WinRM` | `winrm.Config` (`protocol/winrm`) |
| `rig.OSVersion` | `os.Release` (`os` sub-package) |
| `h.OSVersion.IDLike` (string) | `release.IDLike` ([]string) |
| `h.SudoFsys()` | `h.Sudo().FS()` |
| `h.Fsys()` | `h.FS()` |
| `github.com/k0sproject/rig/exec` | `github.com/k0sproject/rig/v2/cmd` |
| `exec.Sudo(h)` option | `h.Sudo()` cloned client |
| `h.Upload(src, dst, perm, ...)` | `remotefs.Upload(fsys, src, dst, ...)` |
| `rig.ErrCantConnect` | `rig.ErrNonRetryable` (alias of `protocol.ErrNonRetryable`) |
| `rigfs.Fsys` | `remotefs.FS` |
| `h.ExecOutput(cmd)` | `h.ExecOutput(cmd)` (unchanged); `h.ExecOutputContext(ctx, cmd)` for cancellation |
| `h.Exec(cmd)` | `h.Exec(cmd)` (unchanged); `h.ExecContext(ctx, cmd)` for cancellation |
| `h.Connect()` | `h.Connect(ctx)` |
| `h.ExecStreams(cmd, stdin, stdout, stderr, opts...)` | `h.Proc(cmd)` + wire streams + `.Start(ctx)` |
| `c.HostPath(h, p)` / `c.Quote(s)` | `fsys.NativePath(p)` / `fsys.ShellQuote(s)` |
| `c.HTTPStatus(h, url)` | `remotefs.HTTPStatusInsecure(ctx, fsys, url)` |
| `c.DownloadURL(h, url, dst)` | `fsys.DownloadURL(url, dst)` |
| `c.CheckPrivilege(h)` | `h.CheckSudo(ctx)` |
| `c.InstallPackage(h, pkg)` | `h.Sudo().PackageManager().Install(ctx, pkg...)` |
| `internal/shell` / vendored shellescape | `sh` + `sh/shellescape` |
| `rig/v2/os.Host` interface | removed — build your own or use `cmd.SimpleRunner` |
| `rig/v2/os.Linux` type | removed |
| `rig/v2/os/registry` package | removed — build your own configurer registry |
| `rig.SetLogger(l)` global | removed — use `rig.WithLogger(l)` at construction |
