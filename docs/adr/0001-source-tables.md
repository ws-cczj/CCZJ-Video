# ADR 0001: Shared source catalog table

## Status

Accepted.

## Decision

The application is a modular monolith backed by one SQLite database. All
source-local catalog rows live in `source_videos`, keyed by
`(source_key, source_vod_id)`. Source types live in `source_types` under the
same stable source key.

`source_key` is an internal identifier, not display text, and must satisfy
`^[a-z0-9][a-z0-9_]{0,63}$` before persistence. User-facing source names may
change independently.

## Consequences

- Adding or editing a source never creates SQL tables dynamically.
- A schema generation is created directly for new databases; unsupported old
  databases are archived and reset rather than migrated at runtime.
- Source keys are immutable once data has been collected. Create a new source
  when an upstream identity changes.
