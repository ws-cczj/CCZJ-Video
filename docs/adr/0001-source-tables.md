# ADR 0001: Source-keyed SQLite tables

## Status

Accepted.

## Decision

The application remains a modular monolith backed by one SQLite database. Each
source owns `v_<source_key>` and `e_<source_key>` tables. A source key is a
stable identifier, not display text, and must satisfy
`^[a-z0-9][a-z0-9_]{0,63}$` before it reaches any SQL identifier builder.

All source creation, update, import, collection, rebuild, and deletion paths
must use `model.ValidateSourceKey`. SQL identifier helpers reject invalid input
instead of normalising it, so an invalid key can never select another source's
tables.

## Consequences

- Adding a source does not require a global schema migration.
- Schema changes shared by source tables must be implemented as versioned,
  transactional migrations.
- Source-key changes are migrations, not in-place edits; user-facing names can
  change independently.

