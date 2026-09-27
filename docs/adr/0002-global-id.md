# ADR 0002: Global video identity

## Status

Accepted.

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
- Normalization changes require a new database reset generation and regression
  coverage; released databases are not transformed in place.
