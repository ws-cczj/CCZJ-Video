import { onBackendEvent } from '../api/events'
import { dropDetailCache, dropAllDetailCache } from './detailCache'
import { usePosterCacheStore } from './posterCache'
import { useVideoStore } from './video'
import { dropAllSegmentCache, dropSourceCache, dropVideoCache } from '../utils/tsCache'

/**
 * Go 侧统一失效层（app/cache/invalidate.go）在前端的订阅端。
 *
 * 前端有三份 Go 看不见的缓存：详情的 localStorage、海报的 localStorage、
 * TS 片段的内存 + IndexedDB。以前删片、改源、清库之后没人通知它们，
 * 于是列表里少了一部剧、点进去却还是旧详情旧海报，最长要等到各自的 TTL 才自愈。
 *
 * 这里只丢"我们自己的数据变了才会错"的内容：作用域与 Go 侧完全一致，
 * 由 Go 决定清到哪一层，前端不再自己判断。
 */

let started = false

export function startCacheInvalidation(): void {
  if (started) return
  started = true
  onBackendEvent('cache:invalidate', applyInvalidate)
}

function applyInvalidate(payload: { scope?: string; source_key?: string; vod_ids?: string[]; reason?: string }): void {
  const scope = String(payload?.scope || '')
  const sourceKey = String(payload?.source_key || '')
  const vodIds = Array.isArray(payload?.vod_ids) ? payload.vod_ids.filter(Boolean) : []
  const poster = usePosterCacheStore()
  const video = useVideoStore()

  switch (scope) {
    case 'all':
      dropAllDetailCache()
      poster.clearAll()
      video.dropFilterMeta()
      void dropAllSegmentCache()
      return
    case 'source':
      if (!sourceKey) return
      dropDetailCache(sourceKey)
      poster.dropSource(sourceKey)
      video.dropFilterMeta(sourceKey)
      void dropSourceCache(sourceKey)
      return
    case 'video':
      if (!sourceKey || vodIds.length === 0) return
      dropDetailCache(sourceKey, vodIds)
      poster.dropSource(sourceKey, vodIds)
      for (const vodId of vodIds) void dropVideoCache(sourceKey, vodId)
      return
    default:
      // 未知作用域说明两侧枚举对不上。宁可什么都不清（缓存最差旧到各自的 TTL），
      // 也不能清错范围 —— 误清一次等于把整源的重抓代价算到用户头上。
      console.warn('[CacheInvalidate] 未知的失效作用域:', scope || '(空)')
  }
}
