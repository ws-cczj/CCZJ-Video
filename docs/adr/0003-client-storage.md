# ADR 0003: Client and application storage ownership

## Status

Accepted.

## Decision

SQLite is the source of truth for business records and preferences that must
survive application sessions. Browser `localStorage` is reserved for UI-only
preferences, while IndexedDB is reserved for disposable media cache data.

The backend records a destructive database-reset generation. On a new
generation, the client clears browser-owned catalog and media caches once;
the IndexedDB media cache schema is reset rather than converted.

## Consequences

- Stores and views access Wails bindings only through `frontend/src/api`.
- Download directory persistence is backend-owned and not duplicated in
  `localStorage`.
- Cache records may be discarded on an incompatible layout change. No browser
  cache conversion is required.
