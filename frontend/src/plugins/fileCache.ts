/** String cache accounting uses a conservative UTF-16 estimate. */
export function createPluginFileCache(maxBytes: number, maxEntries = 128) {
  const entries = new Map<string, string>()
  let bytes = 0
  return {
    get(key: string) {
      const value = entries.get(key)
      if (value !== undefined) { entries.delete(key); entries.set(key, value) }
      return value
    },
    set(key: string, value: string) {
      const old = entries.get(key)
      if (old !== undefined) { bytes -= old.length * 2; entries.delete(key) }
      if (!value || value.length * 2 > maxBytes) return
      entries.set(key, value)
      bytes += value.length * 2
      while (bytes > maxBytes || entries.size > maxEntries) {
        const first = entries.keys().next().value!
        bytes -= entries.get(first)!.length * 2
        entries.delete(first)
      }
    },
    clear() { entries.clear(); bytes = 0 },
  }
}
