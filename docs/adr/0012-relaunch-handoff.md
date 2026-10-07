# ADR 0012: A script that starts the app must hand off the single-instance lock

## Status

Accepted.

## Context

"点了安装，程序关了，再也没回来" is the worst failure this module can produce, because the
receipt says everything worked. It was reachable after ADR 0010 shipped, through a branch that
only runs when the app is slow to let go of the exe.

The swap ladder in `_update_swap.bat` retries `move /y "%NEW%" "%OLD%"` 30 times at ~2.03 s
apart, so ~60 s. When the retries run out it enters `:RENAME_REPLACE`. That branch exists
because a `move` onto a locked target can never succeed by waiting forever, and it works:
**renaming a loaded image is permitted on Windows; deleting it is not.** So `ren` moves the
still-running old exe out of the way, `move` swaps the new one in, the script verifies, writes
`ok` to the result file and executes `start "" "%OLD%"` — which is now the *new* binary.

That premise was measured on this machine rather than recalled: a copy of `PING.EXE` was run
as a live process, then `ren` on it succeeded (rc 0), `move` of a different file onto the
freed name succeeded ("移动了 1 个文件"), and `del /f /q` of the *renamed* live image failed
with 拒绝-access. Rename allowed, delete not, and the name becomes free the moment the rename
lands — which is exactly the sequence `:RENAME_REPLACE` performs.

Then the app never appears. The old process is, by construction, still alive at that moment
(that is what the locked file was telling us), so it still holds the single-instance mutex
(`wails-app-com.cczj.video-sim`; `SingleInstance{UniqueID: "com.cczj.video"}`). Wails beta.24
answers a second launch by raising the existing window and calling `os.Exit` on the newcomer —
silently, with nothing written to the log and nothing in the receipt. The old process exits
moments later having lost its image file. Net user-visible result: window closes, no window
comes back, next start reports `ok`.

The coincidence that makes this reachable in normal use: `shutdownHardStop` is 60 s
(`app/service/app.go:47`) and the retry budget is ~60 s. The rename branch therefore fires
exactly inside the window where a graceful shutdown is still draining — not only after a
crash. Nothing about the `:BACK_FAILED`/`start` lines in the rollback script was safer: the
same argument applies there, and in `:BACK_FAILED` the process is still alive *by definition*
(the rollback could not take the file).

Two earlier roads into this same symptom already existed and are why the mechanism was not new:
`RestartApp` had to learn to wait for the old PID before acquiring the mutex, and any "quit then
launch another" flow needs the same courtesy.

## Decision

**Every line in every script that starts the app carries the handoff token, and a test enforces
that at the line level.**

1. The cross-process contract now lives in one package, `app/handoff`: `Arg(pid)` builds the
   startup argument, `Await(os.Args[1:])` consumes it, `TargetPID` parses it. It replaced
   `app/service/relaunch.go`, which only ever served `RestartApp` — a script that launches the
   app is the second producer of the same argument, so the format cannot be a detail of the
   service package.
2. `main()` calls `handoff.Await` as its **first** statement, before `buildApp()`. The mutex is
   taken inside `application.New`, so a later call site would run after the second instance has
   already been exited.
3. `Await` polls the old PID (from a process snapshot, 200 ms apart — see item 9) for at most 30 s,
   then starts anyway with one `applog.Warn`. Bounded in both directions: the common case
   (process already gone) returns in well under 2 s so an install is never slowed, and a wedged
   old process cannot block the new one forever — the mutex decides from there.
4. `InstallUpdate` passes `handoff.Arg(os.Getpid())` as the script's 5th argument
   (`%5` → `HANDOFF`); `RollbackUpdate` passes it as the 4th. The token is *data*, not a branch:
   both install tiers receive it, only the tier that emits `start` lines splices it in.
5. `assertEveryStartHandsOff` (in `app/updater/install_result_test.go`) walks the generated text
   of both scripts and fails if any `start "" ` line does not end with `%HANDOFF%`, or if the
   script contains no `start` at all where one is expected. A missing token is not a compile
   error and not a failing test anywhere else — it is a swallowed process.
6. The real-`cmd.exe` tests (`swap_script_windows_test.go`, `rollback_script_windows_test.go`)
   rewrite each `start` line into an `echo would-start %HANDOFF%`, so the assertions exercise
   cmd's own `%~5` expansion rather than the Go string. That is what caught item 7 below.
7. The token uses a **colon**: `--cczj-relaunch:1234`. An `=` is a batch-parameter delimiter
   (like space, tab, `,`, `;`), so `--cczj-relaunch=1234` reaches the script as two arguments —
   `%~5` was just `--cczj-relaunch` and the PID landed in `%6`. `TestTargetPID` rejects the old
   `=` forms outright, and `TestArgSurvivesCmdBatchParameterSplitting` fails on any of those
   characters in `Arg`'s output.
8. `Await` carries a **second signal**, because the token can only be honoured by a process that
   knows about it. A machine running published 2.3.0/2.3.1 generates its own script from code that
   predates this ADR: no `%5`, no token, and yet that script does `echo ok>"%RESULT%"` one line
   before `start "" "%OLD%"`. So when there is no token, `Await` checks whether
   `%TEMP%\cczj_video_update_result` was written within the last 15 s and contains exactly `ok`;
   if so it assumes it was spawned by a swap script and waits for other processes **with the same
   image name** to disappear. The gate is threefold on purpose (`ok` only — the failure verdicts
   do not `start`; fresh only — a leftover receipt from a previous install must not stall a normal
   double-click; and only when a same-name process actually exists —
   `TestAwaitReturnsImmediatelyWithoutArgOrReceipt`). The receipt's file name now lives in this
   package (`SwapReceiptPath`, referenced by `updater.installResultPath`): the script writes it and
   the newcomer reads it, and two spellings would silently disable the signal.
   Published 2.1.0/2.2.0 write no receipt at all (`git show c0fff71`, `5fadd03`, `e8f0ff8` emit a
   bare `start "" "%s"`), so the first hop out of those two versions is still uncovered.
9. Both questions the wait asks ("is this PID gone", "is another copy of this image running") are
   answered from a **process snapshot** (`CreateToolhelp32Snapshot` in `procs_windows.go`), not by
   spawning `tasklist`. The polling version was a user-visible defect: this app is built
   `-H windowsgui`, so every console-subsystem child it starts gets a fresh console window, and a
   200 ms poll for up to 30 s paints roughly 150 flashing black windows on the user's desktop —
   reported from the real machine on 2026-10-06. The snapshot also drops the dependency on
   tasklist's column format and localised output, and the "tasklist unavailable ⇒ don't wait"
   degradation with it (the tests' `exec.LookPath("tasklist")` skip guards described a dependency
   that no longer exists, so they are now a plain `requireWindows`).
10. **The console window the swap script owns stays visible.** Decided by the reporter on
    2026-10-06: "黑窗口不藏起来，但是不能一直出现然后关闭持续闪烁". That splits two different windows —
    the single `_update_swap.bat` console, present for the ~60 s of the swap and scrolling the
    script's own English lines, is the only progress a user can see after the app window has closed
    (the download bar dies with the process), so no `CREATE_NO_WINDOW` is added and `SysProcAttr`
    is untouched; the flashing ones were the polled `tasklist` windows, and item 9 removes them.
    Counted across the non-test tree, one install shows exactly one window: the two `cmd /c` script
    launches, `explorer`/`open`/`xdg-open` (not console subsystem), `launchAndExit`'s `.msi` path,
    and `RestartApp`'s own GUI exe, while the script's `ping -n 3 ... >NUL` retry interval inherits
    the console cmd already has rather than allocating another.
    `scripts/verify.ps1` greps `app/handoff`'s non-test files for `exec.` / `tasklist` and fails:
    a reintroduced console child produces no compile error and no failing test anywhere else —
    it produces desktop flashing.

## Consequences

- The `:RENAME_REPLACE` branch — the only one that can succeed while the old process lives — is
  now safe, and so is every future `start` line: the test makes the omission loud at `go test`
  time instead of on a user's desktop. *Every future* is the exact scope: the hop **into** this
  fix from an installed 2.3.0/2.3.1 is emitted by the old binary's own script, which cannot carry
  a token it has never heard of — that hop is covered by item 8 instead, and measured below.
- A relaunch after a slow shutdown can wait up to 30 s before the window appears. That is a
  visible pause with no progress bar, judged cheaper than the failure mode it replaces. `Await`
  deliberately does nothing when there is no token, so normal double-click launches pay nothing.
- The 60 s / ~60 s coincidence is now harmless but still real. Making the retry budget smaller
  than `shutdownHardStop` was tempting and is not part of this change: the window it would
  shrink is the one the handoff already covers, and shortening the budget would push more
  installs into `:RENAME_REPLACE` rather than away from it.
- `git rm --cached` was used when the two files moved from `app/service` to `app/handoff`, so the
  index carries two `D` entries with no commit behind them. Nothing is committed; the deletions
  are staged bookkeeping for a move that is complete on disk.

- Every link is now measured on this machine (2026-10-06, sandbox on `D:` with `APPDATA`
  redirected, two real builds of this HEAD differing only in `-X updater.Version`; the script text
  was the unedited output of `buildSwapScript`, launched exactly the way `installBySwapWindows`
  launches it):
  - *A live image can be renamed, and that frees the name for the swap* — measured (above).
  - *`Await` really waits for a live PID, and really does not wait for a dead one* — measured by
    `TestAwaitWaitsForALiveProcess` (a real ~2 s subprocess; `Await` returned in 2.41 s) and
    `TestAwaitReturnsImmediatelyWhenTargetIsGone` (0.15 s for a PID that never existed).
  - *Wails exits the second process rather than opening a window* — **observed**, not just read from
    beta.24: the control run (same script, `%HANDOFF%` stripped from the `start` lines) swapped the
    binary successfully, the receipt said `ok`, the new process appeared at 15:14:20.964 and was
    gone 311 ms later, while the still-running old process logged
    `app.go:40 [SingleInstance] 检测到重复启动，已激活现有窗口`. Killing the old process left a
    directory holding the new exe and no window anywhere — the reported failure, reproduced.
  - *The path this ADR installs* — 15:12 with the token present: 29 failed `move` attempts over 61 s
    (≈2.03 s each, matching the budget), `:RENAME_REPLACE` succeeded while the old process lived,
    receipt `ok`, the new process came up and stayed silent for 5.2 s, logged `application ready`
    236 ms after the old PID disappeared, and was still alive 30 s later. On disk: exe = new bytes,
    `.old` = old bytes, staging artifact consumed, script self-deleted. The library was the same
    file in place — `user_version` 10 and all ten table row counts identical to the pre-run
    baseline, no `[DataDir]` migration and no schema notice.
  - *The hop out of a published 2.3.x build* (item 8's road) — measured 16:46. App dir holding the
    8.8.8 build, staging the 9.9.9 build, and the **2.3.1-shape script** run exactly as that build
    runs it: `cmd /c _update_swap.bat <old> <new> <receipt> cczjVideo.exe` with **no 5th argument**.
    `move` failed for 60.7 s, `:RENAME_REPLACE` swapped the binary in while the old process lived,
    receipt `ok`; the newcomer appeared at 16:47:11 and, having no token, logged
    `[Relaunch] 等待同名实例 cczjVideo.exe退出后再抢单实例锁` — it recognised its own provenance from
    the fresh receipt alone. Terminating the old process 2 s later produced the second
    `application ready` at 16:47:14.029 (~2.9 s) and **zero** `SingleInstance` lines. On disk: exe =
    new bytes, `.old` = old bytes, staging consumed, script self-deleted.
    The same script with the old process deliberately left alive is the other half of that
    measurement: the newcomer waited 30.28 s (16:43:10.365 → 16:43:40.648, matching the cap),
    started anyway and was eaten by the mutex. So item 3's "timeout, then let the mutex decide"
    branch is now observed rather than only unit-tested — and it is what failure looks like, not
    a goal.
  - *The wait asks the OS directly* — after item 9 the same two assertions run against a process
    snapshot instead of `tasklist`: `TestAwaitWaitsForALiveProcess` still takes 2.21 s for a real
    ~2 s child (the wait itself did not get shorter or longer), and
    `TestAwaitReturnsImmediatelyWhenTargetIsGone` dropped from 0.15 s to 0.02 s because no child
    process is being spawned to answer the question.
  - *The flashing itself, counted on a desktop* — measured 2026-10-06 with a control group, since
    "no console window" is only judgeable by looking at one. The control is the **old** polling build
    (`exec.Command("tasklist", …)`, same `-H windowsgui` link flags, same 200 ms loop): across a 3 s
    wait it started 4 console-subsystem children and Windows allocated **3 new conhost windows** for
    them. The shipped build in the identical harness: 1 child in total (`msedgewebview2.exe`, the
    WebView2 runtime), **0** of them console subsystem, 0 during the wait, **0** conhost windows
    attributed to it. Attributing by **parent PID** is not pedantry — this machine's editor spawns
    `powershell.exe` + `icacls.exe` + conhost about every 0.6 s (25 conhost in 16 s of idle), so a
    naive "every new conhost inside the window" read 44 during a hop that started nothing at all;
    that number is machine noise, not the app's, and the first version of this measurement reported
    it before the control group caught it.
  - *The acceptance sentence, end to end* — 2026-10-06, two further real hops on the sandbox
    (`APPDATA` redirected to `D:`): the old instance logged `应用正常退出`, the newcomer logged
    `[Relaunch] 等待旧进程 …退出后再抢单实例锁` and then
    `application ready … data_dir="D:\cczj-vv\roaming\CCZJ Video"`, with its window present at T+3 s.
    That is 老版本关掉 → 新版本启用 → 数据库认得, observed rather than inferred; the library was the
    same file in place, and the seeded 采集源 plus the freshly fetched 豆瓣热榜 were both readable in
    the new instance.
- What those runs do **not** cover, so don't read them as "whole update chain verified": nobody pressed
  the in-app 「立即安装」 button (that path needs a genuinely newer published release, so
  check → download → checksum → button stays covered only by unit tests and reading), the two builds
  share a schema so no migration-bearing hop happened, and 2.1.0/2.2.0 → this fix remains uncovered by
  design of the old builds, not by omission here. Nothing in this ADR speaks to what the window
  *looks like* after it comes back — the on-screen items (首页轮播交棒、动画总开关、
  「数据比程序新」与旧数据提示两张弹窗) are tracked separately and still need a desktop of their own.

## Rejected alternatives

- **Quote the argument from Go** (`escapeArg`, or embedding the token in `"..."`). Rejected
  without a guess: cmd's batch tokenizer does not honour the `\"` form Go emits, and the real
  `cmd.exe` test showed the split anyway. The character set is cmd's, so the fix has to be in
  the format.
- **Reassemble `%5 %6` inside the script** so an `=`-split token survives. Fixes the wrong layer —
  it hard-codes a two-token assumption into a ladder whose argument list is already five deep,
  and any future `=` in any other argument silently reintroduces the bug.
- **Patch only the success branch** (`:START_NEW`). The `restore` block runs on `swap_failed` and
  `verify_failed`, and there the old exe may equally still be running. Both branches were the bug.
- **Poll `tasklist` inside the batch script** to wait for the old PID. `:RENAME_REPLACE` exists
  precisely because a file lock was a better signal than process inspection (the previous
  updater implementation matched by image name and hung on an unrelated same-named exe, per
  `buildSwapScript` rule 2), and a PID comparison would additionally depend on tasklist's column
  format and localized output. Item 8 does not reverse this: the name match there happens in Go,
  behind a 15 s receipt gate and a 30 s cap, and it asks "who else holds this one app's mutex",
  which is the invariant single-instance already declares — not "which process might be locking
  this file", which is the question rule 2 refuses to answer. Item 9 removed the last `tasklist`
  call anyway.
- **Remove the rename branch** so a locked exe just fails. That converts a working install into a
  guaranteed `swap_failed` for exactly the users whose shutdown is slow, and the receipt would at
  least be honest — but ADR 0010's rule 3 exists because "failed and stayed closed" is the
  complaint this whole ladder was built to end.
- **Drop single-instance instead of handing off.** Multiple windows on one SQLite library and one
  settings table is a larger and worse class of "更新后找不到数据".
