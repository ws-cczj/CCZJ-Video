/* eslint-disable no-console */
/**
 * 预取 + 取回链路：排队、去重、并发/交错、超时与 abort、对冲请求。
 *
 * 会话号（cacheSession）是这里的生命线：切集 / 换视频 / 失效都会 beginCacheSession()
 * 把在途请求全部 abort 并作废其结果，回写的每一处都要重新核对 generation，
 * 否则旧集的字节会污染刚清空的缓存。
 */
import {
  HEDGE_STAGGER_MS,
  LOG_PREFIX,
  MAX_QUEUE_PER_EPISODE,
  PREFETCH_AHEAD_NEXT_EPISODE,
  PREFETCH_DEBOUNCE_MS,
  PREFETCH_TRIGGER_WHEN_LESS_THAN,
} from './constants'
import { diskSave } from './disk-cache'
import { cacheHas, cacheSet } from './memory-cache'
import {
  adaptiveConcurrency,
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
  netDiag,
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

function hedgeKey(url: string, generation = prefetchState.cacheSession): string {
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
  if (scopedJob.priority === 1) prefetchState.queue.unshift(scopedJob); else prefetchState.queue.push(scopedJob)
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
    // ⭐ 交错启动：每个分片间隔 ~1s，避免服务器敏感封禁
    if (active > 1) {
      const staggerDelay = (active - 1) * 1000
      window.setTimeout(() => {
        runOne(job).finally(() => {
          finishInflight(generation)
          if (generation === prefetchState.cacheSession) drainQueue()
        })
      }, staggerDelay)
    } else {
      runOne(job).finally(() => {
        finishInflight(generation)
        if (generation === prefetchState.cacheSession) drainQueue()
      })
    }
  }
}

function canPrefetch(): boolean {
  return session.enabled && session.playbackBufferAhead >= Math.max(12, session.targetDuration * 2)
}

/**
 * 播放器上报"前方还有多少秒缓冲"。它决定后台预取能不能开工，
 * 所以由预取层自己持有：够就立刻尝试排空队列。
 */
export function setPlaybackBufferAhead(seconds: number): void {
  session.playbackBufferAhead = Number.isFinite(seconds) ? Math.max(0, seconds) : 0
  if (canPrefetch()) scheduleDrain()
}

async function runOne(job: FetchJob): Promise<void> {
  const generation = job.generation ?? prefetchState.cacheSession
  if (generation !== prefetchState.cacheSession) return
  const t0 = performance.now()
  const timeout = adaptiveFragmentTimeout()
  let ctrl: AbortController | null = null
  let timeoutID: number | null = null
  try {
    ctrl = new AbortController()
    prefetchState.prefetchControllers.add(ctrl)
    timeoutID = window.setTimeout(() => ctrl?.abort(), timeout)
    const init: RequestInit = { signal: ctrl.signal }
    try { (init as any).priority = 'low' } catch { /* ignore */ }
    const resp = await fetch(job.url, init)
    if (timeoutID != null) window.clearTimeout(timeoutID)
    if (resp.ok && generation === prefetchState.cacheSession) {
      const buf = await resp.arrayBuffer()
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

// ====== 对冲请求（Hedge Fetch）======
//
// 对紧邻播放位置的关键分片开两个并发请求（同一文件），取最快返回的，另一个 abort。
// 能大幅降低尾部延迟，避免单个慢请求拖住整个播放流水线。
// 第二个请求延迟 HEDGE_STAGGER_MS 后发射，避免同时冲击服务器。

export function hedgeFetch(url: string): Promise<ArrayBuffer | null> {
  const ctrl1 = new AbortController()
  const ctrl2 = new AbortController()
  const sessionCtrl = new AbortController()
  prefetchState.prefetchControllers.add(sessionCtrl)
  const timeout = adaptiveFragmentTimeout()
  let settleRequest: ((value: ArrayBuffer | null) => void) | null = null
  let staggerTimer: number | null = null
  let timeoutTimer: number | null = null

  const doFetch = (signal: AbortSignal) => {
    const init: RequestInit = { signal }
    return fetch(url, init)
  }

  const racePromise = new Promise<ArrayBuffer | null>((resolve) => {
    let settled = false
    const settle = (value: ArrayBuffer | null) => {
      if (!settled) { settled = true; resolve(value) }
    }
    settleRequest = settle

    // 请求1：立即发射
    doFetch(ctrl1.signal)
      .then((r) => r.ok ? r.arrayBuffer() : Promise.reject(new Error('http ' + r.status)))
      .then((buf) => { settle(buf); try { ctrl2.abort() } catch { } })
      .catch(() => { /* let the delayed hedge try the same segment */ })

    // 请求2：延迟发射（错开避免服务器压力）
    staggerTimer = window.setTimeout(() => {
      if (settled || ctrl2.signal.aborted) return
      doFetch(ctrl2.signal)
        .then((r) => r.ok ? r.arrayBuffer() : Promise.reject(new Error('http ' + r.status)))
        .then((buf) => { settle(buf); try { ctrl1.abort() } catch { } })
        .catch(() => { if (!settled && !ctrl2.signal.aborted) settle(null) })
    }, HEDGE_STAGGER_MS)
  })

  sessionCtrl.signal.addEventListener('abort', () => {
    try { ctrl1.abort() } catch { }
    try { ctrl2.abort() } catch { }
    settleRequest?.(null)
  }, { once: true })

  // 全局超时保护
  return Promise.race([
    racePromise,
    new Promise<ArrayBuffer | null>((resolve) => {
      timeoutTimer = window.setTimeout(() => {
        sessionCtrl.abort()
        resolve(null)
      }, timeout)
    }),
  ]).finally(() => {
    if (staggerTimer != null) window.clearTimeout(staggerTimer)
    if (timeoutTimer != null) window.clearTimeout(timeoutTimer)
    prefetchState.prefetchControllers.delete(sessionCtrl)
  })
}

// ====== 预取入口 ======

export async function prefetchFirst(count: number): Promise<number> {
  if (!session.enabled || !session.currentEpKey) return 0
  const segs = episodeIndex.segmentsByEpisode.get(session.currentEpKey) || []
  if (segs.length === 0) return 0
  const want = Math.min(count, Math.min(adaptivePrefetchCount(), segs.length))
  const end = Math.min(want, segs.length)
  let added = 0
  for (let i = end - 1; i >= 0; i--) {
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
  for (let i = end - 1; i >= 0; i--) {
    if (enqueue({ url: segs[i], episodeKey: nextEpKey, priority: 2 })) added++
  }
  if (added > 0) { console.log(`${LOG_PREFIX} ⏭ 预取 #${nextIdx} 集`); scheduleDrain() }
  return added
}

/** 详情页自行 fetch m3u8 并解析 TS URL，不等播放器；使用 episodes[epIdx] 的 vod_id+ep_num 做稳定 key */
export async function prefetchFromM3u8(m3u8Url: string, epIdx: number): Promise<number> {
  if (!session.enabled) return 0
  try {
    const ep = session.episodes[epIdx]
    if (!ep) return 0
    const epKey = epKeyFrom(ep, epIdx)

    const parsed = await fetchAndParseM3u8(m3u8Url)
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
  for (let i = end - 1; i >= start; i--) {
    if (enqueue({ url: segUrls[i], episodeKey: epKey, priority: 1 })) added++
  }
  if (added > 0) scheduleDrain()
  return added
}

export function notifyCurrentTs(absUrl: string): void {
  if (!session.enabled || !absUrl || !session.currentEpKey) return
  const segs = episodeIndex.segmentsByEpisode.get(session.currentEpKey) || []
  if (segs.length === 0) return
  const pos = findSegmentIndex(segs, absUrl)
  if (pos < 0) return

  // 1. 标记当前及之前的片段为"已播放"
  let playedSet = episodeIndex.playedSegmentsByEpisode.get(session.currentEpKey)
  if (!playedSet) { playedSet = new Set<number>(); episodeIndex.playedSegmentsByEpisode.set(session.currentEpKey, playedSet) }
  for (let i = 0; i <= pos; i++) playedSet.add(i)

  const totalActual = segs.length
  const remaining = totalActual - pos - 1
  if (remaining <= 0) return

  // 2. 三级区域权重预取
  //   紧急区 (pos+1 ~ pos+2): 对冲请求，双并发取最快
  //   温热区 (pos+3 ~ pos+15): 密集预取，高优先
  //   冷区   (pos+16+):        稀疏预取，广覆盖
  const totalCount = Math.min(adaptivePrefetchCount(), remaining)
  const spreadStep = adaptiveSpreadStep()
  let addedCount = 0

  // --- 紧急区：对冲请求（仅拥堵时启用）---
  if (netDiag.mode === 'server_congested') {
    let hedgeAdded = 0
    for (let offset = 1; offset <= 2 && pos + offset < segs.length; offset++) {
      const idx = pos + offset
      if (playedSet.has(idx)) continue
      const u = segs[idx]
      if (cacheHas(u) || prefetchState.pendingUrls.has(pendingKey(u)) || netDiag.hedgeInFlight.has(hedgeKey(u))) continue
      // 对冲：双请求取最快
      hedgeAdded++
      addedCount++
      netDiag.hedgeInFlight.add(hedgeKey(u))
      const t0 = performance.now()
      const generation = prefetchState.cacheSession
      const episode = session.currentEpKey
      hedgeFetch(u).then((buf) => {
        if (buf && generation === prefetchState.cacheSession) {
          const elapsed = performance.now() - t0
          cacheSet(u, buf, episode, null, elapsed)
          diskSave(u, buf, episode).catch(() => { })
          recordFetchDuration(elapsed, buf.byteLength)
          fireListeners()
        }
      }).finally(() => { netDiag.hedgeInFlight.delete(hedgeKey(u, generation)) })
    }
    if (hedgeAdded > 0) {
      console.log(`${LOG_PREFIX} 🚨 紧急对冲: ${hedgeAdded} 片 (pos=${pos}, mode=${netDiag.mode})`)
    }
  }

  // --- 温热区：密集预取 (pos+3 ~ pos+15) ---
  const warmStart = 3
  const warmEnd = Math.min(15, remaining)
  for (let offset = warmStart; offset <= warmEnd && addedCount < totalCount; offset++) {
    const idx = pos + offset
    if (idx >= segs.length) break
    if (playedSet.has(idx)) continue
    const u = segs[idx]
    if (cacheHas(u) || prefetchState.pendingUrls.has(pendingKey(u)) || netDiag.hedgeInFlight.has(hedgeKey(u))) continue
    if (enqueue({ url: u, episodeKey: session.currentEpKey, priority: 1 })) addedCount++
  }

  // --- 冷区：稀疏预取 (pos+16+, 按 spreadStep 间距) ---
  const coldStart = Math.max(16, warmEnd + 1)
  for (let offset = coldStart; addedCount < totalCount && pos + offset < segs.length; offset += spreadStep) {
    const idx = pos + offset
    if (playedSet.has(idx)) continue
    const u = segs[idx]
    if (cacheHas(u) || prefetchState.pendingUrls.has(pendingKey(u)) || netDiag.hedgeInFlight.has(hedgeKey(u))) continue
    if (enqueue({ url: u, episodeKey: session.currentEpKey, priority: 2 })) addedCount++
  }

  if (pos % 5 === 0) {
    const h = telemetry.curEpStats.hits, m = telemetry.curEpStats.misses
    const total = h + m
    const rate = total === 0 ? 0 : h / total
    console.log(
      `${LOG_PREFIX} pos=${pos}/${totalActual}, prefetch=${addedCount}, ` +
      `命中率=${(rate * 100).toFixed(1)}%, mode=${netDiag.mode}, conc=${adaptiveConcurrency()}`
    )
  }

  // 3. 下一集预取：进度到 30% 或剩余 ≤12 片时触发
  if (session.currentEpIdx + 1 < session.episodes.length && (pos / Math.max(totalActual, 1) >= 0.3 || remaining <= PREFETCH_TRIGGER_WHEN_LESS_THAN)) {
    const nextEp = session.episodes[session.currentEpIdx + 1]
    if (nextEp) {
      const nextEpKey = epKeyFrom(nextEp, session.currentEpIdx + 1)
      const nextSegs = episodeIndex.segmentsByEpisode.get(nextEpKey) || []
      if (nextSegs.length > 0) {
        const nextPlayed = episodeIndex.playedSegmentsByEpisode.get(nextEpKey)
        let nextAdded = 0
        const nextCount = Math.min(PREFETCH_AHEAD_NEXT_EPISODE, nextSegs.length)
        for (let i = 0; i < nextSegs.length && nextAdded < nextCount; i++) {
          if (nextPlayed && nextPlayed.has(i)) continue
          if (cacheHas(nextSegs[i]) || prefetchState.pendingUrls.has(pendingKey(nextSegs[i]))) continue
          if (enqueue({ url: nextSegs[i], episodeKey: nextEpKey, priority: 2 })) nextAdded++
        }
        if (nextAdded > 0) console.log(`${LOG_PREFIX} ⏭ 自动预取 #${session.currentEpIdx + 1} 集 (${nextAdded} 片)`)
      }
    }
  }

  scheduleDrain()
}
