export interface DiskCacheInfo {
  dbName: string
  storeName: string
  count: number
  bytes: number
  ttlDays: number
}

export interface DiskSegment {
  url: string
  data: ArrayBuffer
}

interface DiskSegmentRecord {
  url: string
  data: ArrayBuffer
}

interface DiskSegmentMeta {
  url: string
  episodeKey: string
  ts: number
  size: number
}

const DB_NAME = 'tscache'
const DB_VERSION = 2
const STORE_NAME = 'segments'
const META_STORE_NAME = 'segment_meta'

function openDatabase(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const request = indexedDB.open(DB_NAME, DB_VERSION)
    request.onupgradeneeded = () => {
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
    request.onsuccess = () => resolve(request.result)
    request.onerror = () => reject(request.error)
  })
}

function waitForTransaction(transaction: IDBTransaction): Promise<void> {
  return new Promise((resolve, reject) => {
    transaction.oncomplete = () => resolve()
    transaction.onabort = () => reject(transaction.error)
    transaction.onerror = () => reject(transaction.error)
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
 * The interface deliberately exposes records by episode, never a bulk blob
 * scan, so playback code cannot accidentally restore every cached video.
 */
export class SegmentDiskCache {
  private pruneTimer: number | null = null

  constructor(
    private readonly maxBytes: number,
    private readonly ttlMs: number,
    private readonly pruneDebounceMs: number,
  ) {}

  async save(url: string, data: ArrayBuffer, episodeKey: string): Promise<void> {
    const db = await openDatabase()
    try {
      const transaction = db.transaction([STORE_NAME, META_STORE_NAME], 'readwrite')
      const now = Date.now()
      transaction.objectStore(STORE_NAME).put({ url, data } satisfies DiskSegmentRecord)
      transaction.objectStore(META_STORE_NAME).put({
        url,
        episodeKey,
        ts: now,
        size: data.byteLength,
      } satisfies DiskSegmentMeta)
      await waitForTransaction(transaction)
      this.schedulePrune()
    } finally {
      db.close()
    }
  }

  async loadEpisode(episodeKey: string): Promise<DiskSegment[]> {
    if (!episodeKey) return []
    const db = await openDatabase()
    try {
      const metadataTransaction = db.transaction(META_STORE_NAME, 'readonly')
      const metadataRequest = metadataTransaction
        .objectStore(META_STORE_NAME)
        .index('episodeKey')
        .getAll(IDBKeyRange.only(episodeKey))
      const metadata = await waitForRequest(metadataRequest) as DiskSegmentMeta[]
      await waitForTransaction(metadataTransaction)

      const dataTransaction = db.transaction(STORE_NAME, 'readonly')
      const dataStore = dataTransaction.objectStore(STORE_NAME)
      const recordRequests = metadata.map((entry) => dataStore.get(entry.url))
      const records = await Promise.all(recordRequests.map((request) => waitForRequest(request))) as Array<DiskSegmentRecord | undefined>
      await waitForTransaction(dataTransaction)

      const now = Date.now()
      return metadata.flatMap((entry, index) => {
        const data = records[index]?.data
        if (now - entry.ts > this.ttlMs || !data || data.byteLength === 0) return []
        return [{ url: entry.url, data }]
      })
    } finally {
      db.close()
    }
  }

  async clear(): Promise<void> {
    const db = await openDatabase()
    try {
      const transaction = db.transaction([STORE_NAME, META_STORE_NAME], 'readwrite')
      transaction.objectStore(STORE_NAME).clear()
      transaction.objectStore(META_STORE_NAME).clear()
      await waitForTransaction(transaction)
    } finally {
      db.close()
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
    const db = await openDatabase()
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
      db.close()
    }
  }

  async info(): Promise<DiskCacheInfo> {
    try {
      const db = await openDatabase()
      try {
        const transaction = db.transaction(META_STORE_NAME, 'readonly')
        const request = transaction.objectStore(META_STORE_NAME).getAll()
        const entries = await waitForRequest(request) as DiskSegmentMeta[]
        await waitForTransaction(transaction)
        const bytes = entries.reduce((total, entry) => total + entry.size, 0)
        return this.infoResult(entries.length, bytes)
      } finally {
        db.close()
      }
    } catch {
      return this.infoResult(0, 0)
    }
  }

  async prune(maxBytes = this.maxBytes): Promise<number> {
    const db = await openDatabase()
    try {
      const readTransaction = db.transaction(META_STORE_NAME, 'readonly')
      const request = readTransaction.objectStore(META_STORE_NAME).getAll()
      const entries = await waitForRequest(request) as DiskSegmentMeta[]
      await waitForTransaction(readTransaction)

      const now = Date.now()
      let keptBytes = 0
      const toDelete: string[] = []
      entries.sort((left, right) => right.ts - left.ts)
      for (const entry of entries) {
        if (now - entry.ts > this.ttlMs || keptBytes + entry.size > maxBytes) {
          toDelete.push(entry.url)
        } else {
          keptBytes += entry.size
        }
      }
      if (toDelete.length === 0) return 0

      const deleteTransaction = db.transaction([STORE_NAME, META_STORE_NAME], 'readwrite')
      const segmentStore = deleteTransaction.objectStore(STORE_NAME)
      const metadataStore = deleteTransaction.objectStore(META_STORE_NAME)
      for (const url of toDelete) {
        segmentStore.delete(url)
        metadataStore.delete(url)
      }
      await waitForTransaction(deleteTransaction)
      return toDelete.length
    } finally {
      db.close()
    }
  }

  private schedulePrune(): void {
    if (this.pruneTimer != null) return
    this.pruneTimer = window.setTimeout(() => {
      this.pruneTimer = null
      void this.prune().catch(() => {})
    }, this.pruneDebounceMs)
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
