# ADR 0005: Boundary ownership for frontend and application facade

## Status

Accepted

## Decision

- Generated Wails bindings and runtime events are accessed only through `frontend/src/api/`.
- Browser persistence is owned by `frontend/src/platform/storage.ts`; domain code uses versioned JSON envelopes and compatibility reads for legacy values.
- Playback resume persistence is isolated in `frontend/src/player/usePlaybackProgress.ts`.
- `app.go` remains the composition root and lifecycle facade. Media/source/collection/window delegation lives in `app_facade.go`; download transport and persistence live in `app_download.go`.
- Existing shared styles remain the source of reusable layout/animation utilities. Page-specific CSS stays with its page unless a concrete reuse case exists.
- Race detection runs on Ubuntu CI, while Windows remains the native application verification environment.

## Consequences

The UI no longer depends on generated binding paths or scattered storage formats. The application entrypoint is small enough to review as a composition root, while transport changes can be tested independently from Wails wiring. Visual styles are not duplicated into artificial page-level “shared” files.
