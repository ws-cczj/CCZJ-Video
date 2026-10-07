# ADR 0011: Read a newer library, do not refuse it

## Status

Accepted.

## Context

An official **v2.1.0** build opened against a library that 2.3.x had already migrated to
schema v10 looked like a data-loss bug: 左下角「未选择来源」、首页整块「暂无数据」, while
`global_video` in that same library held 77 rows. `applog` carried three copies of
`missing destination name auto_disabled_at in *[]*model.Source`.

The mechanism is `SELECT *` plus sqlx's strict mapper: when the result set has a column the
struct does not, the query **errors** instead of mapping. The v10 column (`auto_disabled_at`,
`app/db/source.go:138`) is exactly such a column. Reproduced twice on 2026-10-06 — once against
the real library, once against a copy in a `%APPDATA%`-redirected sandbox — so this is a
published-artifact behaviour, not a quirk of one local build.

Two roads lead into that state:

1. Running an old exe against a library a newer version already migrated.
2. 退回上一版 (`RollbackUpdate`): the swap replaces the program but not the library, and neither
   `SwapBackupStat` nor `RollbackUpdate` looks at `PRAGMA user_version`. So rolling back from any
   version that added a column lands in state 1.

Nothing says so. `migrateToLatest` skips every migration whose `version <= current`, so when the
library is *ahead* of the build it silently does nothing and the app starts anyway — and keeps
writing into that library (observed: `[DoubanChart] 入库完成: 新增 0 + 更新 10` from the 2.1.0 run).

One constraint shapes the whole decision: **this cannot be retro-fixed into 2.1.0 or 2.2.0**.
Whatever ships here only protects builds from this one onward. `docs/pending-decisions.md` §10
keeps the matching advice for testers already sitting on a published old build.

## Decision

**Detect, adapt, explain — in three places, none of them a gate.**

1. `runMigrations` (`app/db/sqlite.go:123`) compares `PRAGMA user_version` against
   `LatestSchemaVersion()`. When the library is ahead: don't back it up, don't migrate it, don't
   rebuild indexes — record the two version numbers and return `nil`.
2. The handle swapped in for that case is `(*sqlx.DB).Unsafe()` (`applySchemaAhead`,
   `app/db/migrations.go`). Conditional, not global: the strict mapper stays in charge for every
   normal startup, so a mistyped `db` tag still fails loudly the way it does today. Only the
   one known situation — library ahead of build — gets unknown columns ignored.
   `Unsafe()` wraps the same `*sql.DB`, so the pool settings (8 conns, WAL pragmas in the DSN)
   and any `Tx`/`Stmt` taken from it are unchanged; `TestApplySchemaAheadKeepsQueriesAliveOnUnknownColumns`
   asserts both the strict failure and the shared `*sql.DB`.
3. `App.SchemaNotice()` (`app/service/schemanotice.go`) hands the two numbers plus the data
   directory to the UI, and `SchemaNewerPrompt.vue` says it out loud once per library version:
   nothing was deleted, this build just cannot read the fields a newer one added, so lists may
   look short. Footer offers 知道了 and 检查更新; the data-directory line offers 打开数据目录
   (through `OpenFolder`, which only accepts paths under the app's own directories).

Supporting decisions:

- **Dismissal stores the version number, not a boolean.** `cczj.schema-newer-dismissed` holds the
  `db_version` that was already explained; a library that later becomes newer again re-asks. A
  boolean would have silenced the notice forever after the first downgrade.
- **Startup modal order is now four deep**: 条款闸门 → 更新弹窗 → 旧数据找回 → 这条. The three
  older ones already had a `clear` gate; 旧数据找回 kept its open-state inside the component, so
  this notice had no way to know when to wait. `legacyPromptOpen` moved to
  `frontend/src/stores/legacyState.ts` for exactly that reason — the same z-index collision that
  once made the update modal vanish silently.
- **`rollbackMsg` now warns before the swap** (both locales): the build you roll back to cannot
  read fields this version has written. Before this sentence, 退回上一版 promised only the loss of
  the copy and the need to re-download; it did not mention that the library would outrun the
  program.
- **The old build is still allowed to write.** See Consequences — this is the part of the state
  that is *not* fixed, deliberately.

## Consequences

- A downgrade, or an old exe pointed at a new library, shows real data with a short list and one
  honest explanation instead of an empty app that reads as corruption.
- Not retroactive. Someone on published 2.1.0 still gets the sqlx failures; their remedy is the
  next release, which also carries the legacy-directory recovery from §10 of the pending log.
- The notice is only as good as the version comparison. A library whose `user_version` was
  stamped by something else (any process can write `PRAGMA user_version = 99`) reads as "newer
  than this build" and gets lenient mapping — which is the permissive direction, so the failure
  mode is a spurious notice, not a locked-out user.
- Writes from an old build still apply old semantics to new-maintained columns (source status,
  chart caches). Observed as harmless once: the 2.3.1 session immediately after the 2.1.0 run read
  the same library fine, three sources listed and enabled, `user_version` still 10. Beyond that it
  is inference, and the copy in 检查更新 is what the user has to act on.
- A future migration that adds a `NOT NULL` column **without** a default would break an old
  build's INSERTs, and `Unsafe()` cannot help there. Today's convention already avoids it
  (`auto_disabled_at` is `INTEGER NOT NULL DEFAULT 0`); worth keeping in mind when writing one.
- Verified statically, not on a screen. The lenient path is covered by `app/db/schema_ahead_test.go`
  (strict handle errors on the unknown column, lenient handle reads the row, both share one
  `*sql.DB`), and the sandbox run of official 2.1.0 reproduced the failure this decision addresses.
  The notice itself has never been rendered by a real build: it needs `wails3 build`, and a build
  with the single-instance lock needs the running app closed. So "the user now sees an explanation"
  is a code claim, not an observed one, until that run happens.

## Rejected alternatives

- **Refuse startup, leaving only 打开数据目录** (§10 option C). Removes the user's only way to look
  at their own data, and turns a mis-stamped `user_version` into a lockout. The state is "this
  program is too old", not "your data is broken".
- **Block writes, keep reads** (§10 option B). Needs a second handle threaded through every write
  path (settings, collection, history, caches); half-applied gating is worse than none, and the
  thing it would prevent is what the notice tells the user to do instead.
- **`db.Unsafe()` unconditionally** — swallows unknown columns everywhere, including the normal
  case where strict mapping is the guard that catches a wrong `db` tag.
- **Rewrite the 11 production `SELECT *` sites in `app/db` to explicit column lists.** The more
  principled fix for the add-a-column case and it would make this handle unnecessary, but it is a
  much larger surface to verify, and it still cannot cover a dropped or renamed column — which is
  the case no read-side change can survive, so the version comparison has to exist regardless.
- **Stamp the rollback copy with its schema version** (§10 item 3). The copy carries no marker
  today, so this means install-time bookkeeping just to warn earlier. The two choices actually on
  the table — warn before (`rollbackMsg`) and explain after (`SchemaNewerPrompt`) — are both done
  without new state on disk.
