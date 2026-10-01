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
