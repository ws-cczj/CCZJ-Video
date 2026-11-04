import { CCZJ } from '../constants/cczj'
export interface DiskCacheInfo {
  dbName: string
  storeName: string
  count: number
  bytes: number
  ttlDays: number
}

interface DiskSegmentRecord {
  url: string
  data: ArrayBuffer
}

interface DiskSegmentMeta {
  url: string
  episodeKey: string
  ts: number
  accessedAt?: number
  size: number
}

const DB_NAME = CCZJ.Storage.segmentDatabase
const DB_VERSION = 2
const STORE_NAME = CCZJ.Storage.segmentStore
const META_STORE_NAME = CCZJ.Storage.segmentMetadataStore

function openDatabase(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const request = indexedDB.open(DB_NAME, DB_VERSION)
    let settled = false
    const fail = () => {
      if (settled) return
      settled = true
      clearTimeout(timer)
      reject((request.readyState === 'done' && request.error) || new Error('segment cache database is unavailable'))
    }
    const timer = setTimeout(fail, 1000)
    request.onupgradeneeded = () => {
      if (settled) { request.transaction?.abort(); return }
      const db = request.result
      // The segment cache is disposable. A schema change discards it instead
      // of attempting to migrate opaque media blobs in the browser.
      if (db.objectStoreNames.contains(STORE_NAME)) db.deleteObjectStore(STORE_NAME)
      if (db.objectStoreNames.contains(META_STORE_NAME)) db.deleteObjectStore(META_STORE_NAME)
      db.createObjectStore(STORE_NAME, { keyPath: 'url' })
      const metadata = db.createObjectStore(META_STORE_NAME, { keyPath: 'url' })
      metadata.createIndex('episodeKey', 'episodeKey', { unique: false })
      metadata.createIndex('ts', 'ts', { unique: false })
    }
    request.onsuccess = () => {
      if (settled) { request.result.close(); return }
      settled = true
      clearTimeout(timer)
      resolve(request.result)
    }
    request.onerror = fail
    request.onblocked = fail
  })
}

function waitForTransaction(transaction: IDBTransaction): Promise<void> {
  return new Promise((resolve, reject) => {
    transaction.oncomplete = () => resolve()
    transaction.onabort = () => reject(transaction.error)
    transaction.onerror = event => reject((event.target as IDBRequest | null)?.error ?? transaction.error)
  })
}

function waitForRequest<T>(request: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    request.onsuccess = () => resolve(request.result)
    request.onerror = () => reject(request.error)
  })
}

/**
 * Owns the browser-side binary segment cache and its metadata indexes.
 * Playback reads one segment at a time. Episode metadata is used for pruning
 * and invalidation, never to restore a whole episode into the playback heap.
 */
export class SegmentDiskCache {
  private maintenancePaused = false
  private maintenanceTransaction: IDBTransaction | undefined

  setMaintenancePaused(paused: boolean): void {
    this.maintenancePaused = paused
    if (paused) {
      try { this.maintenanceTransaction?.abort() } catch { /* Already committed. */ }
    }
  }
  private connection: Promise<IDBDatabase> | undefined
  private closeTimer: ReturnType<typeof setTimeout> | undefined
  private users = 0

  private async acquire(): Promise<IDBDatabase> {
    clearTimeout(this.closeTimer)
    this.closeTimer = undefined
    if (!this.connection) {
      const pending = openDatabase().then(db => {
        const invalidate = () => { if (this.connection === pending) this.connection = undefined }
        db.onversionchange = () => { invalidate(); db.close() }
        db.onclose = invalidate
        return db
      }).catch(error => {
        if (this.connection === pending) this.connection = undefined
        throw error
      })
      this.connection = pending
    }
    const db = await this.connection
    this.users++
    return db
  }

  private release(): void {
    if (--this.users > 0) return
    // Reuse through a burst of fragments, release after playback becomes idle.
    this.closeTimer = setTimeout(() => {
      this.closeTimer = undefined
      const pending = this.connection
      void pending?.then(current => {
        if (this.users === 0 && this.connection === pending) {
          this.connection = undefined
          current.close()
        }
      }).catch(() => {})
    }, 5000)
  }

  constructor(
    private readonly maxBytes: number,
    private readonly ttlMs: number,
  ) {}

  async save(url: string, data: ArrayBuffer, episodeKey: string): Promise<boolean> {
    const db = await this.acquire()
    try {
      if (this.maintenancePaused) return false
      const transaction = db.transaction([STORE_NAME, META_STORE_NAME], 'readwrite')
      this.maintenanceTransaction = transaction
      const now = Date.now()
      transaction.objectStore(STORE_NAME).put({ url, data } satisfies DiskSegmentRecord)
      transaction.objectStore(META_STORE_NAME).put({
        url,
        episodeKey,
        ts: now,
        accessedAt: now,
        size: data.byteLength,
      } satisfies DiskSegmentMeta)
      await waitForTransaction(transaction)
      return true
    } finally {
      this.maintenanceTransaction = undefined
      this.release()
    }
  }

  async loadSegment(url: string): Promise<ArrayBuffer | null> {
    if (!url) return null
    const db = await this.acquire()
    try {
      const metadataTransaction = db.transaction(META_STORE_NAME, 'readonly')
      const metadataRequest = metadataTransaction.objectStore(META_STORE_NAME).get(url)
      const metadata = await waitForRequest(metadataRequest) as DiskSegmentMeta | undefined
      await waitForTransaction(metadataTransaction)
      // Check expiry before cloning a potentially large binary from IndexedDB.
      if (!metadata || !Number.isFinite(metadata.ts) || !Number.isFinite(metadata.size) || Date.now() - metadata.ts > this.ttlMs || metadata.size <= 0 || metadata.size > this.maxBytes) return null

      const dataTransaction = db.transaction(STORE_NAME, 'readonly')
      const recordRequest = dataTransaction.objectStore(STORE_NAME).get(url)
      const record = await waitForRequest(recordRequest) as DiskSegmentRecord | undefined
      await waitForTransaction(dataTransaction)
      const data = record?.data
      return data instanceof ArrayBuffer && data.byteLength === metadata.size ? data : null
    } finally {
      this.release()
    }
  }

  async dropSegment(url: string): Promise<void> {
    const db = await this.acquire()
    try {
      const transaction = db.transaction([STORE_NAME, META_STORE_NAME], 'readwrite')
      transaction.objectStore(STORE_NAME).delete(url)
      transaction.objectStore(META_STORE_NAME).delete(url)
      await waitForTransaction(transaction)
    } finally { this.release() }
  }

  async clear(): Promise<void> {
    const db = await this.acquire()
    try {
      const transaction = db.transaction([STORE_NAME, META_STORE_NAME], 'readwrite')
      transaction.objectStore(STORE_NAME).clear()
      transaction.objectStore(META_STORE_NAME).clear()
      await waitForTransaction(transaction)
    } finally {
      this.release()
    }
  }

  /**
   * 按 episodeKey 前缀丢掉落盘片段，返回删掉的记录数。
   *
   * 前缀而不是精确 key：失效事件只给到「哪一部剧」（ep_src_vid_）或「哪个源」
   * （ep_src_），一集有多少个片段、片段 URL 长什么样都不在事件里，而 meta 表只有
   * 元数据、扫一遍很便宜。删的是 url 主键上的两份记录，不会留下孤儿 blob。
   */
  async dropByEpisodePrefixes(prefixes: string[]): Promise<number> {
    const wanted = prefixes.filter(Boolean)
    if (wanted.length === 0) return 0
    const db = await this.acquire()
    try {
      const readTransaction = db.transaction(META_STORE_NAME, 'readonly')
      const request = readTransaction.objectStore(META_STORE_NAME).getAll()
      const entries = await waitForRequest(request) as DiskSegmentMeta[]
      await waitForTransaction(readTransaction)

      const urls = entries
        .filter((entry) => wanted.some((prefix) => entry.episodeKey.startsWith(prefix)))
        .map((entry) => entry.url)
      if (urls.length === 0) return 0

      const deleteTransaction = db.transaction([STORE_NAME, META_STORE_NAME], 'readwrite')
      const segmentStore = deleteTransaction.objectStore(STORE_NAME)
      const metadataStore = deleteTransaction.objectStore(META_STORE_NAME)
      for (const url of urls) {
        segmentStore.delete(url)
        metadataStore.delete(url)
      }
      await waitForTransaction(deleteTransaction)
      return urls.length
    } finally {
      this.release()
    }
  }

  async info(): Promise<DiskCacheInfo> {
    try {
      const db = await this.acquire()
      try {
        const transaction = db.transaction(META_STORE_NAME, 'readonly')
        const request = transaction.objectStore(META_STORE_NAME).getAll()
        const entries = await waitForRequest(request) as DiskSegmentMeta[]
        await waitForTransaction(transaction)
        const bytes = entries.reduce((total, entry) => total + entry.size, 0)
        return this.infoResult(entries.length, bytes)
      } finally {
        this.release()
      }
    } catch {
      return this.infoResult(0, 0)
    }
  }

  async touch(touches: ReadonlyMap<string, number>): Promise<void> {
    const db = await this.acquire()
    try {
      if (this.maintenancePaused || touches.size === 0) return
      const transaction = db.transaction(META_STORE_NAME, 'readwrite')
      this.maintenanceTransaction = transaction
      const store = transaction.objectStore(META_STORE_NAME)
      for (const [url, accessedAt] of touches) {
        const request = store.get(url)
        request.onsuccess = () => {
          const entry = request.result as DiskSegmentMeta | undefined
          if (entry) store.put({ ...entry, accessedAt: Math.max(entry.accessedAt ?? entry.ts, accessedAt) })
        }
      }
      await waitForTransaction(transaction)
    } finally { this.maintenanceTransaction = undefined; this.release() }
  }

  async prune(maxBytes = this.maxBytes, replacements: ReadonlySet<string> = new Set()): Promise<number> {
    const db = await this.acquire()
    try {
      if (this.maintenancePaused) return 0
      const readTransaction = db.transaction(META_STORE_NAME, 'readonly')
      const request = readTransaction.objectStore(META_STORE_NAME).getAll()
      const entries = await waitForRequest(request) as DiskSegmentMeta[]
      await waitForTransaction(readTransaction)

      const now = Date.now()
      let keptBytes = 0
      let keptCount = replacements.size
      const toDelete: string[] = []
      entries.sort((left, right) => (right.accessedAt ?? right.ts) - (left.accessedAt ?? left.ts))
      for (const entry of entries) {
        if (replacements.has(entry.url)) continue
        if (!Number.isFinite(entry.size) || entry.size <= 0 || !Number.isFinite(entry.ts) || now - entry.ts > this.ttlMs || keptBytes + entry.size > maxBytes || keptCount >= 2048) {
          toDelete.push(entry.url)
        } else {
          keptBytes += entry.size
          keptCount++
        }
      }
      if (toDelete.length === 0) return 0
      if (this.maintenancePaused) return 0

      const deleteTransaction = db.transaction([STORE_NAME, META_STORE_NAME], 'readwrite')
      this.maintenanceTransaction = deleteTransaction
      const segmentStore = deleteTransaction.objectStore(STORE_NAME)
      const metadataStore = deleteTransaction.objectStore(META_STORE_NAME)
      for (const url of toDelete) {
        segmentStore.delete(url)
        metadataStore.delete(url)
      }
      await waitForTransaction(deleteTransaction)
      return toDelete.length
    } finally {
      this.maintenanceTransaction = undefined
      this.release()
    }
  }

  private infoResult(count: number, bytes: number): DiskCacheInfo {
    return {
      dbName: DB_NAME,
      storeName: STORE_NAME,
      count,
      bytes,
      ttlDays: this.ttlMs / (24 * 60 * 60 * 1000),
    }
  }
}
