# ADR 0002: Global video identity

## Status

Accepted. Amended 2026-10-01: normalization changes are now applied to
released databases by an ordered migration, not by resetting the store.

## Decision

`global_video.id` is the durable identity for one logical title across
sources. Source rows retain their source-local `vod_id`; favorites use one
global row, while watch history also includes source-local episode
coordinates.

Collection resolves type and title identity within the same catalog-write
transaction. It prefers exact and normalized name matches, then performs a
guarded high-similarity match with type/year checks before creating a new
identity.

## Consequences

- Cross-source lookup uses `global_id`.
- Playback and episode progress remain keyed by `source_key`, `vod_id`, and
  episode number.
- Normalization changes ship as a migration that rewrites `name_norm` and
  merges the identities that therefore collide (`migrateGlobalVideoNameNorm`,
  `migrateDedupeGlobalVideo`, `migrateRetitleGlobalVideo`,
  `migrateMergeSameDoubanIdentity`), repointing favorites, history, and catalog
  rows. Re-keying released data in place is the accepted cost of having one
  identity per title; a snapshot is taken before the migration runs.
