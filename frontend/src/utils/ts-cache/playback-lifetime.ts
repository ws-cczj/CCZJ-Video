import { MAX_CACHED_BYTES, MAX_CACHED_SEGMENTS } from './constants'
import { setMemoryCacheBudget } from './memory-cache'
import { clearM3u8TextCache } from './m3u8'
import { beginCacheSession } from './prefetch'
import { episodeIndex, fireListeners, netDiag, session, telemetry } from './store'

const IDLE_GRACE_MS = 30_000
const IDLE_MAX_BYTES = 8 * 1024 * 1024
const IDLE_MAX_ENTRIES = 16
const owners = new Set<symbol>()
let idleTimer: ReturnType<typeof setTimeout> | null = null
let idle = false

/** Each mounted player owns one release handle; URL/quality changes keep it. */
export function retainPlayback(): () => void {
  const owner = Symbol('playback-cache')
  owners.add(owner)
  idle = false
  session.enabled = true
  setMemoryCacheBudget(MAX_CACHED_BYTES, MAX_CACHED_SEGMENTS)
  if (idleTimer !== null) clearTimeout(idleTimer)
  idleTimer = null
  return () => {
    if (!owners.delete(owner) || owners.size > 0) return
    // Cancel jobs immediately, but give a quick reopen the full warm cache.
    beginCacheSession()
    session.enabled = false
    session.playbackBufferAhead = 0
    session.episodes = []
    session.currentEpIdx = -1
    session.currentEpKey = ''
    session.currentVodId = session.currentSourceKey = ''
    episodeIndex.segmentsByEpisode.clear()
    episodeIndex.playedSegmentsByEpisode.clear()
    telemetry.epStats.clear()
    telemetry.curEpStats = { hits: 0, misses: 0 }
    netDiag.abrSwitchCallback = null
    idleTimer = setTimeout(() => {
      idleTimer = null
      if (owners.size > 0) return
      idle = true
      setMemoryCacheBudget(IDLE_MAX_BYTES, IDLE_MAX_ENTRIES)
      clearM3u8TextCache()
      fireListeners()
    }, IDLE_GRACE_MS)
    fireListeners()
  }
}

export function playbackCacheStatus() {
  return {
    activePlayers: owners.size,
    idleTrimPending: idleTimer !== null,
    memoryLimitBytes: idle ? IDLE_MAX_BYTES : MAX_CACHED_BYTES,
  }
}
