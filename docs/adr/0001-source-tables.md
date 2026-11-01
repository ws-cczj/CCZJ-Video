> 历史设计记录；当前开发规范统一见 [AGENTS.md](../../AGENTS.md)。本文记录当时的选择与证据，不限制后续重构。

# ADR 0001: Shared source catalog table

## Status

Accepted. Amended 2026-10-01: the second consequence no longer describes
startup — released databases are now migrated in place (see ADR 0006).

## Decision

The application is a modular monolith backed by one SQLite database. All
source-local catalog rows live in `source_videos`, keyed by
`(source_key, source_vod_id)`. Source types live in `source_types` under the
same stable source key.

`source_key` is an internal identifier, not display text, and must satisfy
`^[a-z0-9][a-z0-9_]{0,63}$` before persistence (`model.ValidateSourceKey`).
User-facing source names may change independently.

## Consequences

- Adding or editing a source never creates SQL tables dynamically.
- A fresh database is created directly at the current schema version. An older
  database is evolved in place by the ordered migrations in `app/db/migrations.go`
  (`PRAGMA user_version`), with a snapshot taken before the first migration of a
  run; the archive-and-reset path is gone.
- Source keys are immutable once data has been collected. Create a new source
  when an upstream identity changes.

## Category hierarchy and classification

Amended 2026-11-01: migration 26 adds source-local `parent_type_id` and
`manual_short_drama` to `source_types`. Type metadata refresh preserves user
mappings and manual classification; missing parent metadata preserves a known
relationship, while parent `0` explicitly denotes a root. Browsing and random
recommendations share the same automatic descendant classification; the legacy
manual flag is retained for payload compatibility and no longer expands this scope. Parent bans apply to
both incoming rows and existing catalog visibility; cycles terminate by identity.

Source export/import retains optional `parent_type_id` and
`manual_short_drama` fields. An omitted manual flag in an older payload preserves
existing configuration; explicit true/false restores the exported choice.
The metadata merge leaves manual classification and global mapping unchanged.
The category dialog has been removed. Automatic list discovery and verified
provider hierarchy supplementation retain catalog rows, favorites and progress.
