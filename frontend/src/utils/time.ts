/** SQLite timestamps without an offset are UTC, unlike JavaScript's local ISO dates. */
export function parseTimestamp(value: string): number {
  if (!value) return NaN
  const normalized = value.replace(' ', 'T')
  const hasZone = /(?:z|[+-]\d{2}:?\d{2})$/i.test(normalized)
  const hasTime = /\d{2}:\d{2}/.test(normalized)
  return Date.parse(!hasZone && hasTime ? `${normalized}Z` : normalized)
}
