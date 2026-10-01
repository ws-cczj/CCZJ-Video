# ADR 0004: Download recovery and state transitions

## Status

Accepted.

Amended 2026-10-01: both transports referenced below are now implemented, so the
state contract is no longer forward-looking.

## Decision

Download tasks have one backend-owned registry and persisted snapshots. Valid
states are `queued`, `downloading`, `paused`, `done`, `error`, and `cancelled`.
Only the backend starts, pauses, resumes, cancels, or persists a task; the
frontend renders snapshots and progress events.

Duplicate URLs are reported with the stable `DOWNLOAD_DUPLICATE` error code,
not by parsing presentation text. A task receives the application lifecycle
context, so shutdown cancels active work and prevents new tasks from starting.

## Consequences

- Download recovery can be tested without a Wails window.
- Future transports (direct HTTP and HLS) share the same task state contract.
- UI changes cannot alter persisted task state directly.

