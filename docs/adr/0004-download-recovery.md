> 历史设计记录；当前开发规范统一见 [AGENTS.md](../../AGENTS.md)。本文记录当时的选择与证据，不限制后续重构。

# ADR 0004: Download recovery and state transitions

## Status

Accepted.

Amended 2026-10-18: SQLite migration v12 replaces the writable JSON snapshot.
Implementation and validation: [phase 2 record](../../reports/upgrade-phase2-2026-10-18.md).

Amended 2026-10-01: both transports referenced below are now implemented, so the
state contract is no longer forward-looking.

## Decision

Download tasks have one backend-owned registry and a per-task SQLite journal. Valid public
states are `queued`, `downloading`, `paused`, `done`, `error`, and `cancelled`.
Only the backend starts, pauses, resumes, cancels, or persists a task; the
frontend renders snapshots and progress events.

The old `downloads.json` is imported once in a transaction and retained unchanged.
Invalid imports fail visibly without publishing a partial queue. Restart keeps
terminal states; interrupted queued/running tasks become paused and require user
intent to resume. History restoration does not allocate HTTP transports.

The application admits at most two active attempts and 64 waiting attempts. Each
attempt belongs to the application lifecycle. Pause, cancel, remove and overwrite
wait for its cleanup before a destination may be reused. Output identity is
checked alongside URL and task ID; removed records cannot be recreated by stale work.

Completion uses an internal `finalizing` checkpoint: sync the partial file, save
publication intent, rename, save completion, then emit `done`. Restart reconciles
the rename/commit window using the partial and final file and expected size. This
is a recovery protocol, not an atomic transaction across SQLite and the filesystem.

HLS checkpoints pair a synced byte offset with the next segment index. Uncommitted
tail bytes are discarded on resume. Legacy partials with an unverified segment
boundary are kept as `.part.legacy-<timestamp>` before restarting the download.

Duplicate URLs are reported with the stable `DOWNLOAD_DUPLICATE` error code,
not by parsing presentation text. A task receives the application lifecycle
context, so shutdown cancels active work and prevents new tasks from starting.

## Consequences

- Download recovery can be tested without a Wails window.
- Future transports (direct HTTP and HLS) share the same task state contract.
- UI changes cannot alter persisted task state directly.

