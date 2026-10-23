> 历史设计记录；当前开发规范统一见 [AGENTS.md](../../AGENTS.md)。本文记录当时的选择与证据，不限制后续重构。

# ADR 0013: Consent before executing extension scripts

## Status

Accepted, 2026-10-10. Revised, 2026-10-13, to confirm once when explicitly enabling an external plugin.

## Decision

Frontend script packs remain trusted code executing in the app's realm. The
existing Vue, routing and interception interfaces require access to that realm;
cooperative permission wrappers cannot provide a sandbox.

Before importing any pack code, hash its manifest, declared styles and entry
script with SHA-256 (UTF-8, in that order, each prefixed with its UTF-8 byte
length and a colon). File boundaries remain unambiguous even with NUL bytes.
The digest remains the reload fingerprint and verifies shipped builtin bytes.
New external script packs are disabled silently on startup or rescan. Ask for
trust only when the user explicitly enables a pack; persist the first digest
and decision before enabling or importing. Approval applies to the package ID
through normal updates and restarts, as requested by the user. Denial persists
until revoked. Uninstall removes the package's trust record; revoke and disable
remain available in Extensions. An unpersistable decision cannot import the module.

Builtin trust requires manifest and executable files verified against the
embedded originals. Known shipped legacy diagnostics 1.0.0/1.1.0 and pre-icon
manifests are recognized, ignoring JSON field order, formatting and line endings.
All other fields and executable content must still match. The registry returns
a digest of the exact verified disk bytes, so the frontend compares its loaded
content before executing. Directory names and manifest claims cannot establish
trust. Modified builtin code follows the explicit enable flow as other scripts. Consent and permission ledgers cannot be
exported or imported with user backups.

## Consequences

Users can decide whether to run a script before its top-level side effects.
They can clear trust and disable a pack. This does not contain malicious code
after approval, authenticate remote dependencies imported by a trusted script,
or undo effects the script already performed. Supporting untrusted plugins
would require a separate isolated execution model and a restricted message
interface; that would be a new plugin contract.

## Lifecycle amendment, 2026-10-13

Managed plugin resources reject registration after disposal. Vue effects and
component scopes share cleanup ownership; cooperative permission identity is
removed with the runtime. State mutation and registry/file publication are
serialized. Pending asynchronous initialization can be cancelled by disable,
with a bounded wait for other stages; this cancels host waiting and managed
resources, not arbitrary JavaScript execution. Permission decisions must be
persisted before guarded actions proceed. See [the audit](../../reports/history/settings-plugins.md#origin-plugin-audit-2026-10-13).
