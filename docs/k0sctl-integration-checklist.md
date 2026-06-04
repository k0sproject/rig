# k0sctl Integration Validation Checklist

This document tracks the concrete validation status of each k0sctl workflow against the rig v2 API. For each workflow it lists the v2 entry point, a pass criterion observable in rig's integration test suite (`test/rig_test.go`, `make inttest`), and whether the rig side is **READY** or has remaining **GAP**s.

k0sctl's migration work (adapting call sites) is out of scope here. The focus is whether rig v2 exposes the right API surface.

---

## 1. Connect

**v2 entry point:** `client.Connect(ctx)` / `ClientWithConfig` (YAML-embeddable)

**Validation:** connect to an SSH, OpenSSH, and WinRM host; `client.IsConnected()` returns `true`; `client.ExecOutput(ctx, "echo ok")` returns `"ok"`.

**Status:** READY
- `ClientWithConfig` supports inline YAML unmarshalling (same field layout as v0.x `Connection`).
- `CompositeConfig` selects the protocol from the YAML fields present.
- `Client.IsConnected()` is on `protocol.Connection` and forwarded from `Client`.

---

## 2. Sudo

**v2 entry point:** `sudoClient := client.Sudo()`

**Validation:** `sudoClient.ExecOutput(ctx, "id -u")` returns `"0"` on a non-root SSH session; subsequent calls reuse the memoized clone.

**Status:** READY
- `Client.Sudo()` returns a memoized clone whose runner wraps every command with the detected sudo mechanism.
- v0.x pattern `exec.Sudo(h)` at many call sites must be replaced in k0sctl; no compat shim exists in rig — this is a k0sctl migration item, not a rig gap.

---

## 3. Upload

**v2 entry point:** `remotefs.Upload(client.Sudo().FS(), localSrc, remoteDst, remotefs.WithPermissions(mode))`

**Validation:** upload a local file to a temp path on the remote; confirm checksum matches via `fs.Sha256(dst)`.

**Status:** READY
- `remotefs.Upload` is implemented with atomic temp-file rename and optional permission override.
- `remotefs.WithPermissions(mode fs.FileMode)` sets the destination file mode.
- v0.x signature `h.Upload(src, dst, perm, exec.Sudo(h))` is replaced; k0sctl must update call sites.

---

## 4. Remote FS Operations

**v2 entry point:** `fs := client.FS()` or `client.Sudo().FS()`, which returns `remotefs.FS`

**Validation:** exercise the methods k0sctl uses most: `FileExist`, `MkdirAll`, `WriteFile`, `ReadFile`, `Remove`, `Rename`, `Chmod`, `Chown`, `LookPath`, `TempDir`, `MkdirTemp`, `CreateTemp`, `Getenv`, `Hostname`, `UserHomeDir`.

**Status:** READY
- `remotefs.FS` embeds `remotefs.OS` which covers all of the above.
- `remotefs.ReplaceOrAppend` provides grep-and-patch file editing.
- HTTP operations: `fs.DownloadURL(url, dst)` and the free function `remotefs.HTTPStatus(ctx, fs, url)` for health checks.
- Path utilities (`Join`, `Dir`, `Base`) are OS-aware (POSIX vs Windows separators).

---

## 5. Service Environment

**v2 entry point:** `svc, err := client.Sudo().Service(name)` → `svc.SetEnvironment(ctx, env)`

**Validation:** call `SetEnvironment` with a test key; confirm the environment file exists at the path reported by the init system; call a `daemon-reload` equivalent; verify the env var is visible in a newly started process.

**Status:** READY
- `Service.SetEnvironment(ctx, map[string]string)` delegates to `initsystem.ServiceEnvironmentManager` (implemented by Systemd and OpenRC).
- Cleanup of rig-written env files stays in k0sctl (`CleanupServiceEnvironment`) — not a rig gap.

---

## 6. Package Install

**v2 entry point:** `pm, err := client.PackageManager()` → `pm.Install(ctx, pkg)`

**Validation:** install a known package (e.g. `curl`) on a test host; verify the binary appears in `LookPath`.

**Status:** READY
- `Client.PackageManager()` lazy-initializes from the default provider registry (apt, yum, dnf, apk, chocolatey, etc.).
- `PackageManager.Install(ctx, pkg)` is implemented for all supported distros.

---

## 7. OS Detection

**v2 entry point:** `release, err := client.OS()`

**Validation:** after `Connect`, `release.ID` is a non-empty distro string; `release.Arch()` returns a normalized GOARCH (`amd64`, `arm64`, `arm`, `386`).

**Status:** READY
- `Client.OS()` returns `*os.Release` with lazy init.
- `Release.Arch()` returns a normalized GOARCH string, populated from `uname -m` (POSIX) or `$env:PROCESSOR_ARCHITECTURE` (Windows).
- `WithOSIDOverride(id)` client option overrides `Release.ID` after detection — needed by k0sctl to handle fallback/override during the detection phase.

**Migration notes for k0sctl:**
- `Release.IDLike` changed type: v0.x `string` → v2 `[]string`. k0sctl's `strings.SplitSeq(h.OSVersion.IDLike, " ")` must change to iterating the slice directly.
- v0.x `h.OSVersion == nil` pattern (used to check if detection has run) has no equivalent; use `client.IsConnected()` or call `client.OS()` eagerly in the DetectOS phase.

---

## 8. Windows Paths

**v2 entry point:** `fs := client.FS()` on a Windows host; `fs.Join(...)`, `fs.Dir(path)`, `fs.Base(path)`

**Validation:** on a WinRM host, `fs.Join("C:\\Users", "foo")` returns `C:\Users\foo`; `fs.Dir("C:\\Users\\foo\\bar")` returns `C:\Users\foo`; path-aware `MkdirAll` and `WriteFile` accept backslash paths.

**Status:** READY
- `WinFS` implements the `OS` interface with Windows-native path semantics.
- `remotefs.WinFS` uses PowerShell via the `rigrcp` daemon for all FS operations.

---

## 9. WinRM

**v2 entry point:** `protocol/winrm.Connection` / `CompositeConfig{WinRM: &winrm.Config{...}}`

**Validation:** connect to a WinRM host; execute a command; upload a file via `remotefs.Upload`; install a package via chocolatey; verify `client.OS().ID` is `"windows"`.

**Status:** READY
- `protocol/winrm` implements `protocol.Connection`.
- WinRM FS operations use the `rigrcp` PowerShell daemon.
- CI runs integration tests against a real `windows-2022` runner (`.github/workflows/integration.yml`).

---

## 10. OpenSSH

**v2 entry point:** `protocol/openssh.Connection` / `CompositeConfig{OpenSSH: &openssh.Config{...}}`

**Validation:** connect using the system `ssh` binary; verify that a host key mismatch causes an error that wraps `protocol.ErrNonRetryable`; multiplexing control socket is cleaned up after `Disconnect`.

**Status:** READY
- `protocol/openssh` parses `~/.ssh/config` and system config via the `sshconfig` package.
- Host key mismatch returns an error wrapping `protocol.ErrNonRetryable` (prevents silent retry).
- Multiplexing (`EnableMultiplex`) is supported.

---

## 11. Native SSH

**v2 entry point:** `protocol/ssh.Connection` / `CompositeConfig{SSH: &ssh.Config{...}}`

**Validation:** connect using the Go SSH library; verify keepalive fires when configured; verify `IsConnected()` returns `false` after the underlying TCP connection drops.

**Status:** READY
- `protocol/ssh` uses `golang.org/x/crypto/ssh`.
- SSH keepalive is available via `ssh.WithKeepAlive(duration)`.
- `Connection.IsConnected()` probes the underlying SSH channel.

---

## 12. Reconnect

**v2 entry point:** `client.Disconnect()` then `client.Connect(ctx)`

**Validation:** connect; run a command; disconnect; reconnect; run the same command again successfully. Confirm that lazy-initialized services (FS, OS, etc.) reinitialize after reconnect.

**Status:** READY
- `Client.Disconnect()` tears down the underlying connection.
- Subsequent `Connect` re-runs `setupConnection`.
- All lazy providers (FS, OS, PackageManager, InitSystem) reinitialize on first use after reconnect.
- `ClientWithConfig.UnmarshalYAML` disconnects any existing client before replacing config, enabling live re-configuration.

---

## 13. Error Handling

**v2 entry point:** `protocol.ErrNonRetryable`, `protocol.ErrValidationFailed`

**Validation:** a connection to an unreachable host wraps `protocol.ErrNonRetryable`; config validation errors wrap `protocol.ErrValidationFailed`; both are exported from the top-level `rig` package as `rig.ErrNonRetryable` and `rig.ErrValidationFailed`.

**Status:** READY
- `protocol.ErrNonRetryable` is the v2 replacement for v0.x `rig.ErrCantConnect`. k0sctl must update error sentinel checks.
- `rig.ErrNonRetryable` and `rig.ErrValidationFailed` are re-exported from the root package.
- `protocol.ErrAbort` is **not** a v2 export; CLAUDE.md is stale on this point — the correct name is `ErrNonRetryable`.

---

## Summary

| Workflow         | Rig v2 Status | k0sctl Migration Work Required |
|------------------|:-------------:|-------------------------------|
| Connect          | READY         | Replace `Connection` embed with `ClientWithConfig` |
| Sudo             | READY         | Replace `exec.Sudo(h)` at all call sites with `h.Sudo()` |
| Upload           | READY         | Replace `h.Upload(src,dst,perm,exec.Sudo(h))` |
| Remote FS ops    | READY         | Replace `SudoFsys()` with `h.Sudo().FS()` |
| Service env      | READY         | Wire `Service.SetEnvironment`; keep cleanup in k0sctl |
| Package install  | READY         | Replace distro configurer install calls |
| OS detection     | READY         | Adapt `IDLike` (`string`→`[]string`); drop `OSVersion==nil` pattern |
| Windows paths    | READY         | No change required |
| WinRM            | READY         | Update config embed type |
| OpenSSH          | READY         | Update config embed type |
| Native SSH       | READY         | Update config embed type |
| Reconnect        | READY         | No change required |
| Error handling   | READY         | Replace `ErrCantConnect` with `ErrNonRetryable` |

All rig v2 API surface required for k0sctl migration is present. The remaining work is entirely in k0sctl (updating call sites). See the todo board (`todo/next/migration-guide.md`) for the companion migration guide task.
