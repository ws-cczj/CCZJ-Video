> 历史设计记录；当前开发规范统一见 [AGENTS.md](../../AGENTS.md)。本文记录当时的选择与证据，不限制后续重构。

# ADR 0014: Independent application modes

## Status

Accepted, 2026-10-18.

## Decision

The application has one persisted `application_mode`: `video`, `novel` or `comic`.
`modes/definitions.ts` is the Strategy registry: each mode defines its home,
startup caption and lazy shell factory. The composition root selects one shell;
the router guards against entering routes belonging to the other mode.

Mode changes save through the existing settings API before navigating to a new
document URL. The URL includes the target mode and home hash, ensuring a normal
switch replaces the document, reruns bootstrap and displays the startup screen.
Changing shells in place would retain unrelated KeepAlive, media and plugin
state and make the mode boundary harder to reason about. This reload does not
restart the Go process and does not invoke Vue unmount hooks; settings is the
switch entry point, after leaving playback/reading routes normally.

The video shell owns its existing navigation, KeepAlive pages, collection and
download store initialization. The novel shell owns separate navigation, routes,
styles and reading state; it never initializes those video stores. Global native
window, licensing, update and notification infrastructure remains shared.

`NovelRepository` is the Adapter seam for book queries and chapter retrieval.
The initial implementation supplies original demo content. Views depend on the
repository contract rather than embedding demo arrays or using video APIs.
Book shelves, reading progress and reader preferences use separate versioned
storage keys through the platform storage adapter. Video favorites, viewing
history and theme settings retain their existing contracts.

Comic mode extends the same registry with its own shell, repository, follows,
episode/page progress and preferences. The two readers share only the modal
interaction and session brightness modules. Novel pagination and comic strip/
page navigation remain independent implementations because their layouts and
progress semantics differ.

## Consequences

Each mode can evolve its layout and content services without conditionalizing
every video page. The full reload briefly returns to startup, intentionally
trading seamless in-place switching for a clean document state. Invalid/missing
mode settings fall back to video. A failed save leaves the current UI in place
and permits retry.

Novel mode currently demonstrates discovery, search/filter, shelf, details,
chapters and reading. It does not introduce an account, payment, ranking service,
external book source or a claim to commercial catalogue completeness. A future
network repository can replace the demo adapter, with asynchronous loading and
error handling added at that seam.


## 2026-10-19: mode-owned work and diagnostics

The frontend still performs a full document navigation when switching modes. Only
one shell is mounted. Inactive shells are not retained in a three-mode KeepAlive
cache. The Go host, SQLite, settings, update infrastructure and bounded caches
remain shared; creating a service does not imply starting its jobs.

Each active mode now owns a cancellable task group. Only video activation starts
collection and Douban schedulers. Request contexts and cache maintenance attach
to that group. A switch pauses video downloads durably, cancels the old owner and
waits for its registered work before saving/activating the new mode. Downloads
need explicit manual resume after returning. The frontend displays this warning
and asks for confirmation before the switch. Legacy Douban work finishes its
current record; if it has not drained within the deadline, switching fails rather
than reporting a fully isolated new mode.

Diagnostics use the selected mode before collecting domain statistics. Reading
reports omit video collection, Douban and download snapshots; shared SQL tables
are counted with a kind filter. Reading network traffic is attributed to novel
or comic at request creation, including image requests. Recording files carry
mode identity, close on a mode switch and are not reused by the next mode.
Reading diagnostics load a separate component without initializing video stores
or segment caches. Untagged host logs are omitted from mode exports. Process
memory, Go profiles, the network admission budget and SQLite/log file sizes are
explicitly shared-host observations, not per-mode memory measurements.

A warm MuMu observation found about 188–233 ms to main DOM appearance and
1.20–1.30 s to splash removal, with 6–24 ms for native mode switching in that
sample. This is not a content-ready benchmark or a comparison against a retained
shell implementation. Reducing repeat splash work should be evaluated before
retaining three full DOM trees. Lightweight view-state retention with heavy
resources released remains an option; a complete freeze/wake cache is not yet
implemented. Evidence: `reports/mode-isolation-2026-10-19/observation.json`.

## 2026-10-19: bounded state checkpoints and fixed splash preparation

The selected policy now keeps a small checkpoint for each visited mode, while
still replacing the document to release all page instances, media, listeners
and plugin runtime state. This supersedes the earlier always-home navigation
policy. It is state hibernation, not retention of three live DOM trees or a
browser execution freeze. The Go mode-owned drain and manual download resume
policy remain in place.

Only allowlisted browsing routes, bounded scroll positions and opted-in scalar
form/filter fields cross the document boundary. Checkpoints use session storage,
expire after 12 hours, and are limited to 16 KiB per mode / 64 KiB for the whole
serialized envelope. They contain no content arrays, decoder state or executable
values. Durable progress and preferences remain in SQLite. Missing or invalid
checkpoints fall back to the mode home; media and reading routes are never
opened automatically. A short-lived single-use handoff identifies a deliberate
switch for diagnostics, but never changes the splash duration.

Per the user's clarification, the splash keeps its existing fixed presentation
interval. Data completion does not decide when it disappears, and resume does
not skip it. Only the selected shell is prefetched while global settings load.
The active page's settings, source metadata, first query and independent
recommendations run concurrently during the splash. Slow work continues in the
page after the fixed interval, with its existing loading/error handling.

Startup diagnostics include selected-shell loading, first-screen preparation,
whether that preparation finished before splash fade-out, and the current mode's
checkpoint size/activation strategy. These observations do not claim full image
decoding, complete pagination or per-mode process memory. Desktop Back handling
also avoids using a previous mode's browser history entry as its destination.

Verification and remaining device acceptance: `reports/mode-resume-2026-10-19.md`.
