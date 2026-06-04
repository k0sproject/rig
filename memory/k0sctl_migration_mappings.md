---
name: k0sctl-migration-mappings
description: Concrete v0.x → v2 API mapping for k0sctl migration, including tricky protocol and IDLike changes
metadata:
  type: project
---

## Embedding

| v0.x | v2 |
|---|---|
| `rig.Connection \`yaml:",inline"\`` | `rig.ClientWithConfig \`yaml:",inline"\`` |

## Protocol Config Construction

| v0.x | v2 |
|---|---|
| `rig.SSH{...}` | `ssh.Config{...}` (`protocol/ssh`) |
| `rig.OpenSSH{...}` | `openssh.Config{...}` (`protocol/openssh`) |
| `rig.WinRM{...}` | `winrm.Config{...}` (`protocol/winrm`) |

## Sudo

| v0.x | v2 |
|---|---|
| `exec.Sudo(h)` at ~100 call sites | `h.Sudo()` returns memoized cloned client; all calls on clone use sudo |

## Command Execution

| v0.x | v2 |
|---|---|
| `h.Exec(cmd, opts...)` | `h.Exec(ctx, cmd, opts...)` — ctx is first arg |
| `h.ExecOutput(cmd, opts...)` | `h.ExecOutput(ctx, cmd, opts...)` |
| `h.ExecStreams(cmd, stdin, out, err, opts...)` | `h.ExecStreams(ctx, cmd, stdin, out, err, opts...)` |
| `exec.Sudo(h)` option | dropped — use `h.Sudo().Exec(...)` |
| `github.com/k0sproject/rig/exec` | `github.com/k0sproject/rig/v2/cmd` |

Most exec options survive the rename: `Redact`, `HideOutput`, `Stdout`, `Stderr`, `Stdin`, `AllowWinStderr`, `HideCommand`, `RedactString`, `LogError`. Only `exec.Sudo` is gone.

`exec.DisableRedact` global → v2 equivalent needed as per-client config.

## Filesystem

| v0.x | v2 |
|---|---|
| `h.SudoFsys()` | `h.Sudo().FS()` |
| `h.Fsys()` | `h.FS()` |
| `rigfs.Fsys` | `remotefs.FS` (implements `fs.FS` + rich `remotefs.OS` interface) |
| `fs.WalkDir(h.SudoFsys(), ...)` | `fs.WalkDir(h.Sudo().FS(), ...)` — stdlib works unchanged |

## Upload

| v0.x | v2 |
|---|---|
| `h.Upload(src, dst, perm, exec.Sudo(h))` | `remotefs.Upload(h.Sudo().FS(), src, dst, remotefs.WithPermissions(perm))` |

## OS Detection

| v0.x | v2 |
|---|---|
| `h.OSVersion` (`*rig.OSVersion`, may be nil) | `h.OS()` — call eagerly, no nil check needed |
| `h.OSVersion.IDLike` (space-separated string) | `release.IDLike` (`[]string`) — split is done for you |
| `h.OSVersion.ID`, `.Version`, `.Name` | same fields on `release.OSRelease` |

## Error Sentinel

| v0.x | v2 |
|---|---|
| `rig.ErrCantConnect` | `rig.ErrNonRetryable` (or `protocol.ErrAbort`) |
| `strings.Contains(err.Error(), "host key mismatch")` workaround | can be dropped — ErrNonRetryable wraps it properly |

## Connection Lifecycle

| v0.x | v2 |
|---|---|
| `h.Connect()` | `h.Connect(ctx)` |
| `h.Disconnect()` | `h.Disconnect()` — unchanged |

## Reconnect / Protocol Check (TRICKY)

v0.x: `h.Connection.Protocol() == "SSH"` matched **native SSH only** (not OpenSSH).

v2: `h.Protocol()` returns `"SSH"` for **both** native SSH and OpenSSH.
Use `h.ProtocolName() == "SSH"` to target native SSH only in v2.

**Why:** k0sctl reconnects after `/etc/environment` changes only for native SSH connections where the session persists env at connect time. OpenSSH reconnects naturally. Using the wrong check in v2 would cause unnecessary reconnects on OpenSSH hosts.

## Services

| v0.x | v2 |
|---|---|
| `h.Configurer.StartService(h, name)` | `h.Sudo().Service(name).Start(ctx)` |
| `h.Configurer.StopService(h, name)` | `h.Sudo().Service(name).Stop(ctx)` |
| `h.Configurer.DaemonReload(h)` | implicit — v2 calls it automatically when systemd requires it |
| `h.Configurer.UpdateServiceEnvironment(h, name, env)` | `h.Sudo().Service(name).SetEnvironment(ctx, env)` — DaemonReload implicit |
| `h.Configurer.CleanupServiceEnvironment(h, name)` | `h.Sudo().Service(name).SetEnvironment(ctx, nil)` or similar |

## Configurer Shrinkage

`remotefs.OS` directly provides these — they can be removed from k0sctl's Configurer interface:
`FileExist`, `WriteFile`, `MkDir`, `DeleteFile`, `MoveFile`, `Stat`, `LookPath`, `Hostname`, `TempDir`, `TempFile`, `Chmod`, `ReadFile`, `CommandExist`

## Misc Facts

- rig v2.0.0-beta.1 has been tagged.
- `docs/MIGRATING-from-v0.x.md` exists in the rig repo.
- Integration checklist (`docs/k0sctl-integration-checklist.md`) is intentionally git-ignored from the rig repo.
- `remotefs.FS` implements `fs.FS` — all stdlib `fs.*` helpers work unchanged.
- `remotefs.OS` mirrors stdlib `os` package signatures.
