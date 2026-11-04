import { MAX_PREFETCH_COUNT, DEFAULT_REDUNDANCY, MIN_REDUNDANCY, MAX_REDUNDANCY, MAX_CACHED_BYTES, PREFETCH_CONCURRENCY, STANDARD_INVENTORY } from './constants'
import { cache, netDiag, session, telemetry } from './store'
import type { NetworkMode } from './store'

/** Transfer time is compared with playable duration. A large 10-second segment
 * taking 3 seconds is healthy; a 1-second segment taking 3 seconds is not.
 * These observations cannot identify whether a bottleneck is local or remote.
 */
export function updateNetworkDiagnosis(): void {
  const durations = telemetry.recentFetchDurations
  if (!durations.length) return
  const ms = durations.reduce((a,b) => a+b,0)
  const bytes = telemetry.recentFetchBytes.reduce((a,b) => a+b,0)
  const seconds = telemetry.recentMediaSeconds.reduce((a,b) => a+b,0)
  netDiag.downloadAvgMs = ms / durations.length
  const intervals = telemetry.recentFetchStarts.map((start, i) => [start, telemetry.recentFetchEnds[i]!]).sort((a, b) => a[0]! - b[0]!)
  let activeMs = 0, end = -Infinity
  for (const interval of intervals) { activeMs += Math.max(0, interval[1]! - Math.max(end, interval[0]!)); end = Math.max(end, interval[1]!) }
  // Concurrent requests share a wall-clock interval; summing their individual
  // durations would treat each connection as if it owned the full link.
  netDiag.throughputBps = bytes * 8000 / Math.max(1, activeMs || ms)
  netDiag.downloadRatio = bytes * 8 / Math.max(1, seconds * netDiag.throughputBps)
  const ratio = netDiag.downloadRatio * session.playbackRate
  netDiag.failureCount = 0
  if (ratio >= .8) {
    netDiag.recoveryStreak = 0
    netDiag.consecutiveSlowSegs++
    if (ratio >= 1 || netDiag.consecutiveSlowSegs >= 2) netDiag.mode = 'bandwidth_limited'
  } else {
    netDiag.consecutiveSlowSegs = 0
    if (ratio < .55) netDiag.recoveryStreak++
    else netDiag.recoveryStreak = 0
    // Several good transfers are required to resume speculation after trouble.
    if (netDiag.recoveryStreak >= 4) netDiag.mode = 'normal'
  }
}

export function recordNetworkFailure(): void {
  netDiag.failureCount++
  netDiag.recoveryStreak = 0
  netDiag.mode = 'unstable'
}

export function getNetworkMode(): NetworkMode { return netDiag.mode }

export function resetNetworkDiagnosis(): void {
  netDiag.mode = 'normal'
  netDiag.downloadAvgMs = netDiag.downloadVariance = netDiag.slowFetchCount = 0
  netDiag.fastFetchStreak = netDiag.consecutiveSlowSegs = 0
  netDiag.throughputBps = netDiag.downloadRatio = netDiag.recoveryStreak = netDiag.failureCount = 0
}

export function adaptivePrefetchCount(redundancy = adaptiveRedundancy()): number {
  const sizes = telemetry.recentFetchBytes.filter(bytes => Number.isFinite(bytes) && bytes > 0)
  let largest = Math.max(0, ...sizes)
  const recentKeys = [...(cache.epCacheKeys.get(session.currentEpKey) ?? [])].slice(-8)
  for (const key of recentKeys) largest = Math.max(largest, cache.lruCache.get(key)?.size ?? 0)
  // Reserve a quarter of retained RAM for rewind/other entries. Conservatively
  // bound future fragments by recent network and retained binary sizes. Disk
  // hits must also constrain a restored session with no fresh network sample.
  const byteLimit = largest > 0 ? Math.max(1, Math.floor(MAX_CACHED_BYTES * .75 / largest)) : MAX_PREFETCH_COUNT
  return Math.min(MAX_PREFETCH_COUNT, STANDARD_INVENTORY + redundancy + 1, byteLimit)
}

export function adaptiveRedundancy(): number {
  const durations = telemetry.recentFetchDurations
  if (!durations.length) return DEFAULT_REDUNDANCY
  const sorted = durations.slice().sort((a, b) => a - b)
  const p90 = sorted[Math.min(sorted.length - 1, Math.floor(sorted.length * .9))]!
  const meanBytes = telemetry.recentFetchBytes.reduce((sum, bytes) => sum + bytes, 0) / durations.length
  const predictedMs = netDiag.throughputBps > 0 ? meanBytes * 8000 * PREFETCH_CONCURRENCY / (netDiag.throughputBps * .8) : p90
  const typical = Math.max(1, session.targetDuration)
  // Cover a conservative refill plus jitter in wall time, converted to media
  // consumption. The bounded window remains attainable on a constrained link.
  return Math.max(MIN_REDUNDANCY, Math.min(MAX_REDUNDANCY, Math.ceil((Math.max(p90, predictedMs) / 1000 * 1.5 + 1) * session.playbackRate / typical)))
}

// Speculation starts past the contiguous HLS buffer, never at far-away sparse
// segments while the next playable fragment is missing.
export function adaptiveBufferOffset(): number { return Math.max(1,Math.floor(session.playbackBufferAhead / Math.max(1,session.targetDuration))) }
export function adaptiveSpreadStep(): number { return 1 }

export function adaptiveConcurrency(): number {
  return PREFETCH_CONCURRENCY
}

export function adaptiveFragmentTimeout(): number {
  return Math.min(60_000, Math.max(15_000, session.targetDuration * 1000 * Math.max(3,netDiag.downloadRatio * 3)))
}

export function adaptiveForwardBuffer(): number { return Math.max(6, Math.min(30, session.targetDuration * 2)) }
