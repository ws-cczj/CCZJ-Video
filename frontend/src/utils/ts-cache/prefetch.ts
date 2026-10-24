/* eslint-disable no-console */
/**
 * 预取 + 取回链路：排队、去重、前台优先、超时与取消。
 *
 * 会话号（cacheSession）是这里的生命线：切集 / 换视频 / 失效都会 beginCacheSession()
 * 把在途请求全部 abort 并作废其结果，回写的每一处都要重新核对 generation，
 * 否则旧集的字节会污染刚清空的缓存。
 */
import {
  LOG_PREFIX,
  MAX_QUEUE_PER_EPISODE,
  PREFETCH_AHEAD_NEXT_EPISODE,
  PREFETCH_DEBOUNCE_MS,
  PREFETCH_TRIGGER_WHEN_LESS_THAN,
} from './constants'
import { diskLoad, diskSave } from './disk-cache'
import { cacheHas, cacheSet } from './memory-cache'
import {
  adaptiveConcurrency,
  adaptiveBufferOffset,
  adaptiveFragmentTimeout,
  adaptivePrefetchCount,
  adaptiveSpreadStep,
  updateNetworkDiagnosis,
} from './network'
import { findSegmentIndex, segmentCacheKey } from './segment-key'
import { fetchAndParseM3u8 } from './m3u8'
import {
  episodeIndex,
  epKeyFrom,
  fireListeners,
  prefetchState,
  session,
  telemetry,
} from './store'
import type { FetchJob } from './store'
import { recordFetchDuration } from './telemetry'

// ====== 会话与在途计数 ======

function pendingKey(url: string, generation = prefetchState.cacheSession): string {
  return `${generation}:${segmentCacheKey(url)}`
}

export function activeInflight(): number {
  return prefetchState.inflightBySession.get(prefetchState.cacheSession) || 0
}

function finishInflight(generation: number): void {
  const next = (prefetchState.inflightBySession.get(generation) || 0) - 1
  if (next > 0) prefetchState.inflightBySession.set(generation, next)
  else prefetchState.inflightBySession.delete(generation)
}

export function beginCacheSession(): void {
  prefetchState.cacheSession++
  if (prefetchState.debounceTimer != null) {
    window.clearTimeout(prefetchState.debounceTimer)
    prefetchState.debounceTimer = null
  }
  for (const controller of prefetchState.prefetchControllers) controller.abort()
  prefetchState.prefetchControllers.clear()
  for (const job of prefetchState.queue) prefetchState.pendingUrls.delete(pendingKey(job.url, job.generation))
  prefetchState.queue.length = 0
  prefetchState.epQueueCount.clear()
}

// ====== 排队与拉取 ======

function enqueue(job: FetchJob): boolean {
  if (!job?.url) return false
  const generation = prefetchState.cacheSession
  if (prefetchState.pendingUrls.has(pendingKey(job.url, generation))) return false
  if (cacheHas(job.url)) return false
  const cnt = prefetchState.epQueueCount.get(job.episodeKey) || 0
  if (cnt >= MAX_QUEUE_PER_EPISODE) return false
  prefetchState.pendingUrls.add(pendingKey(job.url, generation))
  prefetchState.epQueueCount.set(job.episodeKey, cnt + 1)
  const scopedJob = { ...job, generation }
  prefetchState.queue.push(scopedJob)
  prefetchState.queue.sort((a,b) => a.priority - b.priority)
  return true
}

function scheduleDrain(): void {
  if (prefetchState.debounceTimer != null) return
  prefetchState.debounceTimer = window.setTimeout(() => { prefetchState.debounceTimer = null; drainQueue() }, PREFETCH_DEBOUNCE_MS)
}

function drainQueue(): void {
  if (!canPrefetch()) return
  const maxConc = adaptiveConcurrency()
  while (activeInflight() < maxConc && prefetchState.queue.length > 0) {
    const job = prefetchState.queue.shift(); if (!job) break
    const generation = job.generation ?? prefetchState.cacheSession
    const active = activeInflight() + 1
    prefetchState.inflightBySession.set(generation, active)
    runOne(job).finally(() => {
      finishInflight(generation)
      if (generation === prefetchState.cacheSession) drainQueue()
    })
  }
}

function canPrefetch(): boolean {
  return session.enabled && prefetchState.foregroundActive === 0 && adaptiveConcurrency() > 0 && session.playbackBufferAhead / session.playbackRate >= Math.max(20, session.targetDuration * 3)
}

/**
 * 播放器上报"前方还有多少秒缓冲"。它决定后台预取能不能开工，
 * 所以由预取层自己持有：够就立刻尝试排空队列。
 */
export function beginForegroundFetch(): () => void {
  prefetchState.foregroundActive++
  for (const controller of prefetchState.prefetchControllers) controller.abort()
  let released = false
  return () => {
    if (released) return
    released = true
    prefetchState.foregroundActive = Math.max(0,prefetchState.foregroundActive - 1)
    if (canPrefetch()) scheduleDrain()
  }
}

export function setPlaybackBufferAhead(seconds: number, playbackRate = 1): void {
  session.playbackRate = Number.isFinite(playbackRate) && playbackRate > 0 ? playbackRate : 1
  session.playbackBufferAhead = Number.isFinite(seconds) ? Math.max(0, seconds) : 0
  if (canPrefetch()) scheduleDrain()
  else for (const controller of prefetchState.prefetchControllers) controller.abort()
}

async function runOne(job: FetchJob): Promise<void> {
  const generation = job.generation ?? prefetchState.cacheSession
  const timeout = adaptiveFragmentTimeout()
  let ctrl: AbortController | null = null
  let timeoutID: number | null = null
  try {
    if (generation !== prefetchState.cacheSession || !canPrefetch() || cacheHas(job.url)) return
    // Read only this scheduled segment. Without this check, removing eager
    // episode restoration would cause prefetch to download disk hits again.
    const stored = await diskLoad(job.url)
    if (generation !== prefetchState.cacheSession || !canPrefetch()) return
    if (cacheHas(job.url)) return
    if (stored) {
      cacheSet(job.url, stored, job.episodeKey)
      fireListeners()
      return
    }
    const t0 = performance.now()
    ctrl = new AbortController()
    prefetchState.prefetchControllers.add(ctrl)
    timeoutID = window.setTimeout(() => ctrl?.abort(), timeout)
    const init: RequestInit = { signal: ctrl.signal }
    try { (init as any).priority = 'low' } catch { /* ignore */ }
    const resp = await fetch(job.url, init)
    if (resp.ok && generation === prefetchState.cacheSession) {
      const buf = await resp.arrayBuffer()
      if (generation !== prefetchState.cacheSession || ctrl.signal.aborted) return
      const elapsed = performance.now() - t0
      cacheSet(job.url, buf, job.episodeKey, null, elapsed)
      diskSave(job.url, buf, job.episodeKey).catch(() => { })
      recordFetchDuration(elapsed, buf.byteLength)
      updateNetworkDiagnosis()
      fireListeners()
    }
  } catch { /* 静默 */ }
  finally {
    if (timeoutID != null) window.clearTimeout(timeoutID)
    if (ctrl) prefetchState.prefetchControllers.delete(ctrl)
    prefetchState.pendingUrls.delete(pendingKey(job.url, generation))
    // Release the slot for failures as well as successes. Otherwise transient
    // errors eventually make an episode appear permanently queue-full.
    if (generation === prefetchState.cacheSession) {
      const remaining = (prefetchState.epQueueCount.get(job.episodeKey) || 0) - 1
      if (remaining > 0) prefetchState.epQueueCount.set(job.episodeKey, remaining)
      else prefetchState.epQueueCount.delete(job.episodeKey)
    }
  }
}

// ====== 预取入口 ======

export async function prefetchFirst(count: number): Promise<number> {
  if (!session.enabled || !session.currentEpKey) return 0
  const segs = episodeIndex.segmentsByEpisode.get(session.currentEpKey) || []
  if (segs.length === 0) return 0
  const want = Math.min(count, Math.min(adaptivePrefetchCount(), segs.length))
  const end = Math.min(want, segs.length)
  let added = 0
  for (let i = 0; i < end; i++) {
    if (enqueue({ url: segs[i], episodeKey: session.currentEpKey, priority: 1 })) added++
  }
  if (added > 0) scheduleDrain()
  return added
}

export async function prefetchNextEpisode(count: number): Promise<number> {
  if (!session.enabled) return 0
  const nextIdx = session.currentEpIdx + 1
  if (nextIdx < 0 || nextIdx >= session.episodes.length) return 0
  const nextEp = session.episodes[nextIdx]
  const nextEpKey = epKeyFrom(nextEp, nextIdx)
  if (!nextEpKey) return 0
  const segs = episodeIndex.segmentsByEpisode.get(nextEpKey) || []
  if (segs.length === 0) return 0
  const end = Math.min(count, segs.length)
  let added = 0
  for (let i = 0; i < end; i++) {
    if (enqueue({ url: segs[i], episodeKey: nextEpKey, priority: 2 })) added++
  }
  if (added > 0) { console.log(`${LOG_PREFIX} ⏭ 预取 #${nextIdx} 集`); scheduleDrain() }
  return added
}

/** 详情页自行 fetch m3u8 并解析 TS URL，不等播放器；使用 episodes[epIdx] 的 vod_id+ep_num 做稳定 key */
export async function prefetchFromM3u8(m3u8Url: string, epIdx: number): Promise<number> {
  if (!session.enabled) return 0
  const generation = prefetchState.cacheSession
  try {
    const ep = session.episodes[epIdx]
    if (!ep) return 0
    const epKey = epKeyFrom(ep, epIdx)

    const parsed = await fetchAndParseM3u8(m3u8Url)
    if (generation !== prefetchState.cacheSession || !session.enabled) return 0
    if (parsed.isMaster || parsed.urls.length === 0) return 0

    const segs = parsed.urls
    episodeIndex.segmentsByEpisode.set(epKey, segs)
    if (parsed.targetduration > 0 && parsed.targetduration <= 30) session.targetDuration = parsed.targetduration

    const played = episodeIndex.playedSegmentsByEpisode.get(epKey)
    const totalActual = segs.length
    const count = Math.min(adaptivePrefetchCount(), totalActual)
    console.log(`${LOG_PREFIX} 📄 解析 #${epIdx} 集: ${totalActual} 片, 预取 ${count} 片`)

    const contiguousEnd = Math.ceil(count * 0.4)
    let added = 0
    let i = 0
    while (added < contiguousEnd && i < segs.length) {
      if (played && played.has(i)) { i++; continue }
      if (enqueue({ url: segs[i], episodeKey: epKey, priority: 1 })) added++
      i++
    }
    const step = Math.max(2, adaptiveSpreadStep())
    while (added < count && i < segs.length) {
      if (!played || !played.has(i)) {
        if (enqueue({ url: segs[i], episodeKey: epKey, priority: 1 })) added++
      }
      i += step
    }
    if (added > 0) scheduleDrain()
    return added
  } catch { return 0 }
}

// 从已解析的片段 URL 直接开始预取（不需要再请求 m3u8 了）
export function prefetchFromSegments(segUrls: string[], epIdx: number, startFrom: number = 0, count: number = 20): number {
  if (!session.enabled || segUrls.length === 0) return 0
  const ep = session.episodes[epIdx]
  const epKey = epKeyFrom(ep, epIdx)
  if (!epKey) return 0
  episodeIndex.segmentsByEpisode.set(epKey, segUrls)
  if (session.currentEpIdx < 0) { session.currentEpIdx = epIdx; session.currentEpKey = epKey }
  const start = Math.max(0, startFrom)
  const end = Math.min(segUrls.length, start + Math.min(count, adaptivePrefetchCount()))
  let added = 0
  for (let i = start; i < end; i++) {
    if (enqueue({ url: segUrls[i], episodeKey: epKey, priority: 1 })) added++
  }
  if (added > 0) scheduleDrain()
  return added
}

export function notifyCurrentTs(absUrl: string): void {
  if (!session.enabled || !absUrl || !session.currentEpKey) return
  const segs = episodeIndex.segmentsByEpisode.get(session.currentEpKey) || []
  const pos = findSegmentIndex(segs, absUrl)
  if (pos < 0) return
  let played = episodeIndex.playedSegmentsByEpisode.get(session.currentEpKey)
  if (!played) { played = new Set(); episodeIndex.playedSegmentsByEpisode.set(session.currentEpKey, played) }
  for (let i = 0; i <= pos; i++) played.add(i)
  const remaining = segs.length - pos - 1
  if (remaining <= 0) return
  // Old queued positions are no longer useful after a seek. Keep only the
  // current contiguous window, ordered nearest-first, beyond HLS's own buffer.
  const start = Math.min(segs.length,pos + adaptiveBufferOffset())
  const end = Math.min(segs.length,start + adaptivePrefetchCount())
  for (let i = prefetchState.queue.length - 1; i >= 0; i--) {
    const job = prefetchState.queue[i]
    if (job.episodeKey !== session.currentEpKey) continue
    const index = findSegmentIndex(segs,job.url)
    if (index >= start && index < end) continue
    prefetchState.queue.splice(i,1)
    prefetchState.pendingUrls.delete(pendingKey(job.url,job.generation))
    const count = (prefetchState.epQueueCount.get(job.episodeKey) || 1) - 1
    if (count > 0) prefetchState.epQueueCount.set(job.episodeKey,count)
    else prefetchState.epQueueCount.delete(job.episodeKey)
  }
  for (let i = start; i < end; i++) if (!played.has(i)) enqueue({url:segs[i],episodeKey:session.currentEpKey,priority:1})
  if (canPrefetch() && remaining <= PREFETCH_TRIGGER_WHEN_LESS_THAN && session.playbackBufferAhead >= Math.min(30,remaining * session.targetDuration)) {
    void prefetchNextEpisode(PREFETCH_AHEAD_NEXT_EPISODE)
  }
  scheduleDrain()
}
