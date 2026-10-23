> 历史设计记录；当前开发规范统一见 [AGENTS.md](../../AGENTS.md)。本文记录当时的选择与证据，不限制后续重构。

# ADR 0015: Playback lifetime and platform presentation

## Status

Accepted, 2026-10-18. Incremental migration; related mode design is in ADR 0014.
Current architecture guidance is consolidated in root AGENTS.md.

## Decision

The frontend chooses presentation. The backend supplies content, persisted
preferences and native capabilities, rather than returning CSS or component
trees. Application mode (`video`, `novel`, `comic`) and presentation platform
(`mobile`, `desktop`) are separate decisions.

`platform/presentation.ts` selects a presentation profile after native runtime
initialization, once per document. Resizing or entering fullscreen changes the
layout, not the platform. Mobile CSS is dynamically imported before application
mount only for mobile hosts. Video mode lazily selects its mobile or desktop
shell; the desktop shell owns its download hover listener. Mode-specific state
and repositories remain governed by ADR 0014.

Playback has three boundaries:

| Boundary | Owner | Responsibility |
| --- | --- | --- |
| Page workflow | `views/Player.vue` | Route identity, loading, switching source/episode, favorite actions and composition |
| Page features | `player/page/*` | Source discovery, episode history and desktop keyboard bindings, each with a small dependency interface |
| Media host | `components/VideoPlayer.vue` and existing playback modules | One native video element, HLS/session lifetime, playback commands and presentation composition |

`useSourceCatalog` owns discovery generations, its page-local cache, concurrency
and pending cancellable requests. Disposal invalidates results, requests
cancellation where supported, and prevents subsequent fallback searches.
Transport failure remains distinguishable from an empty successful search.
Requests still pending at disposal are counted honestly; a cancellation request
is not proof that native work has stopped.

`useEpisodeHistory` owns persistence and the visible percentage map. Precise
position saves retain the existing behavior and database throttling. Unchanged
integer percentages do not allocate a replacement map or dispatch another
progress UI notification. History write counters count attempts, not confirmed
database commits.

`PlayerInformationPanel` owns source/episode presentation and mobile comment-tab
state. Its stable read model groups details, catalog and episodes. It emits
workflow actions instead of navigating or importing the page controller.
Panel styles are scoped in `player-information.css`. Reading progress inside
the child no longer makes that dependency part of the page's normal render.
The fullscreen episode slot can still observe progress while it is open.

`usePlayerPresentationMode` owns fullscreen/PiP state, listeners and asynchronous
action invalidation. It does not create video/HLS instances. Layout, control
visibility and drawers must not add a layout-dependent key to the media element.
Source loading still follows the existing page loading/remount behavior; this
decision does not claim source switching is a continuous decoder session.

Mobile control components are asynchronous imports used by the mobile branch.
Video information and shortcut settings load on their first opening. Shortcut
capture and preference state belong to the desktop dialog. A shared dialog
frame owns motion, focus containment and dismissal, with separately scoped body
styles. Global mobile selectors remain a migration area; loading them only on
mobile does not mean every responsive component has already been separated.

## Diagnostics and evidence

`diagnostics/architecture.ts` adds a revision, presentation profile and player
session IDs. Its registry retains numeric descriptors only. Browser resource
providers are registered individually and removed on disposal, exposing duplicate
players rather than replacing a single global provider silently.

Recording markers include this context. `playback_page_closed` includes progress
event/UI-update counts, history-write attempts and source request accounting.
The existing explicitly enabled recording cadence remains five seconds; this
change adds no always-running diagnostic sampler.

Compare actual recordings from the same host, source, quality and similar use.
Use first-frame time, stalls, decoded/dropped frames, process memory trend,
buffer/cache state and post-close resource owners together. The perf store is
cumulative for the document; session IDs do not reset its counters. Use deltas
between markers when deriving per-player values.

`player_closed` is emitted during `beforeUnmount`: the DOM may still contain the
video until Vue completes removal. A subsequent sample, rather than the close
marker alone, establishes the attached DOM count. Missing browser/native metrics
are unavailable, not zero. Reduced file length is maintainability evidence, not
a memory or frame-rate measurement.

## Migration limits

This increment establishes feature and presentation seams without replacing the
working playback engine or changing buffer budgets. `VideoPlayer.vue` remains
the composition host for shared controls, subtitles and enhancement settings.
Further extraction should follow those feature lifetimes, rather than introduce
a single enormous composable or a generic server-driven UI framework.

During the initial 2026-10-18 increment, no automated tests, builds, packaging or scripted UI
scenarios were run. Source inspection and existing diagnostic
files provide structural evidence; runtime improvements need subsequent actual
use recorded under the new revision.

## Completion increment, 2026-10-19

The remaining settings views now own their scoped CSS: subtitles, playback
preferences, ad reporting, quality, enhancement confirmation, and desktop volume
and speed controls. Mobile mounts its settings in the drawer host and does not
mount a hidden desktop toolbar. `usePlayerChrome`, `usePlayerVolume`,
`usePlaybackTelemetry`, `useTimeJump` and `useAdReport` own their respective
state/timers. Favorites capture content identity across asynchronous operations.
Page progress listeners and fullscreen/PiP shortcuts use the owned media view,
rather than searching the document for another player's video element.

Built-in log and diagnostic plugin component entries lazily select a mobile
adapter or desktop view. Plugin manifests and `cczj.components` names remain
compatible. The mobile adapters own touch layout and export actions; common
panel logic owns filtering, data requests and activity-driven polling.

Android now reports app-process PSS/RSS, private resident bytes, file descriptors
and CPU with an explicit `app_process` scope. It does not include isolated
WebView/GPU processes. FileProvider exposes only diagnostic/log export folders
for user-initiated sharing. No filesystem access is added to plugin manifests.

Because these changes require native Go/Java code, the Android preview host was
built and updated once. The normal build's compiler checks completed; no test
suite or scripted UI/playback scenarios ran. Two passive native snapshots and
an actual diagnostic export confirmed resource availability. This is an idle
baseline, not a measured playback improvement; evidence and limits are recorded
in `reports/history/playback.md`.

## Follow-through against the original proposal, 2026-10-19

The earlier completion description covered feature extraction, but desktop toolbar
markup, mobile drawer state, native operations and broad mobile player selectors
were still coupled to the host. `PlayerDesktopControls` now owns desktop popup and
time input state; `CczjMobilePlayerChrome` owns mobile panel/category state. Both
consume live readonly playback values and commands while the media host persists.

`player/platform` owns host capability probes, window/element fullscreen, device/
element PiP, device volume and multicast preparation. Playback presentation owns
operation generations and resource ownership. Desktop toolbar CSS and mobile
player CSS follow their presentation modules; detached drawers own their theme
variables. Legacy hidden-toolbar overrides were removed. Shared feature widgets
and unrelated responsive page CSS remain intentionally shared or incrementally
migrated.

New diagnostic events and bounded per-session issue groups distinguish recovery
attempts from terminal HLS failure. Revision `player-seams-20261019-4` separates
this error-counting change from older recordings. Passive idle observations do
not validate the newly split controls or establish playback performance gains.
See `docs/player-presentation-completion-2026-10-19.md` for evidence and limits.
