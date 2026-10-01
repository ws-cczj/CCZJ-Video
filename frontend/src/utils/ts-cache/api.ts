/* eslint-disable no-console */
/**
 * 对外门面：会话生命周期 + 统计快照 + 统一失效落点 + TsCache 聚合对象。
 *
 * 这里只做「把播放器/详情页的请求分发到各层」，具体机制在
 * memory-cache / prefetch / network / telemetry / m3u8 / disk-cache 各自的文件里。
 */
import { addAdDomain, getAdDomains } from './ad-filter'
import { diskCache, diskCacheInfo, diskCachePrune, diskClear, diskLoadForEpisode } from './disk-cache'
import { cacheClear, cacheHas, evict } from './memory-cache'
import { clearM3u8TextCache, fetchAndParseM3u8, getM3u8FromCache, setM3u8Cache } from './m3u8'
import {
  adaptiveBufferOffset,
  adaptiveConcurrency,
  adaptiveFragmentTimeout,
  adaptivePrefetchCount,
  adaptiveSpreadStep,
  getNetworkMode,
  resetNetworkDiagnosis,
} from './network'
import {
  activeInflight,
  beginCacheSession,
  notifyCurrentTs,
  prefetchFirst,
  prefetchFromM3u8,
  prefetchFromSegments,
  prefetchNextEpisode,
  setPlaybackBufferAhead,
} from './prefetch'
import { TsCacheLoader } from './loader'
import { notifyFragmentRequested } from './telemetry'
import {
  cache,
  epKeyFrom,
  episodeIndex,
  fireListeners,
  getListeners,
  netDiag,
  onStateChange,
  prefetchState,
  session,
  telemetry,
} from './store'
import type { EpisodeLite, Listener } from './store'

// ====== 公共 API ======

export function setEpisodes(list: EpisodeLite[]): void {
  session.episodes = Array.isArray(list) ? list.slice() : []

  const newVodId = (session.episodes[0]?.vod_id != null) ? String(session.episodes[0].vod_id) : ''

  // ⭐ 不再清空 LRU — 只清空"相对集索引"的映射
  episodeIndex.segmentsByEpisode.clear()
  episodeIndex.playedSegmentsByEpisode.clear()
  beginCacheSession()
  prefetchState.pendingUrls.clear()
  telemetry.epStats.clear()  // ⭐ 集数级统计重置
  telemetry.curEpStats = { hits: 0, misses: 0 }
  telemetry.recentFetchDurations.length = 0
  telemetry.recentFetchBytes.length = 0
  // ⭐ v3: 重置网络诊断状态（新视频 = 新网络环境）
  resetNetworkDiagnosis()
  netDiag.hedgeInFlight.clear()

  session.currentVodId = newVodId
  session.currentSourceKey = session.episodes[0]?.source_key ? String(session.episodes[0].source_key) : ''
  session.currentEpIdx = -1
  session.currentEpKey = ''
  session.playbackBufferAhead = 0
  fireListeners()
  // Cache restoration is scoped to the current episode in setCurrentEpisode.
}

export function setCurrentEpisode(idx: number): void {
  if (idx < 0 || idx >= session.episodes.length) {
    session.currentEpIdx = -1
    session.currentEpKey = ''
    return
  }
  if (session.currentEpIdx === idx) return
  session.currentEpIdx = idx
  const ep = session.episodes[idx]
  const newKey = epKeyFrom(ep, idx)
  if (session.currentEpKey === newKey) return
  session.currentEpKey = newKey
  // The queued URLs belong to the old episode. Completed cache entries are
  // still useful under LRU, but queued work must not survive an episode jump.
  beginCacheSession()
  session.playbackBufferAhead = 0
  if (session.currentEpKey) {
    if (!episodeIndex.playedSegmentsByEpisode.has(session.currentEpKey)) {
      episodeIndex.playedSegmentsByEpisode.set(session.currentEpKey, new Set<number>())
    }
    // ⭐ 从 epStats 中拿出该集的计数器（或新建）
    let cs = telemetry.epStats.get(session.currentEpKey)
    if (!cs) { cs = { hits: 0, misses: 0 }; telemetry.epStats.set(session.currentEpKey, cs) }
    telemetry.curEpStats = cs

    // ⭐ 按需从磁盘加载该集的缓存（只有当前集的片段才恢复到内存）
    diskLoadForEpisode(session.currentEpKey).catch(() => { })
  } else {
    telemetry.curEpStats = { hits: 0, misses: 0 }
  }
  fireListeners()
}

export function setTargetDuration(sec: number): void { if (sec > 0 && sec <= 30) session.targetDuration = sec }

/** 把解析好的片段列表绑定到"当前集"或 episodes[epIdx]，避免越界/无 key 的情况 */
export function setSegments(segments: string[], epIdx?: number): void {
  let key = session.currentEpKey
  // 未显式指定 currentEpKey，但传入了 epIdx → 用 episodes[epIdx] 生成
  if (!key && typeof epIdx === 'number' && epIdx >= 0 && session.episodes[epIdx]) {
    const ep = session.episodes[epIdx]
    key = epKeyFrom(ep, epIdx)
  }
  // 都没有，但 episodes 至少有一集 → 用第 0 集
  if (!key && session.episodes.length > 0) {
    key = epKeyFrom(session.episodes[0], 0)
  }
  if (!key) return

  const seen = new Set<string>()
  const list: string[] = []
  for (const s of segments) { if (s && !seen.has(s)) { seen.add(s); list.push(s) } }
  episodeIndex.segmentsByEpisode.set(key, list)

  // 如果此刻 currentEpKey 为空，也把它设为这集的 key（后续 notifyCurrentTs 能匹配）
  if (!session.currentEpKey) {
    session.currentEpKey = key
    if (typeof epIdx === 'number' && epIdx >= 0) session.currentEpIdx = epIdx
    let cs = telemetry.epStats.get(session.currentEpKey)
    if (!cs) { cs = { hits: 0, misses: 0 }; telemetry.epStats.set(session.currentEpKey, cs) }
    telemetry.curEpStats = cs
  }

  // Records in the v2 cache are indexed by episode key. Reload after the playlist is parsed.
  if (key === session.currentEpKey && list.length > 0) {
    diskLoadForEpisode(key).catch(() => { })
  }
  fireListeners()
}

export function stats() {
  const segs = session.currentEpKey ? (episodeIndex.segmentsByEpisode.get(session.currentEpKey) || []) : []
  let cachedForCurEp = 0
  if (session.currentEpKey) {
    for (const u of segs) if (cacheHas(u)) cachedForCurEp++
  }
  const h = telemetry.curEpStats.hits, m = telemetry.curEpStats.misses
  const total = h + m
  const played = session.currentEpKey ? (episodeIndex.playedSegmentsByEpisode.get(session.currentEpKey)?.size || 0) : 0
  const avgMs = telemetry.recentFetchDurations.length > 0
    ? Math.round(telemetry.recentFetchDurations.reduce((a, b) => a + b, 0) / telemetry.recentFetchDurations.length) : 0
  return {
    hits: h, misses: m, entries: cachedForCurEp, totalEntries: cache.lruCache.size,
    bytes: cache.totalCacheBytes, totalSegments: segs.length,
    playedSegments: played,
    hitRate: total === 0 ? 0 : h / total,
    avgFetchMs: avgMs,
    prefetchCount: adaptivePrefetchCount(),
    // 预取只维持播放窗口而非下载整集；将目标单独提供给 UI，避免把
    // “24/813 个全片段”误显示成缓存一直未完成。
    prefetchTarget: Math.min(adaptivePrefetchCount(), segs.length),
    queued: prefetchState.queue.filter((job) => job.episodeKey === session.currentEpKey).length,
    inflight: activeInflight(),
    bufferOffset: adaptiveBufferOffset(),
    spreadStep: adaptiveSpreadStep(),
    cacheMB: cache.totalCacheBytes / 1024 / 1024,
    // ⭐ v3: 网络诊断信息
    networkMode: netDiag.mode,
    concurrency: adaptiveConcurrency(),
    fragmentTimeout: adaptiveFragmentTimeout(),
    consecutiveSlowSegs: netDiag.consecutiveSlowSegs,
  }
}

/** 按稳定 key 取一集的进度信息（兼容旧数字 idx 调用） */
export function episodeProgress(idx: number): { total: number; cached: number } {
  let key: string | null = null
  if (idx >= 0 && session.episodes[idx]) {
    const ep = session.episodes[idx]
    key = epKeyFrom(ep, idx)
  }
  const segs = (key && episodeIndex.segmentsByEpisode.get(key)) || []
  const cached = segs.filter((u) => cacheHas(u)).length
  return { total: segs.length, cached }
}

export function getTotalEpisodes(): number { return session.episodes.length }
// hls.js always uses TsCacheLoader now. Do not monkey-patch window.fetch:
// Wails runtime RPC uses fetch too, and a global wrapper obscures its failures
// in DevTools while providing no cache benefit to the custom HLS loader.
export function enable(): void { session.enabled = true }

// ⭐ v3: ABR 回调注册 —— VideoPlayer 通过此接口接收降码率信号
export function setAbrSwitchCallback(cb: ((targetLevel: number) => void) | null): void {
  netDiag.abrSwitchCallback = cb
}

export function clear(): void {
  session.episodes = []
  session.currentEpIdx = -1
  session.currentEpKey = ''
  session.currentVodId = ''
  episodeIndex.segmentsByEpisode.clear()
  episodeIndex.playedSegmentsByEpisode.clear()
  cache.epCacheKeys.clear()
  cacheClear()
  clearM3u8TextCache()
  beginCacheSession()
  prefetchState.pendingUrls.clear()
  netDiag.hedgeInFlight.clear()
  telemetry.epStats.clear()
  telemetry.curEpStats = { hits: 0, misses: 0 }
  telemetry.recentFetchDurations.length = 0
  telemetry.recentFetchBytes.length = 0
  // ⭐ v3: 重置网络诊断状态
  resetNetworkDiagnosis()
  netDiag.abrSwitchCallback = null
  if (prefetchState.debounceTimer != null) { window.clearTimeout(prefetchState.debounceTimer); prefetchState.debounceTimer = null }
  fireListeners()
}

// ====== 统一失效层的前端落点 ======
//
// Go 侧清完自己的缓存后发 cache:invalidate，这里按同一粒度丢掉片段缓存：内存 LRU、
// 该集的片段清单、IndexedDB 落盘副本。
//
// 刻意不碰 episodes / currentEpIdx 这类会话状态：用户可能正在看同源的另一个视频，
// 后台每写一次库就把播放器重置一遍，代价比留下几片旧片段大得多。

async function dropCacheByPrefixes(prefixes: string[]): Promise<void> {
  for (const entry of Array.from(cache.lruCache.values())) {
    if (entry.episodeKey && prefixes.some((p) => entry.episodeKey.startsWith(p))) evict(entry)
  }
  for (const key of Array.from(episodeIndex.segmentsByEpisode.keys())) {
    if (!prefixes.some((p) => key.startsWith(p))) continue
    episodeIndex.segmentsByEpisode.delete(key)
    episodeIndex.playedSegmentsByEpisode.delete(key)
    telemetry.epStats.delete(key)
    cache.epCacheKeys.delete(key)
  }
  // 正在播的这一集被失效了：进行中的预取必须断掉，否则它们会把旧片段清单里的 URL
  // 重新填回刚清空的缓存。
  if (session.currentEpKey && prefixes.some((p) => session.currentEpKey.startsWith(p))) beginCacheSession()
  try {
    await diskCache.dropByEpisodePrefixes(prefixes)
  } catch { /* IndexedDB 打不开：内存侧已经失效，落盘副本最坏等到 TTL 自己走 */ }
  fireListeners()
}

/** 丢掉某一部剧全部集的片段缓存。 */
export function dropVideoCache(sourceKey: string, vodId: string): Promise<void> {
  if (!sourceKey || !vodId) return Promise.resolve()
  return dropCacheByPrefixes([`ep_${sourceKey}_${vodId}_`])
}

/** 丢掉某个采集源全部片的片段缓存。 */
export function dropSourceCache(sourceKey: string): Promise<void> {
  if (!sourceKey) return Promise.resolve()
  return dropCacheByPrefixes([`ep_${sourceKey}_`])
}

/** 用户显式清除：所有片段缓存与 m3u8 文本缓存一起丢掉，当前会话的剧集清单保留。 */
export async function dropAllSegmentCache(): Promise<void> {
  cacheClear()
  episodeIndex.segmentsByEpisode.clear()
  episodeIndex.playedSegmentsByEpisode.clear()
  cache.epCacheKeys.clear()
  telemetry.epStats.clear()
  clearM3u8TextCache()
  try {
    await diskCache.clear()
  } catch { /* 同上 */ }
  fireListeners()
}

export const TsCache = {
  enable, clear, stats,
  setEpisodes, setCurrentEpisode, setSegments, setTargetDuration, setPlaybackBufferAhead,
  prefetchFirst, prefetchNextEpisode, prefetchFromM3u8,
  notifyCurrentTs, notifyFragmentRequested,
  episodeProgress, getTotalEpisodes,
  onStateChange, removeListener: (cb: Listener) => getListeners().delete(cb),
  diskClear, diskCacheInfo, diskCachePrune,
  dropVideoCache, dropSourceCache, dropAllSegmentCache,
  // ⭐ v1.7.0-beta.1 统一 loader：hls.js 新 API，一个 loader 处理所有请求类型
  //   用法: new Hls({ loader: TsCache.TsCacheLoader })
  TsCacheLoader,
  fetchAndParseM3u8, getM3u8FromCache, setM3u8Cache,
  prefetchFromSegments: prefetchFromSegments,
  // ⭐ v3: 网络诊断 + ABR 集成
  getNetworkMode,
  setAbrSwitchCallback,
  adaptiveConcurrency,
  adaptiveFragmentTimeout,
  // ⭐ 广告域名上报
  addAdDomain,
  getAdDomains,
}
