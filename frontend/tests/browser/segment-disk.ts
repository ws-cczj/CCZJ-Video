import { TsCache } from '../../src/utils/tsCache'
import { diskCache } from '../../src/utils/ts-cache/disk-cache'
import { cacheClear } from '../../src/utils/ts-cache/memory-cache'
import { session } from '../../src/utils/ts-cache/store'

const mib = 1024 ** 2
const url = (index: number) => `https://segments.invalid/${index}.ts`
;(window as any).segmentDiskTest = {
  async seed() {
    await diskCache.clear()
    const bytes = new Uint8Array(2 * mib)
    for (let index = 0; index < 40; index++) {
      bytes.fill(index + 1)
      await diskCache.save(url(index), bytes.buffer, 'ep_fixture_1_1')
    }
    cacheClear()
  },
  open() {
    TsCache.setEpisodes([{ source_key: 'fixture', vod_id: '1', ep_num: 1, ep_url: 'https://segments.invalid/playlist' }])
    TsCache.setCurrentEpisode(0)
    TsCache.setSegments(Array.from({ length: 40 }, (_, index) => url(index)))
  },
  stats: () => TsCache.stats(),
  episode: () => session.currentEpKey,
  clearMemory: cacheClear,
  prefetch(index: number) {
    TsCache.enable()
    TsCache.setPlaybackBufferAhead(60)
    TsCache.prefetchFromSegments(Array.from({ length: 40 }, (_, item) => url(item)), 0, index, 1)
  },
  async expire(index: number) {
    const db = await new Promise<IDBDatabase>((resolve, reject) => {
      const request = indexedDB.open('tscache', 2)
      request.onsuccess = () => resolve(request.result)
      request.onerror = () => reject(request.error)
    })
    try {
      await new Promise<void>((resolve, reject) => {
        const tx = db.transaction('segment_meta', 'readwrite')
        const store = tx.objectStore('segment_meta')
        const request = store.get(url(index))
        request.onsuccess = () => store.put({ ...request.result, ts: Date.now() - 3 * 24 * 60 * 60 * 1000 })
        tx.oncomplete = () => resolve()
        tx.onabort = () => reject(tx.error)
        tx.onerror = () => reject(tx.error)
      })
    } finally { db.close() }
  },
  load(index: number) {
    return new Promise((resolve, reject) => {
      const loader = new TsCache.TsCacheLoader({})
      loader.load({ url: url(index), type: 'media-fragment', responseType: 'arraybuffer' }, {}, {
        onSuccess: (response: { data: ArrayBuffer }) => {
          const data = new Uint8Array(response.data)
          resolve({ bytes: data.byteLength, first: data[0], last: data[data.length - 1] })
          loader.destroy()
        },
        onError: (error: unknown) => { loader.destroy(); reject(error) },
      })
    })
  },
}
