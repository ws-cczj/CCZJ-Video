# ADR 0003: Client and application storage ownership

## Status

Accepted.

## Decision

SQLite is the source of truth for business records and preferences that must
survive application sessions, including download directory and download task
state. Browser `localStorage` is reserved for UI-only preferences; IndexedDB is
reserved for large media cache data.

Every new browser-storage record must include a version and a one-way migration
from previous keys. A browser value may seed an unset backend preference once,
but it must not continuously overwrite SQLite.

## Consequences

- Stores and views access Wails bindings only through `frontend/src/api`.
- The player settings adapter owns `vp_settings` migration and versioning.
- Download directory persistence is backend-owned and no longer duplicated in
  `localStorage`.

