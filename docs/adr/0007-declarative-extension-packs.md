# ADR 0007: Declarative extension packs

## Status

Accepted, amended by [ADR 0008](0008-injected-frontend-packs.md): packs may now also ship
frontend JS/CSS (`kind: script`), so "a pack never contributes code" holds only for the
three declarative kinds below.

## Decision

User-supplied functionality ships as **declarative extension packs**: a directory
under the application data directory (`plugins/<id>/`) holding one `plugin.json`
manifest plus data files (JSON config, GLSL, images). Packs are interpreted by
engines that already exist in the application. A pack never contributes code.

Each pack declares exactly one `kind`, and only that kind's section is present:

- `source` — one or more collection adapters, each a document that validates
  against `docs/source-strategy-v2.schema.json`. It configures how a
  `SourceStrategy` builds request URLs and reads the response envelope; it does
  not add a driver.
- `shader` — an mpv-style hook shader file consumed by the WebGL pass runner.
  Passes run at source resolution; the engine owns the output scale.
- `theme` — preset theme records (primary colour, mode, optional tint
  overrides, optional bundled background image).

`app/plugin` owns the scan, the strict validation and the registry. Validation
rejects unknown manifest fields, ids that do not match their directory name,
duplicate ids, and any referenced path that is not an existing regular file
lexically inside the pack directory. Files above their per-kind size cap are
rejected rather than truncated. The registry reports three states per pack:
`ready`, `disabled` (user choice, persisted in the `settings` KV table),
`invalid` with a machine-readable reason.

A failing pack must degrade to itself. An invalid pack is excluded from every
engine; a pack that fails at runtime (e.g. a shader that does not compile)
quarantines only that pack's entry, and the built-in paths are untouched.

## Consequences

- No arbitrary code execution, so no sandbox, no permission model and no way for
  a pack to brick an installation. This is deliberate: Go's `plugin` package is
  unusable on Windows, and running pack code in the webview would give zero
  isolation.
  *(Superseded by ADR 0008: this product is single-user, so "zero isolation" was
  protecting the user from themselves, and the declarative layer turned every UI
  request into a release. The three kinds below still ship no code.)*
- Extensibility is bounded by what the built-in engines can express. New
  capability means a new engine plus a new `kind`, reviewed as normal code.
- UI mount points (pack-supplied pages, card actions) are out of scope until a
  declarative schema for them exists.
  *(Superseded by ADR 0008: pack-supplied pages and nav entries are now a script-pack
  capability rather than a declarative schema.)*
- Pack-supplied collection adapters still cannot create tables or invent source
  keys: ADR 0001 keeps ownership of `source_key` and `source_videos`, so a pack
  only fills the existing `sources.strategy_config` column.
- Generated bindings stay the only Go↔UI channel and stay behind
  `frontend/src/api/` per ADR 0005.
