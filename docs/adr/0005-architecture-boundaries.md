# ADR 0005: Boundary ownership for frontend and application facade

## Status

Accepted. Amended 2026-10-01 to match the package layout and the verification
reality (there is no CI pipeline in this repository).

## Decision

- Generated Wails bindings and runtime events are accessed only through `frontend/src/api/`.
- Browser persistence is owned by `frontend/src/platform/storage.ts`; domain code uses versioned JSON envelopes and compatibility reads for legacy values.
- Playback resume persistence is isolated in `frontend/src/player/usePlaybackProgress.ts`.
- `app.go` remains the composition root and lifecycle facade, kept small enough
  to review (under 600 lines, enforced by `scripts/verify.ps1`). Delegation lives
  in the `app/service` package rather than loose root files: `app.go` wires
  services, `facade*.go` expose bound methods one file per domain, and
  `download.go` + `download_http.go` + `download_hls.go` own the download
  transport (task lifecycle / direct multi-connection / m3u8), and
  `direct_resume.go` / `diagnostics.go` / `logs.go` / `relaunch.go` own theirs.
- Existing shared styles remain the source of reusable layout/animation utilities
  (`styles/cczj-utilities.css`, `styles/animations.css`). A page's own CSS stays
  with that page's component, but once an SFC passes roughly a thousand lines the
  style block moves out to `styles/views/*.css` or `styles/components/*.css` and
  the SFC re-attaches it with `<style scoped src="…">` — scoped attribute hashing
  still applies, so the boundary is file size only, not cascade reach.
- Verification runs locally on Windows through `scripts/verify.ps1`
  (`gofmt`, `go vet`, `go test`, boundary greps, `npm run build`). There is no CI
  runner, and `go test -race` does not start on this machine, so concurrency is
  reviewed by reading the code instead of by a race detector.

## Consequences

The UI no longer depends on generated binding paths or scattered storage formats. The application entrypoint is small enough to review as a composition root, while transport changes can be tested independently from Wails wiring. Visual styles are not duplicated into artificial page-level “shared” files.
