---
agent: kimmo
created: 2026-05-26T15:03:33+03:00
importance: 0.50
tags:
  - rig-v2
  - api-polish
  - completed
---

## Completed Rig v2 API Audit Tasks (2026-05-26)

Completed all 5 tasks from todo board's "next" column:

**t-050: Audit hidden remote work** - Methods like `Client.FS()`, `Client.OS()`, `Client.PackageManager()` look cheap but do lazy-init OS/PM detection on first call. Documented as acceptable design; recommended adding docstring notes about lazy-init behavior.

**t-051: Context-first APIs** - Audit shows good compliance. Blocking operations consistently use context-first pattern. Minor gaps: `IsConnected()` and `ExecInteractive()` don't accept context, but these are intentional design choices.

**t-052: Review remotefs.OS API** - Well-designed remote stdlib API with method names following os package conventions. Coherent cross-platform (POSIX/Windows) semantics. No k0sctl-specific helpers - maintains general-purpose design.

**t-053: Review provider injection ergonomics** - Clean functional provider APIs. Injection via `WithOSReleaseProvider()`, etc. is ergonomic. Recommendation: add godoc examples and document `rigtest` helpers more prominently.

**t-072: Document timeout policy** - Created comprehensive TIMEOUT_POLICY.md documenting:
  - `Client.Connect(ctx)` - 10s default timeout if no deadline
  - Service operations - inherit deadline from ctx, no default
  - `IsConnected()` - may block, no timeout, caller should wrap
  - Lazy-init methods - no context, detection is sync
  - Filesystem ops - no context, sync operations

Created deliverables:
- API_AUDIT_FINDINGS.md - comprehensive review of all 5 areas
- TIMEOUT_POLICY.md - detailed timeout behavior and consumer guidance for k0sctl migration
