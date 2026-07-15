# ADR 0002: Global video identity

## Status

Accepted.

## Decision

`global_video.id` is the durable identity for one logical title across sources.
Source rows retain their source-local `vod_id`; favorites and watch history use
`global_id` plus source-local coordinates where necessary.

`global_id` must not be regenerated during source import, collection, or source
table rebuilding when an existing normalized title/type match is available.
The association is therefore stable across changes to a source API's local ID.

## Consequences

- Cross-source lookup is performed through `global_id`.
- Source-local playback and episode progress remain keyed by `source_key`,
  `vod_id`, and episode number.
- Changes to normalization rules require a versioned data migration and
  regression coverage for favorites and history.

