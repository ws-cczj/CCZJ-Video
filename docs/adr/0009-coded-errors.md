# ADR 0009: One coded error type across the Go boundary

## Status

Accepted.

## Context

Wails turns a Go `error` into a string before the frontend sees it, so whatever the
backend returns *is* the UI's only input. Three schemes coexisted:

- `fmt.Errorf` prose (`"source does not exist: %s"`, `"plugin: 无法写入 %q: %w"`), which
  the UI can only print;
- extension packs' own `reason_code` values (`manifest_too_large`, `id_invalid`), which
  the settings page groups and translates by;
- `strings.Contains(err.Error(), ...)` inside the proxy layer to guess whether a failure
  was a network error worth retrying.

The third scheme fails silently: rename one message and both the retry logic and the UI
grouping stop matching, with no compile error. It also lost the underlying cause — a
message built with `%v` instead of `%w` cannot be unwrapped.

## Decision

- `app/apperror` is the only error *type* in the app. `app/plugin`'s pack error is now an
  alias (`type packError = apperror.Error`), so pack validation codes and general codes
  travel in the same shape.
- Codes are stable identifiers, not prose: `VALIDATION`, `CONFLICT`, `NOT_FOUND`,
  `CANCELLED`, `TIMEOUT`, `UNAVAILABLE`, `STORAGE`, `CORRUPT`, `UNSUPPORTED`, `INTERNAL`,
  plus `DOWNLOAD_DUPLICATE` (the existing overwrite dialog contract) and the lowercase
  `reason_*` pack codes documented in [docs/plugins.md](../plugins.md) §2.
- **Only boundary errors carry a code.** "Boundary" means a value that leaves the package:
  a bound method's return, an error stored for the UI, or an error another package
  classifies. Errors that stay inside a package keep using `fmt.Errorf`, and must chain
  the cause with `%w`.
- Classification uses `apperror.CodeOf` / `errors.As` / `errors.Is`. `strings.Contains` on
  an error string is banned.
- Rendering is part of the contract: `CODE: message: cause`. The frontend's
  `normalizeApiError` splits on it, and unmatched prose falls back to `INTERNAL` — so a
  boundary error without a code still displays, it just cannot be acted on.
- A code never masks a more specific one: `apperror.Wrap` is not applied around an error
  that already carries a code (`errors.As` reads the outermost one).

## Consequences

The UI can branch on `NOT_FOUND` instead of matching Chinese text — `videoStore.loadDetail`
now stays quiet when a catalog row is simply missing. Retry policy lives in code, so
rewording a message is a copy change rather than a behaviour change.

Deliberate non-goals: `app/db` keeps its ~115 internal `fmt.Errorf` chains (they are
causes, and prefixing each with `STORAGE:` would double the text the UI shows); download
task failures stay plain strings because they are persisted as data in `tasks.error`;
`collect` keeps its `PathError`/sentinel domain types for the same reason they exist —
structured paths, not codes.
