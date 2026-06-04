---
name: k0sctl-usage-patterns
description: k0sctl v0.21.x concrete usage patterns for rig — call signatures, embed style, error handling, and service routing
metadata:
  type: project
---

## Host Embedding

`type Host struct { rig.Connection \`yaml:",inline"\`; ... }`

The `rig.Connection` is embedded inline. In v2 this becomes `rig.ClientWithConfig` with the same `yaml:",inline"` tag.

## Command Execution

- All exec methods accept variadic `exec.Option` as last arg.
- `exec.Sudo(h)` appears at ~100 call sites (69 direct Exec/ExecOutput, 7 ExecStreams, rest via Configurer methods).
- `h.Exec(cmd, exec.Sudo(h))` — fire-and-forget with sudo.
- `h.ExecOutput(cmd, exec.Sudo(h))` — returns (stdout string, error).
- `h.ExecStreams(cmd, stdin, &stdout, &stderr, exec.Sudo(h))` — returns a waitable Proc; `cmd.Wait()` called after.

### Other exec options used by k0sctl

| Option | Count | Notes |
|---|---|---|
| `exec.HideOutput()` | 4 | Sensitive output: kubeconfig, tokens |
| `exec.LogError(true)` | 3 | All on Upload() calls only |
| `exec.Stdin(content)` | 3 | WriteFile (linux/windows), kubectl apply -f - |
| `exec.AllowWinStderr()` | 2 | Windows-only, guarded by `if h.IsWindows()` |
| `exec.HideCommand()` | 1 | lock.go Stat() — hides path from debug log |
| `exec.RedactString(content)` | 1 | windows WriteFile — redacts content from logs |

## Upload

Signature always: `h.Upload(src string, dst string, perm fs.FileMode, exec.Sudo(h))`

Three call sites; all pass `exec.LogError(true)` alongside `exec.Sudo(h)`.

## Filesystem

`h.SudoFsys()` returns `rigfs.Fsys` implementing `fs.FS` plus `MkDirAll`, `OpenFile`, `Stat`.

Usage patterns:
- `h.SudoFsys().MkDirAll(path, perm)` — create directory tree
- `h.SudoFsys().OpenFile(path, flags, perm)` — open/create file for writing
- `h.SudoFsys().Stat(path)` — file existence / metadata check
- `fs.WalkDir(h.SudoFsys(), root, fn)` — stdlib walk over remote tree
- `fs.ReadDir(h.SudoFsys(), path)` — read directory entries

## OS Detection

- `h.OSVersion` is `*rig.OSVersion`, nil-checked at call sites before use.
- `h.OSVersion.IDLike` is a **space-separated string** — callers do `strings.Split(h.OSVersion.IDLike, " ")` or `strings.SplitSeq`.
- `h.OSVersion.ID`, `.Version`, `.Name` also used.
- `h.ResolveConfigurer()` called after `h.Connect()` to pick the right per-distro configurer.

## Error Sentinel

- `errors.Is(err, rig.ErrCantConnect)` — 3 sites: connect phase, prepare phase, statusfunc.
- `strings.Contains(err.Error(), "host key mismatch")` — OpenSSH workaround, string-match because ErrCantConnect doesn't wrap it.

## Config Construction (programmatic)

```go
rig.Connection{SSH: &rig.SSH{Address: addr, Port: port}}
```

## Reconnect Patterns

Two distinct patterns:

1. **Deliberate reconnect** — after writing `/etc/environment`, only for native SSH:
   ```go
   if h.Connection.Protocol() == "SSH" {
       h.Disconnect()
       // ... wait ...
       h.Connect()
   }
   ```

2. **Recovery reconnect** — in statusfunc polling loop after detecting a disconnected host.

## Services (via Configurer)

All service operations are routed through `h.Configurer`, not rig directly:
- `h.Configurer.StartService(h, name)`
- `h.Configurer.StopService(h, name)`
- `h.Configurer.UpdateServiceEnvironment(h, name, envMap)`
- `h.Configurer.DaemonReload(h)`
- `h.Configurer.CleanupServiceEnvironment(h, name)`

## Configurer Methods That Map to remotefs.OS

These are duplicated from rig operations and can be removed in v2 migration:
`FileExist`, `WriteFile`, `MkDir`, `DeleteFile`, `MoveFile`, `Stat`, `LookPath`, `Hostname`, `TempDir`, `TempFile`, `Chmod`, `ReadFile`, `CommandExist`
