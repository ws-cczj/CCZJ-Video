import { ReadingStateApply, ReadingStateLoad } from '../api/app'
import { activityDiagnostics } from '../diagnostics/activity'

export interface ReadingRecord { bucket: 'shelf' | 'progress' | 'preferences'; key: string; value: string }
export interface ReadingChange extends ReadingRecord { delete?: boolean }

export function readingRecord(bucket: ReadingRecord['bucket'], key: string, value: unknown): ReadingRecord {
  return { bucket, key, value: JSON.stringify(value) }
}

/** Owns hydration, ordered writes, coalescing and explicit retry of unsaved edits.
 * Callers retain their presentation shapes; only changed identities cross IPC.
 */
export function readingPersistence(options: {
  kind: 'novel' | 'comic'
  legacy: () => ReadingRecord[]
  apply: (records: ReadingRecord[]) => void
  status: (ready: boolean, failed: boolean) => void
}) {
  let ready = false
  let loading: Promise<boolean> | undefined
  let writing: Promise<boolean> | undefined
  const pending = new Map<string, ReadingChange>()
  let failed=false
  function observe(){activityDiagnostics.readingState(options.kind,{ready,failed,pending:pending.size})}
  observe()

  async function init(): Promise<boolean> {
    if (ready) return true
    if (loading) return loading
    loading = (async () => {
      const finish=activityDiagnostics.begin(`reading.${options.kind}.hydrate`)
      try {
        const records = await ReadingStateLoad(options.kind, JSON.stringify(options.legacy()))
        if (!Array.isArray(records)) throw new Error('Invalid reading snapshot')
        const decoded = records.map(record => {
          if (!record || !['shelf', 'progress', 'preferences'].includes(record.bucket) || typeof record.key !== 'string' || typeof record.value !== 'string') throw new Error('Invalid reading record')
          const value: unknown = JSON.parse(record.value)
          if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('Invalid reading record value')
          return record as ReadingRecord
        })
        options.apply(decoded)
        ready = true
        options.status(true, false)
        failed=false;observe();finish('success')
        return true
      } catch (error) {
        console.warn('Reading state hydration failed', error)
        options.status(false, true)
        failed=true;observe();finish('failed')
        return false
      } finally { loading = undefined }
    })()
    return loading
  }

  function enqueue(change: ReadingChange): void {
    pending.set(`${change.bucket}\u0000${change.key}`, change)
    observe()
  }

  async function flush(): Promise<boolean> {
    if (!await init()) return false
    if (writing) {
      const saved = await writing
      return saved && (pending.size ? flush() : true)
    }
    writing = (async () => {
      while (pending.size) {
        const batch = [...pending.entries()].slice(0, 256)
        const finish=activityDiagnostics.begin(`reading.${options.kind}.save`)
        try {
          await ReadingStateApply(options.kind, JSON.stringify(batch.map(([, change]) => change)))
          finish('success')
        } catch (error) {
          console.warn('Reading state write failed; edits retained for retry', error)
          options.status(true, true)
          failed=true;observe();finish('failed')
          return false
        }
        // A newer edit to this identity may have arrived during the write.
        for (const [key, change] of batch) if (pending.get(key) === change) pending.delete(key)
        observe()
      }
      options.status(true, false)
      failed=false;observe()
      return true
    })()
    try { return await writing } finally { writing = undefined }
  }

  return { init, enqueue, flush, async retry() { return await init() && await flush() } }
}
