import { ref } from 'vue'

export interface ScriptTrustRecord { digest: string; allowed: boolean }
type Ledger = Record<string, ScriptTrustRecord>

export async function scriptDigest(parts: string[]): Promise<string> {
  const encoder = new TextEncoder()
  const bytes = encoder.encode(parts.map(part => `${encoder.encode(part).byteLength}:${part}`).join(''))
  const hash = await globalThis.crypto.subtle.digest('SHA-256', bytes)
  return [...new Uint8Array(hash)].map(value => value.toString(16).padStart(2, '0')).join('')
}

/** Package consent, independent of the cooperative write/network gates.
 * The injected script has the application's privileges after approval. */
export function createScriptTrust(deps: {
  load: () => Promise<string>
  save: (ledger: string) => Promise<void>
  ask: (id: string, name: string) => Promise<boolean>
}) {
  const records = ref<Ledger>({})
  let loaded: Promise<void> | null = null
  let queue: Promise<unknown> = Promise.resolve()

  async function load(): Promise<void> {
    if (!loaded) {
      loaded = deps.load().then(raw => {
        let parsed: unknown = {}
        try { parsed = raw ? JSON.parse(raw) : {} } catch { /* Corrupt consent grants nothing. */ }
        if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return
        const valid: Ledger = Object.create(null)
        for (const [id, value] of Object.entries(parsed)) {
          if (!value || typeof value !== 'object') continue
          const record = value as ScriptTrustRecord
          if (/^[a-f0-9]{64}$/.test(record.digest) && typeof record.allowed === 'boolean') valid[id] = record
        }
        records.value = valid
      }).catch(error => { loaded = null; throw error })
    }
    await loaded
  }

  function serialize<T>(work: () => Promise<T>): Promise<T> {
    const result = queue.then(work)
    queue = result.catch(() => {})
    return result
  }

  function requireTrust(id: string, name: string, digest: string, builtinDigest = '', allowPrompt = true): Promise<boolean> {
    return serialize(async () => {
      if (builtinDigest && digest === builtinDigest) return true
      await load()
      const existing = Object.prototype.hasOwnProperty.call(records.value, id) ? records.value[id] : undefined
      if (existing) return existing.allowed
      // Startup/rescan never interrupt the user. Only explicit enabling asks.
      if (!allowPrompt) return false
      const allowed = await deps.ask(id, name)
      const next = { ...records.value, [id]: { digest, allowed } }
      // Consent must be durable before executing code; a failed save denies
      // execution and keeps the previous ledger intact.
      await deps.save(JSON.stringify(next))
      records.value = next
      return allowed
    })
  }

  function revoke(id: string): Promise<void> {
    return serialize(async () => {
      await load()
      const next = { ...records.value }
      delete next[id]
      await deps.save(JSON.stringify(next))
      records.value = next
    })
  }

  return { records, load, requireTrust, revoke }
}
