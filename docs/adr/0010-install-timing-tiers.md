# ADR 0010: Two install tiers — now, or on exit

## Status

Accepted.

## Context

A hot-swap install costs the user a running app: the process has to let go of
`cczjVideo.exe` before anything can move over it, so installing means "the window closes and
something else starts". Before this change there was exactly one shape of that —
*install now, relaunch automatically* — and it collides with two real situations:

- The user is mid-task (a collection run, an episode half-watched, a download in flight) and
  the only reason they clicked 安装 was to get it off their plate.
- The swap takes a few seconds of retrying `move` while the process drains. Every extra
  second the process holds the image file is a retry burned from the script's 30-retry budget.

The other timing — "install when I close the app" — was not offered at all, so users who did
not want an interruption simply closed the modal and re-downloaded next time.

Also relevant: `RestartApp` relaunches the *same* binary before quitting, so the new process
re-acquires the exe lock immediately. A deferred swap scheduled during a restart would race
that new instance, exhaust its retries, and report `swap_failed` for something the user never
asked to be cancelled.

## Decision

Two tiers, chosen per install, with no default and no auto-scheduling:

1. **立即安装** — unchanged path: validate the artifact, write `_update_swap.bat`, start it,
   let this process exit, script starts the new exe.
2. **退出时安装** — record `{path, version}` in the `pending_install` setting and say nothing
   else happens yet. The arrangement is *consumed* by `ServiceShutdown`: the record is read and
   cleared while the database is still open, and `_update_swap.bat` is started as the last step
   before the log closes. That script variant does **not** start the exe afterwards — closing
   the app was the user's request.

Supporting decisions:

- **One script, one bool.** `buildSwapScript(startAfterSwap bool)` either emits the
  `start "" "%OLD%"` lines or omits them; the batch body is otherwise byte-identical. An
  earlier draft threaded a fifth `start`/`nostart` argument through `cmd` and guarded each
  launch with `if "%START%" NEQ "nostart"` — dropped, because then the fact "does this tier
  relaunch" would live in both the Go caller and the batch text.
  `TestBuildSwapScriptTiersDifferOnlyByStart` asserts the two variants differ only by the lines
  that start the app, and `TestOnExitSwapScriptRunsForReal` runs the no-launch variant through
  `cmd.exe` verbatim.
  **Amended by `docs/adr/0012-relaunch-handoff.md`:** the script now takes a *fifth* argument,
  the single-instance handoff token. It does not reinstate the rejected `start`/`nostart`
  design — the token is data, not a branch. Both tiers receive it and parse it (`set
  "HANDOFF=%~5"`); only the tier that emits `start` lines splices it in, so the "does this tier
  relaunch" fact still lives in exactly one place. `TestBuildSwapScriptTiersDifferOnlyByStart`
  and `assertEveryStartHandsOff` hold both halves.
- **Re-validate at shutdown, not only at the click.** `SwapOnExit` runs the same artifact gate
  (`isUpdateArtifact` + recorded digest) as `InstallUpdate`. The bytes may have changed, been
  deleted, or never landed between the click and the exit.
- **The record is consumed, not re-armed, and its failure is reported by ADR-adjacent item
  #195's channel** — the script still writes `ok` / `swap_failed` / `verify_failed` to
  `cczj_video_update_result`, and the next startup shows that receipt. A stale record (killed
  process, no shutdown at all) simply means the swap is attempted at the *next* exit; the
  artifact stays discoverable through `already_downloaded_path`, so the user can also just
  install it manually.
- **`Install` clears the arrangement** — installing now supersedes "on exit", and leaving the
  record behind would schedule a second swap against bytes that are already in place.
- **`RestartApp` does not consume the arrangement** (see Context). The deferred install waits
  for a real exit.
- Non-Windows platforms refuse the tier with `UNAVAILABLE`, matching the rollback entry; this
  app ships Windows binaries, and no other platform can swap a running exe.
- **The receipt copy is shared between the two tiers.** When a deferred swap fails, the next
  startup says exactly what it says for an immediate one: "the file was not replaced, you are
  still running {version}". That sentence is true for both tiers, so no per-tier provenance
  travels with the verdict — which means the receipt cannot tell the user "this is the one you
  arranged for exit". Adding it would need a second cross-process marker (the result file
  carries one verdict string, and `pending_install` is already cleared before the script runs),
  so it stays out until the wording actually misleads someone.
- Nothing about the arrangement is surfaced in the diagnostics notes; `applog` records it three
  times (scheduled / consumed / refused) and the update modal shows the current state. A note
  for "there is an arrangement" is not an anomaly, and diagnostics is an anomaly list.
- **The 下次启动 label is removed, not reused.** The downloaded-package footer used to carry a
  button labelled 下次启动 ("install at the next start") whose handler was `IgnoreVersion` — it
  dismissed that version and nothing was ever installed later. That promise was already false;
  putting a real deferred tier next to it would make the two read as one thing. The button now
  reads 忽略此版本 (the same key the pre-download footer uses; `update.later` is deleted from
  both locales rather than repurposed), and the hint line describes 退出时安装. Behaviour is
  unchanged — only the words.
- The button's state is per artifact, not global: `GetPendingInstall` returns the path the
  arrangement points at, and the modal only offers 取消退出时安装 when that path is the package
  it is currently showing. Two different downloaded packages must not share one label.

## Consequences

- The user keeps control of the interruption, which is the standing rule for this module
  (see the no-auto-download decision): nothing downloads, installs or relaunches by itself.
- A deferred install is invisible between click and exit except for one info toast and the
  button's state in the modal, which now reads 取消退出时安装. Deliberately no persistent
  banner: the arrangement is a promise about a future exit, and a nag on every session would
  be noise.
- If shutdown drains longer than the script's retry budget (~60 s), the deferred swap falls into
  `:RENAME_REPLACE` rather than landing as `swap_failed` — a running exe can be renamed even
  though it cannot be deleted, so the swap usually goes through. Amended by
  `docs/adr/0012-relaunch-handoff.md`, which is about the `start` that follows such a success.
  Chosen over raising the cap: an unbounded retry loop was one of the original updater bugs.
- `pending_install` lives in the settings table, so moving the data directory carries the
  arrangement along with it. Harmless: the path is absolute, and a wrong path fails the gate
  at exit rather than swapping something else in.

## Rejected alternatives

- **Install-on-exit as an app-wide default/setting** — a policy switch that would also
  auto-swap after a session where the user never asked for the update. Can still be added later
  on top of this mechanism (see `docs/pending-decisions.md` §9).
- **A third tier ("install at next start")** — requires the script to run before the process
  has the exe locked, i.e. a launcher or scheduled task. Bigger blast radius than the problem.
- **Front-end-driven shutdown install** (UI calls the swap binding when it sees the window
  close) — races the shutdown path, and dies if the UI never mounted. The Go side owns the
  record and the hook.
- **Keeping the record until a successful swap is observed** — needs version-comparison logic
  on startup plus a cancel entry that is reachable when the modal is not open; the consumed-on-
  exit rule gets the same outcome with less state.
