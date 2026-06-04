---
name: project_migration_guide
description: v0.x→v2 migration guide location, scope, and key decisions made during authoring
metadata:
  type: project
---

Migration guide lives at `docs/MIGRATING-from-v0.x.md` in the rig repo.

Tagged v2.0.0-beta.1 before writing it.

`docs/k0sctl-integration-checklist.md` is intentionally untracked from the rig repo (covered its purpose, then removed from git).

**Why:** Guide was written with k0sctl as the primary consumer in mind. All rig v2 API surface required for k0sctl is present; remaining work is entirely in k0sctl (updating call sites).

**How to apply:** When continuing migration work, the guide is the authoritative translation reference. Don't reconstruct mapping tables from scratch — read the guide first.

## Key decisions recorded in the guide

- `ClientWithConfig` is for YAML embedding; `rig.NewClient(rig.WithConnection(...))` is for programmatic use
- `exec` package renamed to `cmd`; only `exec.Sudo(h)` is gone — other options survive
- `remotefs.Upload` has no context (by design; cancel via connection)
- `Protocol()` returns `"SSH"` for both native and OpenSSH; use `ProtocolName() == "SSH"` to target native SSH only (matters for reconnect-after-env-write pattern)
- `DaemonReload` is implicit in v2 — no separate call needed
- `IDLike` changed from `string` (space-separated) to `[]string`
- `ErrCantConnect` → `ErrNonRetryable`; OpenSSH host-key-mismatch string-match workaround dropped
- `remotefs.FS` implements `fs.FS` — stdlib `fs.WalkDir`, `fs.ReadFile`, `fs.Glob` work directly
- `remotefs.OS` mirrors stdlib `os` package signatures (MkdirAll, Remove, Rename, Chmod, WriteFile, etc.)
