# ADR 0006: Catalog persistence and on-demand detail

## Status

Accepted.

## Decision

`source_videos` is the durable per-source catalog. It stores source/global
identities, source/global type identifiers, and card fields required by local
lists. Detail text, cast, playback/download routes, and episodes are never
written by collection or detail loading.

Details resolve `source_key + source_vod_id` from the catalog and call the
source strategy's detail URL. Results are retried a bounded number of times
and cached only in process memory. A final failure is structured data, never a
stale SQLite playback URL.

## Compatibility

New installs create this catalog schema directly. When a stored database is
from an unsupported generation, startup archives it and creates a fresh store;
it does not run a runtime schema migration.
