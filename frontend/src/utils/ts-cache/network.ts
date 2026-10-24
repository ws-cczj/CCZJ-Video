import { DEFAULT_PREFETCH_SECONDS, MAX_PREFETCH_COUNT, MIN_PREFETCH_COUNT } from './constants'
import { netDiag, session, telemetry } from './store'
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
  netDiag.throughputBps = bytes * 8000 / ms
  netDiag.downloadRatio = ms / Math.max(1,seconds * 1000)
  const last = durations.length - 1
  const ratio = durations[last] / Math.max(1,telemetry.recentMediaSeconds[last] * 1000) * session.playbackRate
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

export function adaptivePrefetchCount(): number {
  const seconds = netDiag.mode === 'normal' ? DEFAULT_PREFETCH_SECONDS : 20
  return Math.max(MIN_PREFETCH_COUNT, Math.min(MAX_PREFETCH_COUNT, Math.ceil(seconds / Math.max(1,session.targetDuration))))
}

// Speculation starts past the contiguous HLS buffer, never at far-away sparse
// segments while the next playable fragment is missing.
export function adaptiveBufferOffset(): number { return Math.max(1,Math.floor(session.playbackBufferAhead / Math.max(1,session.targetDuration))) }
export function adaptiveSpreadStep(): number { return 1 }

/** One spare connection only when observed capacity has comfortable headroom. */
export function adaptiveConcurrency(): number {
  if (netDiag.mode !== 'normal') return 0
  if (telemetry.recentFetchDurations.length >= 2 && netDiag.downloadRatio * session.playbackRate >= .55) return 0
  return 1
}

export function adaptiveFragmentTimeout(): number {
  return Math.min(60_000, Math.max(15_000, session.targetDuration * 1000 * Math.max(3,netDiag.downloadRatio * 3)))
}

export function adaptiveForwardBuffer(): number { return netDiag.mode === 'normal' ? 30 : 60 }
