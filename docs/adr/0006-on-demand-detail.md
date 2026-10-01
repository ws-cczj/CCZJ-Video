# ADR 0006: Catalog persistence and on-demand detail

## Status

Accepted. Amended 2026-10-01: the Compatibility clause was reversed — released
databases are migrated in place, and the memory cache became stale-while-
revalidate.

## Decision

`source_videos` is the durable per-source catalog. It stores source/global
identities, source/global type identifiers, and card fields required by local
lists. Detail text, cast, playback/download routes, and episodes are never
written by collection or detail loading.

Details resolve `source_key + source_vod_id` from the catalog and call the
source strategy's detail URL. Results are retried a bounded number of times and
cached only in process memory (`app/detail`), bounded by entry count and byte
size, with a per-key single flight. Once an entry ages out it is still served
immediately while a background refetch runs, and that refetch only becomes
visible on the next read — the screen never waits on the network. The freshness
window is the「数据新鲜度」setting and takes effect on the next read. A final
failure is structured data, never a stale SQLite playback URL.

## Compatibility

New installs create this catalog schema directly. An older stored database is
upgraded in place by the ordered, transactional migrations in
`app/db/migrations.go` (`PRAGMA user_version`, snapshot taken first); the
former archive-and-reset path was removed because it silently destroyed
favorites, history, and collected rows on every schema bump.
