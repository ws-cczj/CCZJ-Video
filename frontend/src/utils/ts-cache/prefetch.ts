import { MAX_QUEUE_PER_EPISODE, MIN_INVENTORY, STANDARD_INVENTORY, DEFAULT_REDUNDANCY } from './constants'
import { cacheHas } from './memory-cache'
import { queueRecentSegments, setDiskPlaybackActive } from './disk-cache'
import { adaptiveConcurrency, adaptivePrefetchCount, adaptiveRedundancy } from './network'
import { fetchAndParseM3u8 } from './m3u8'
import { episodeIndex, epKeyFrom, fireListeners, onStateChange, prefetchState, session } from './store'
import type { EpisodeLite, SegmentInfo } from './store'
import { acquireSegment, cancelSpeculation, cancelTransfers, clearTransferFailures, onTransfersChanged,
  pruneTransferFailures, resetSegmentFailure, transferBusy, transferCounts, transferFailure, transferKey, transferSucceededSince } from './transfer'
import type { TransferPurpose } from './transfer'
import { proxyHlsURL } from '../../player/hls/proxy'

type Lease = ReturnType<typeof acquireSegment>
const workers = new Map<string, { lease: Lease; purpose: TransferPurpose }>()
let timer: ReturnType<typeof setTimeout> | undefined
let timerAt = Infinity
let scheduled = false
let replenishing = false
let refillSince = 0
let buffered: [number, number][] = []
let currentIndex = 0
let warmups = 0
let lastRedundancy = DEFAULT_REDUNDANCY
let redundancyAt = 0
let stallAt = 0
let skipped = new Set<string>()
let suppressed: { key: string; failures: number } | undefined
let nextWarmup = ''
let lastSnapshot = { inventory: 0, available: 0, seconds: 0, redundancy: DEFAULT_REDUNDANCY, minimum: MIN_INVENTORY, target: STANDARD_INVENTORY + DEFAULT_REDUNDANCY + 1, replenishing: false, assisting: false, pending: 0 }

function keyOf(segment: SegmentInfo): string { return transferKey(segment.url, segment.range) }
function abortError(): DOMException { return new DOMException('Cancelled', 'AbortError') }
function cancelTimer(): void { if (timer !== undefined) clearTimeout(timer); timer = undefined; timerAt = Infinity }
function wake(delay = 0): void {
  if (delay > 0) {
    const at = performance.now() + delay
    if (at < timerAt) { cancelTimer(); timerAt = at; timer = setTimeout(() => { timer = undefined; timerAt = Infinity; wake() }, delay) }
    return
  }
  if (scheduled) return
  scheduled = true
  queueMicrotask(() => { scheduled = false; pump() })
}
onTransfersChanged(() => wake())
export function activeInflight(): number { const counts = transferCounts(); return counts.prefetch + counts.helper + counts.warmup }
export function inventorySnapshot() { return { ...lastSnapshot, retainedTransferBytes: transferCounts().bytes } }
export function hasPlaybackGap(): boolean {
  if (!stallAt || session.playbackPaused || session.playbackSeeking) return false
  return episodeIndex.timeline.slice(currentIndex, currentIndex + 2).some(segment => segment.start <= session.playbackPosition + .5 && !ready(segment)
    && (transferBusy(segment.url, segment.range) || !!transferFailure(segment.url, segment.range)))
}
export function segmentRetryDelay(url?: string, range?: string | null): number {
  const segment = episodeIndex.timeline[currentIndex]
  const row = url ? transferFailure(url, range) : segment && transferFailure(segment.url, segment.range)
  return Math.max(250, Math.min(15_000, (row?.nextRetry ?? performance.now()) - performance.now()))
}

/** Session replacement invalidates pending writes before old readers settle. */
export function beginCacheSession(): void {
  prefetchState.cacheSession++
  cancelTimer(); cancelTransfers()
  for (const worker of workers.values()) worker.lease.release()
  workers.clear()
  for (const controller of prefetchState.prefetchControllers) controller.abort()
  prefetchState.prefetchControllers.clear()
  for (const cancel of prefetchState.previewRequests) cancel()
  prefetchState.queue.length = 0; prefetchState.pendingUrls.clear(); prefetchState.epQueueCount.clear()
  currentIndex = 0; replenishing = false; refillSince = 0; buffered = []; stallAt = 0
  episodeIndex.timeline = []
  session.playbackPosition = 0
  skipped = new Set(); lastRedundancy = DEFAULT_REDUNDANCY; redundancyAt = 0
  suppressed = undefined
  nextWarmup = ''
  lastSnapshot = { inventory: 0, available: 0, seconds: 0, redundancy: DEFAULT_REDUNDANCY, minimum: MIN_INVENTORY, target: STANDARD_INVENTORY + DEFAULT_REDUNDANCY + 1, replenishing: false, assisting: false, pending: 0 }
}

export function setPlaybackTimeline(segments: SegmentInfo[]): void {
  const previous = episodeIndex.timeline
  const changed = previous.length && (previous.length !== segments.length || previous.some((segment, i) => keyOf(segment) !== keyOf(segments[i]!)))
  if (changed) {
    cancelTimer(); clearTransferFailures(); skipped.clear()
    const keep = new Set(segments.map(keyOf))
    cancelSpeculation(keep)
    for (const [key, worker] of workers) if (!keep.has(key)) { worker.lease.release(); workers.delete(key) }
  }
  episodeIndex.timeline = segments.filter(s => s.url && Number.isFinite(s.start) && s.duration > 0)
  if (session.currentEpKey) episodeIndex.segmentKeysByEpisode.set(session.currentEpKey, new Set(episodeIndex.timeline.map(keyOf)))
  locate(); wake()
}
function locate(): void {
  const timeline = episodeIndex.timeline
  let low = 0, high = timeline.length
  while (low < high) {
    const mid = (low + high) >>> 1, segment = timeline[mid]!
    if (segment.start + segment.duration <= session.playbackPosition + .02) low = mid + 1
    else high = mid
  }
  currentIndex = low
}
export function updatePlaybackState(state: { position: number; rate: number; paused: boolean; seeking: boolean; waiting?: boolean; buffered: [number, number][] }): void {
  const jump = Math.abs(state.position - session.playbackPosition) > Math.max(2, session.targetDuration)
  session.playbackPosition = Math.max(0, Number.isFinite(state.position) ? state.position : 0)
  session.playbackRate = Number.isFinite(state.rate) && state.rate > 0 ? state.rate : 1
  session.playbackPaused = state.paused; session.playbackSeeking = state.seeking
  buffered = state.buffered
  if (state.waiting && !stallAt) stallAt = performance.now()
  if (!state.waiting || state.paused || state.seeking) stallAt = 0
  locate()
  if (jump || state.seeking) {
    replenishing = false; refillSince = 0
    const keep = new Set(episodeIndex.timeline.slice(currentIndex, currentIndex + MAX_QUEUE_PER_EPISODE).map(keyOf))
    cancelSpeculation(keep)
    for (const [key, worker] of workers) if (!keep.has(key)) { worker.lease.release(); workers.delete(key) }
  }
  wake()
}
export function setTransportActive(active: boolean, owner?: symbol): void {
  if (!active && owner && session.transportOwner !== owner) return
  const wasActive = session.transportActive
  if (!active && wasActive) queueRecentSegments()
  setDiskPlaybackActive(active)
  session.transportActive = active
  session.transportOwner = active ? owner ?? session.transportOwner : null
  if (!active) {
    cancelTimer(); for (const worker of workers.values()) worker.lease.release(); workers.clear()
    if (wasActive) for (const controller of prefetchState.prefetchControllers) controller.abort()
  }
  else wake()
}
export function setPlaybackBufferAhead(seconds: number, playbackRate = 1): void {
  session.playbackBufferAhead = Number.isFinite(seconds) ? Math.max(0, seconds) : 0
  session.playbackRate = playbackRate > 0 ? playbackRate : 1
  wake()
}
export function beginForegroundFetch(): () => void {
  prefetchState.foregroundActive++
  for (const cancel of prefetchState.previewRequests) cancel()
  let released = false
  return () => { if (released) return; released = true; prefetchState.foregroundActive = Math.max(0, prefetchState.foregroundActive - 1); wake(); fireListeners() }
}
export function beginPreviewFetch(cancel: () => void): (() => void) | null {
  if (prefetchState.foregroundActive > 0 || (session.transportActive && lastSnapshot.inventory < lastSnapshot.minimum + lastSnapshot.redundancy)) return null
  prefetchState.previewRequests.add(cancel)
  return () => { prefetchState.previewRequests.delete(cancel) }
}
function ready(segment: SegmentInfo): boolean {
  if (skipped.has(keyOf(segment))) return true
  if (cacheHas(segment.url, segment.range)) return true
  const start = Math.max(segment.start, session.playbackPosition)
  return buffered.some(([from, to]) => from <= start + .05 && to >= segment.start + segment.duration - .05)
}
function claim(segment: SegmentInfo, purpose: 'prefetch' | 'helper'): void {
  const key = keyOf(segment)
  if (workers.has(key) || transferBusy(segment.url, segment.range)) return
  const lease = acquireSegment({ ...segment, purpose, episodeKey: session.currentEpKey })
  const worker = { lease, purpose }
  workers.set(key, worker)
  void lease.promise.catch(() => {}).finally(() => {
    lease.release()
    if (workers.get(key) === worker) workers.delete(key)
    wake(); fireListeners()
  })
}
function pump(): void {
  if (!session.enabled || !session.transportActive || !session.currentEpKey || session.playbackSeeking) return
  const now = performance.now(), desiredRedundancy = adaptiveRedundancy()
  if (desiredRedundancy >= lastRedundancy || now - redundancyAt > 10_000) { lastRedundancy = desiredRedundancy; redundancyAt = now }
  const timeline = episodeIndex.timeline
  const target = Math.min(adaptivePrefetchCount(lastRedundancy), Math.max(0, timeline.length - currentIndex))
  const window = timeline.slice(currentIndex, currentIndex + Math.min(MAX_QUEUE_PER_EPISODE, Math.max(0, target)))
  const keep = new Set(window.map(keyOf))
  pruneTransferFailures(keep)
  let inventory = 0, available = 0, seconds = 0, pending = 0, hole = false
  for (const segment of window) {
    if (ready(segment)) { available++; if (!hole) { inventory++; seconds += segment.start + segment.duration - Math.max(session.playbackPosition, segment.start) } }
    else { hole = true; if (transferBusy(segment.url, segment.range)) pending++ }
  }
  if (!replenishing && (inventory - lastRedundancy < MIN_INVENTORY || seconds / session.playbackRate < 6) && available < target) { replenishing = true; refillSince = now }
  if (replenishing && (target === 0 || inventory > STANDARD_INVENTORY + lastRedundancy || (target > 0 && available >= target))) { replenishing = false; refillSince = 0 }
  const counts = transferCounts()
  const recentFailures = window.map(segment => transferFailure(segment.url, segment.range)).filter(row => row && row.error.kind !== 'capacity' && now - row.at < 30_000)
  const oldestFailure = Math.min(...recentFailures.map(row => row!.at))
  const broadFailure = recentFailures.length >= 3 && !window.some(segment => transferSucceededSince(segment.url, segment.range, oldestFailure))
  const assisting = !broadFailure && replenishing && !session.playbackPaused && (now - refillSince > Math.max(4000, session.targetDuration * 1000 / session.playbackRate) || (stallAt > 0 && now - stallAt > 1500))
  lastSnapshot = { inventory, available, seconds: Math.max(0, seconds), redundancy: lastRedundancy, minimum: MIN_INVENTORY, target, replenishing, assisting, pending }
  if (!replenishing) return
  for (const controller of prefetchState.prefetchControllers) controller.abort()
  let retryAt = Infinity
  const candidates = broadFailure ? window.filter(segment => !ready(segment)).slice(0, 1) : window
  for (const segment of candidates) {
    if (ready(segment) || transferBusy(segment.url, segment.range) || segment.demandOnly) continue
    const failure = transferFailure(segment.url, segment.range)
    if (failure && session.playbackPaused) continue
    if (failure?.error.kind === 'capacity') continue
    if (failure && failure.nextRetry > now) { retryAt = Math.min(retryAt, failure.nextRetry); continue }
    const active = transferCounts()
    if (!broadFailure && failure && segment.start <= session.playbackPosition + session.targetDuration * 2 && !session.playbackPaused && active.helper === 0 && active.foreground === 0) claim(segment, 'helper')
    else if (active.prefetch < (broadFailure ? 1 : adaptiveConcurrency())) claim(segment, 'prefetch')
    else if (assisting && active.helper === 0 && active.foreground === 0) claim(segment, 'helper')
    else break
  }
  if (retryAt < Infinity) wake(Math.max(50, retryAt - now))
  if (!session.playbackPaused && !assisting) wake(Math.max(100, Math.max(4000, session.targetDuration * 1000 / session.playbackRate) - (now - refillSince)))
  if (counts.prefetch !== transferCounts().prefetch || counts.helper !== transferCounts().helper) fireListeners()
}

export interface BlockedSegment { key: string; start: number; end: number; next: number; seconds: number; retries: number }
export function blockedSegment(): BlockedSegment | null {
  if (!session.transportActive || !stallAt || session.playbackPaused || session.playbackSeeking) return null
  const now = performance.now(), timeline = episodeIndex.timeline
  for (let i = currentIndex; i < Math.min(timeline.length - 1, currentIndex + 2); i++) {
    const segment = timeline[i]!, failure = transferFailure(segment.url, segment.range)
    if (!failure || failure.failures < 6 || failure.error.kind === 'capacity' || failure.error.code === 401 || failure.error.code === 403 || now - failure.at > 60_000 || segment.start > session.playbackPosition + .5 || ready(segment)) continue
    if (suppressed?.key === keyOf(segment) && failure.failures <= suppressed.failures) continue
    const next = timeline[i + 1]!, after = timeline[i + 2]
    if (!cacheHas(next.url, next.range) || transferFailure(next.url, next.range) || (after && !cacheHas(after.url, after.range))) continue
    if (failure.firstPosition >= segment.start + segment.duration || !transferSucceededSince(next.url, next.range, Math.max(failure.firstAt, now - 60_000))) continue
    return { key: keyOf(segment), start: segment.start, end: segment.start + segment.duration, next: next.start, seconds: next.start - session.playbackPosition, retries: failure.failures - 1 }
  }
  return null
}
export function retryBlockedSegment(): void {
  const blocked = blockedSegment()
  const segment = blocked ? episodeIndex.timeline.slice(currentIndex, currentIndex + 2).find(segment => keyOf(segment) === blocked.key) : episodeIndex.timeline[currentIndex]
  if (segment) {
    suppressed = { key: keyOf(segment), failures: transferFailure(segment.url, segment.range)?.failures ?? 0 }
    resetSegmentFailure(segment.url, segment.range)
  }
  wake()
}
export function acceptSkippedSegment(key: string): boolean {
  const blocked = blockedSegment()
  if (!blocked || blocked.key !== key) return false
  skipped.add(key); stallAt = 0
  workers.get(key)?.lease.release(); workers.delete(key)
  wake(); return true
}
export function notifyCurrentTs(url: string): void {
  if (!url) return
  locate()
  if (session.currentEpKey && currentIndex < episodeIndex.timeline.length) {
    let played = episodeIndex.playedSegmentsByEpisode.get(session.currentEpKey)
    if (!played) { played = new Set(); episodeIndex.playedSegmentsByEpisode.set(session.currentEpKey, played) }
    played.add(currentIndex)
  }
  wake()
  const next = session.episodes[session.currentEpIdx + 1]
  const nextKey = next ? epKeyFrom(next, session.currentEpIdx + 1) : ''
  if (nextKey && nextWarmup !== nextKey && !lastSnapshot.replenishing && episodeIndex.timeline.length - currentIndex <= MIN_INVENTORY) {
    nextWarmup = nextKey
    void prefetchNextEpisode(2).catch(() => { if (nextWarmup === nextKey) nextWarmup = '' })
  }
}
export function prefetchFromSegments(urls: string[], epIdx: number, startFrom = 0, _count = STANDARD_INVENTORY): number {
  const ep = session.episodes[epIdx], key = epKeyFrom(ep, epIdx)
  if (!key) return 0
  episodeIndex.segmentsByEpisode.set(key, urls)
  episodeIndex.segmentKeysByEpisode.set(key, new Set(urls.map(url => transferKey(url))))
  if (!session.currentEpKey) { session.currentEpKey = key; session.currentEpIdx = epIdx }
  if (session.currentEpKey === key && !episodeIndex.timeline.length) {
    setPlaybackTimeline(urls.map((url, i) => ({ url, start: i * session.targetDuration, duration: session.targetDuration })))
    currentIndex = Math.max(0, startFrom)
  }
  wake(); return Math.min(urls.length, adaptivePrefetchCount())
}
export async function prefetchFromM3u8(url: string, epIdx: number): Promise<number> {
  const generation = prefetchState.cacheSession
  try {
    const parsed = await fetchAndParseM3u8(url)
    if (generation !== prefetchState.cacheSession || parsed.isMaster) return 0
    return prefetchFromSegments(parsed.urls, epIdx)
  } catch { return 0 }
}
export async function prefetchFirst(_count: number): Promise<number> { wake(); return lastSnapshot.target }
export async function prefetchNextEpisode(count: number): Promise<number> {
  const next = session.episodes[session.currentEpIdx + 1]
  if (!next || lastSnapshot.replenishing) return 0
  const controller = new AbortController()
  prefetchState.prefetchControllers.add(controller)
  try { return Math.min(count, await prefetchMediaStart(next, controller.signal)) }
  finally { prefetchState.prefetchControllers.delete(controller); wake() }
}

/** Warm only the next item; current inventory and playback retain precedence. */
export async function prefetchMediaStart(episode: EpisodeLite, signal: AbortSignal, idle = false): Promise<number> {
  if (!/\.m3u8(?:$|[?#])/i.test(episode.ep_url)) return 0
  const eligible = () => !signal.aborted && warmups === 0 && transferCounts().foreground === 0 && (idle ? !session.transportActive : session.transportActive && !lastSnapshot.replenishing && lastSnapshot.inventory >= MIN_INVENTORY + lastSnapshot.redundancy)
  if (!eligible()) await new Promise<void>((resolve, reject) => {
    const done = () => { off(); signal.removeEventListener('abort', abort); resolve() }
    const abort = () => { off(); signal.removeEventListener('abort', abort); reject(abortError()) }
    const off = onStateChange(() => { if (eligible()) done() })
    signal.addEventListener('abort', abort, { once: true })
    if (signal.aborted) abort(); else if (eligible()) done()
  })
  if (signal.aborted) return 0
  warmups++
  const controller = new AbortController(), abort = () => controller.abort(), generation = prefetchState.cacheSession
  const timeout = setTimeout(abort, 12_000)
  signal.addEventListener('abort', abort, { once: true }); prefetchState.prefetchControllers.add(controller)
  let warmed = 0, bytes = 0
  try {
    let url = new URL(proxyHlsURL(episode.ep_url), location.href).href, parsed = await fetchAndParseM3u8(url, controller.signal)
    for (let depth = 0; parsed.isMaster && depth < 3; depth++) {
      const variant = parsed.streamInfo.slice().sort((a, b) => a.bandwidth - b.bandwidth)[0]?.url || parsed.variantUrls[0]
      if (!variant) return 0
      url = variant; parsed = await fetchAndParseM3u8(url, controller.signal)
    }
    if (parsed.isMaster || /#EXT-X-BYTERANGE|#EXT-X-MAP:[^\n]*BYTERANGE/.test(parsed.text)) return 0
    const map = parsed.text.match(/#EXT-X-MAP:[^\n]*URI="([^"]+)"/)
    for (const segment of [...(map ? [new URL(map[1]!, url).href] : []), ...parsed.urls.slice(0, 2)]) {
      if (controller.signal.aborted || generation !== prefetchState.cacheSession) break
      if (session.transportActive && lastSnapshot.replenishing) break
      const lease = acquireSegment({ url: segment, purpose: 'warmup', episodeKey: epKeyFrom(episode), maxBytes: 8 * 1024 * 1024 - bytes, timeoutMs: 12_000 })
      const stop = () => lease.release()
      controller.signal.addEventListener('abort', stop, { once: true })
      try { const result = await lease.promise; bytes += result.buffer.byteLength; warmed++ }
      finally { controller.signal.removeEventListener('abort', stop); lease.release() }
    }
    return warmed
  } finally {
    clearTimeout(timeout); signal.removeEventListener('abort', abort); prefetchState.prefetchControllers.delete(controller)
    warmups--; fireListeners()
  }
}

if (import.meta.hot) import.meta.hot.dispose(() => { cancelTimer(); beginCacheSession() })
