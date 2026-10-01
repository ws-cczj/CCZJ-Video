import { MAX_CACHED_BYTES, MAX_CACHED_SEGMENTS, MAX_PER_EPISODE } from './constants'
import { segmentCacheKey } from './segment-key'
import { cache, episodeIndex, session } from './store'
import type { CacheEntry } from './store'

// ====== LRU 内存缓存 ======
//
// 状态本体在 store.cache 里（内存层、失效层、统计都要读同一份 lruCache /
// totalCacheBytes / epCacheKeys），本模块只提供读写它的这组操作。

function _incEpCache(epKey: string, cacheKey: string): void {
  let s = cache.epCacheKeys.get(epKey)
  if (!s) { s = new Set(); cache.epCacheKeys.set(epKey, s) }
  s.add(cacheKey)
}
function _decEpCache(epKey: string, cacheKey: string): void {
  const s = cache.epCacheKeys.get(epKey)
  if (!s) return
  s.delete(cacheKey)
  if (s.size === 0) cache.epCacheKeys.delete(epKey)
}

export function cacheGet(url: string, range?: string | null): CacheEntry | null {
  const key = segmentCacheKey(url, range)
  const entry = cache.lruCache.get(key)
  if (!entry) return null
  entry.ts = Date.now()
  // 命中移到尾部：Map 插入序即访问序，队首永远是全局最久未读的那条，全局淘汰才能 O(1)
  cache.lruCache.delete(key)
  cache.lruCache.set(key, entry)
  return entry
}

/**
 * 把片段写入内存缓存。
 *
 * `episodeKey` 由调用方给出（预取任务知道自己取的是哪一集），不再靠「拿 URL 去
 * segmentsByEpisode 里逐条比对」反推 —— 那个反推一旦因为签名轮换而比不中，条目就
 * 变成无主条目，既不受单集上限约束，也就永远不会被该上限淘汰。
 *
 * `fetchMs` 是这次下载真实花掉的时间，命中时要原样回报给 hls.js（见 loader）。
 */
export function cacheSet(
  url: string,
  buf: ArrayBuffer,
  episodeKey?: string,
  range?: string | null,
  fetchMs?: number
): void {
  const key = segmentCacheKey(url, range)
  const size = buf.byteLength
  const epKey = episodeKey || session.currentEpKey || ''

  // 同一份内容重写（时效签名换了、缓存键没换）：先把旧条目摘掉，否则同一份字节
  // 会被 totalCacheBytes 计两次，上限越管越松。
  const prevEntry = cache.lruCache.get(key)
  if (prevEntry) evict(prevEntry)

  // ---- 1) 硬约束：单集最多 "min(MAX_PER_EPISODE, 实际片段数)" 片 ----
  if (epKey) {
    const totalSegsForEp = episodeIndex.segmentsByEpisode.get(epKey)?.length ?? 0
    const hardLimit = Math.min(MAX_PER_EPISODE, totalSegsForEp > 0 ? totalSegsForEp : MAX_PER_EPISODE)
    if ((cache.epCacheKeys.get(epKey)?.size || 0) >= hardLimit) {
      const oldestOfEp = findLeastUsedEntry(epKey)
      if (oldestOfEp) evict(oldestOfEp)
    }
  }

  // ---- 2) 全局条数 / 字节上限 ----
  while (cache.lruCache.size >= MAX_CACHED_SEGMENTS || cache.totalCacheBytes + size > MAX_CACHED_BYTES) {
    const oldest = findLeastUsedEntry()
    if (!oldest) break
    evict(oldest)
  }

  cache.lruCache.set(key, {
    buffer: buf,
    size,
    cacheKey: key,
    ts: Date.now(),
    episodeKey: epKey,
    fetchMs: fetchMs && fetchMs > 0 ? Math.round(fetchMs) : 0,
  })
  cache.totalCacheBytes += size
  if (epKey) _incEpCache(epKey, key)
}

export function evict(entry: CacheEntry): void {
  cache.totalCacheBytes -= entry.size
  if (entry.episodeKey) _decEpCache(entry.episodeKey, entry.cacheKey)
  cache.lruCache.delete(entry.cacheKey)
}

/**
 * 真正的 LRU：谁最久没被读到就淘汰谁。
 *
 * 全局淘汰直接取队首 —— cacheGet 命中会把条目挪到尾部，所以队首就是最久未读的那条，O(1)。
 * 单集淘汰走 epCacheKeys 索引，只遍历该集名下的 ≤MAX_PER_EPISODE 条，不再扫整张 lruCache。
 * 之前两种情况都要 for-of 遍历全部 1024 条，而它挂在 cacheSet 这条热路径上：一集缓存填满后
 * 每插一片就全表扫一次。
 */
export function findLeastUsedEntry(onlyEpisodeKey?: string): CacheEntry | null {
  if (onlyEpisodeKey !== undefined) {
    let worstOfEp: CacheEntry | null = null
    const keys = cache.epCacheKeys.get(onlyEpisodeKey)
    if (keys) {
      for (const cacheKey of keys) {
        const entry = cache.lruCache.get(cacheKey)
        if (entry && (!worstOfEp || entry.ts < worstOfEp.ts)) worstOfEp = entry
      }
    }
    return worstOfEp
  }
  const oldestKey = cache.lruCache.keys().next().value
  if (oldestKey === undefined) return null
  return cache.lruCache.get(oldestKey) ?? null
}

export function cacheHas(url: string, range?: string | null): boolean { return cache.lruCache.has(segmentCacheKey(url, range)) }
export function cacheClear(): void { cache.lruCache.clear(); cache.totalCacheBytes = 0; cache.epCacheKeys.clear() }
