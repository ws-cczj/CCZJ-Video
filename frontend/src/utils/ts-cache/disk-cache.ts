/* eslint-disable no-console */
/**
 * IndexedDB 持久化层：磁盘副本的存、取、清、修剪。
 *
 * diskCache 实例只在这里 new 一次并导出，失效入口拿的是同一个对象；
 * 落盘键一律走 segmentCacheKey 归一化，与内存层保持同一套身份判定。
 */
import { SegmentDiskCache } from '../tsCacheDisk'
import { LOG_PREFIX } from './constants'
import { segmentCacheKey } from './segment-key'
import { prefetchState, session } from './store'
import type { DiskCacheInfo } from '../tsCacheDisk'

const MAX_DISK_BYTES = 160 * 1024 * 1024
const DISK_TTL_MS = 2 * 24 * 60 * 60 * 1000
const DISK_PRUNE_DEBOUNCE_MS = 5_000
const DISK_READ_TIMEOUT_MS = 250
export const diskCache = new SegmentDiskCache(MAX_DISK_BYTES, DISK_TTL_MS, DISK_PRUNE_DEBOUNCE_MS)
let diskQuotaWarned = false

export async function diskSave(url: string, buf: ArrayBuffer, epKey?: string, range?: string | null): Promise<void> {
  try {
    // 与内存层用同一个归一化键：签名轮换时否则同一片段在磁盘上堆积成多份。
    await diskCache.save(segmentCacheKey(url, range), buf, epKey || session.currentEpKey || '')
  } catch (error: any) {
    if (error?.name === 'QuotaExceededError' || String(error?.message || '').includes('quota')) {
      if (!diskQuotaWarned) {
        diskQuotaWarned = true
        console.warn(`${LOG_PREFIX} browser disk-cache quota is full; clearing cache`)
      }
      void diskCache.clear().catch(() => {})
    }
  }
}

/** A miss reads only the requested binary; late session results are discarded. */
export async function diskLoad(url: string, range?: string | null): Promise<ArrayBuffer | null> {
  const generation = prefetchState.cacheSession
  let timer: ReturnType<typeof setTimeout> | undefined
  try {
    // Disk caching is optional. A blocked database must not stall playback;
    // late reads own no cache writes and their result can safely be discarded.
    const data = await Promise.race([
      diskCache.loadSegment(segmentCacheKey(url, range)),
      new Promise<null>(resolve => { timer = setTimeout(() => resolve(null), DISK_READ_TIMEOUT_MS) }),
    ])
    return generation === prefetchState.cacheSession ? data : null
  } catch {
    return null
  } finally {
    if (timer !== undefined) clearTimeout(timer)
  }
}

export async function diskPrune(): Promise<void> {
  try {
    await diskCache.prune()
  } catch {
    // Disk caching is opportunistic and must not interrupt playback.
  }
}

export async function diskClear(): Promise<void> {
  try {
    await diskCache.clear()
  } catch {
    // Disk caching is opportunistic and must not interrupt playback.
  }
}

export async function diskCacheInfo(): Promise<DiskCacheInfo> {
  return diskCache.info()
}

export async function diskCachePrune(maxBytes: number): Promise<number> {
  try {
    return await diskCache.prune(maxBytes)
  } catch {
    return 0
  }
}
