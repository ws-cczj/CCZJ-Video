/* eslint-disable no-console */
/**
 * IndexedDB 持久化层：磁盘副本的存、取、清、修剪。
 *
 * diskCache 实例只在这里 new 一次并导出，失效入口拿的是同一个对象；
 * 落盘键一律走 segmentCacheKey 归一化，与内存层保持同一套身份判定。
 */
import { SegmentDiskCache } from '../tsCacheDisk'
import { LOG_PREFIX } from './constants'
import { cacheSet } from './memory-cache'
import { segmentCacheKey } from './segment-key'
import { cache, session } from './store'
import type { DiskCacheInfo } from '../tsCacheDisk'

const MAX_DISK_BYTES = 160 * 1024 * 1024
const DISK_TTL_MS = 2 * 24 * 60 * 60 * 1000
const DISK_PRUNE_DEBOUNCE_MS = 5_000
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

export async function diskLoadForEpisode(epKey: string): Promise<number> {
  if (!epKey) return 0
  try {
    const entries = await diskCache.loadEpisode(epKey)
    if (session.currentEpKey !== epKey) return 0
    for (const entry of entries) cacheSet(entry.url, entry.data, epKey)
    if (entries.length > 0) {
      const mb = (cache.totalCacheBytes / 1024 / 1024).toFixed(1)
      console.log(`${LOG_PREFIX} restored ${entries.length} segments (${mb} MB) for ${epKey}`)
    }
    return entries.length
  } catch {
    return 0
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
