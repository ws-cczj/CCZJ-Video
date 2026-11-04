import { SegmentDiskCache } from '../tsCacheDisk'
import { MAX_CACHED_BYTES } from './constants'
import { segmentCacheKey } from './segment-key'
import { cache, episodeIndex, prefetchState, session } from './store'
import type { DiskCacheInfo } from '../tsCacheDisk'

const MAX_DISK_BYTES = 256 * 1024 * 1024
const DISK_TTL_MS = 7 * 24 * 60 * 60 * 1000
const DISK_READ_TIMEOUT_MS = 250
const MAX_RECENT_SEGMENTS = 32
const MAX_COMMIT_BYTES = MAX_CACHED_BYTES / 2
export const diskCache = new SegmentDiskCache(MAX_DISK_BYTES, DISK_TTL_MS)
const pending = new Map<string, string>()
const touches = new Map<string, number>()
let playing = false
let generation = 0
let flushing = false
let flushRequested = false
let pendingWriteBytes = 0, pendingWrites = 0
let skippedWrites = 0, failedWrites = 0, persistedWrites = 0

export function diskWriteStats() {
  return { pendingDiskWrites: pendingWrites, pendingDiskWriteBytes: pendingWriteBytes,
    skippedDiskWrites: skippedWrites, failedDiskWrites: failedWrites, persistedDiskWrites: persistedWrites }
}

/** Pause automatic writes/deletions synchronously before a new transport starts. */
export function setDiskPlaybackActive(active: boolean): void {
  playing = active
  diskCache.setMaintenancePaused(active)
  if (!active) void flushRecent()
}

/** Keep keys only: eviction from the existing memory LRU may discard a candidate. */
export function queueRecentSegments(): void {
  if (!session.currentEpKey) return
  const distances = new Map(episodeIndex.timeline.map(segment => [
    segmentCacheKey(segment.url, segment.range),
    Math.abs(segment.start - session.playbackPosition),
  ]))
  const entries = [...cache.lruCache.values()]
    .filter(entry => entry.episodeKey === session.currentEpKey && entry.size === entry.buffer.byteLength)
    .sort((a, b) => (distances.get(a.cacheKey) ?? Infinity) - (distances.get(b.cacheKey) ?? Infinity) || b.ts - a.ts)
  let bytes = 0, count = 0
  // Insert farthest first so the next batch keeps the nearest entries.
  const selected = entries.filter(entry => {
    if (count >= MAX_RECENT_SEGMENTS || bytes + entry.size > MAX_COMMIT_BYTES) return false
    bytes += entry.size; count++; return true
  })
  for (const entry of selected.reverse()) {
    pending.delete(entry.cacheKey)
    pending.set(entry.cacheKey, entry.episodeKey)
  }
  while (pending.size > MAX_RECENT_SEGMENTS) {
    pending.delete(pending.keys().next().value!)
    skippedWrites++
  }
  if (!playing) void flushRecent()
}

function quotaError(error: unknown): boolean {
  return error instanceof DOMException && error.name === 'QuotaExceededError'
}

async function flushRecent(): Promise<void> {
  if (playing) return
  if (flushing) { flushRequested = true; return }
  flushing = true
  flushRequested = false
  const epoch = generation
  const current = () => !playing && epoch === generation
  try {
    const accessed = new Map(touches)
    if (accessed.size) {
      await diskCache.touch(accessed)
      if (!current()) return
      for (const [key, at] of accessed) if (touches.get(key) === at) touches.delete(key)
    }
    let entries = [...pending.keys()].reverse().map(key => cache.lruCache.get(key))
      .filter(entry => entry && entry.size > 0 && entry.buffer.byteLength === entry.size)
    // Reserve space before writing, counting replacement records only once.
    let bytes = 0
    entries = entries.filter(entry => {
      if (!entry || bytes + entry.size > MAX_COMMIT_BYTES) return false
      bytes += entry.size; return true
    })
    if (!current()) return
    await diskCache.prune(MAX_DISK_BYTES - bytes, new Set(entries.map(entry => entry!.cacheKey)))
    // Drop references between transactions; new playback can abort maintenance.
    const keys = entries.map(entry => entry!.cacheKey)
    entries = []
    for (const key of keys) {
      if (!current()) return
      const entry = cache.lruCache.get(key)
      if (!entry || entry.size !== entry.buffer.byteLength) { pending.delete(key); skippedWrites++; continue }
      pendingWrites = 1; pendingWriteBytes = entry.size
      try {
        let saved: boolean
        try { saved = await diskCache.save(key, entry.buffer, entry.episodeKey) }
        catch (error) {
          if (!current() || !quotaError(error)) throw error
          const info = await diskCache.info()
          if (!current()) return
          await diskCache.prune(Math.max(0, info.bytes - Math.max(16 * 1024 * 1024, entry.size)))
          if (!current()) return
          saved = await diskCache.save(key, entry.buffer, entry.episodeKey)
        }
        if (!current()) return
        if (saved) { pending.delete(key); persistedWrites++ }
      } catch {
        if (!current()) return
        failedWrites++; pending.delete(key)
        // A full or unavailable database must not trigger repeated eviction.
        break
      } finally { pendingWrites = 0; pendingWriteBytes = 0 }
    }
    if (current()) {
      for (const key of pending.keys()) if (!cache.lruCache.has(key)) { pending.delete(key); skippedWrites++ }
      await diskCache.prune()
    }
  } catch { if (current()) failedWrites++ }
  finally {
    flushing = false
    if (flushRequested && !playing) { flushRequested = false; void flushRecent() }
  }
}

export function cancelDiskCommit(): void {
  generation++
  flushRequested = false
  pending.clear()
  touches.clear()
  diskCache.setMaintenancePaused(true)
  diskCache.setMaintenancePaused(playing)
}

/** A miss reads only the requested binary; late session results are discarded. */
export async function diskLoad(url: string, range?: string | null): Promise<ArrayBuffer | null> {
  const epoch = generation
  const sessionGeneration = prefetchState.cacheSession
  let timer: ReturnType<typeof setTimeout> | undefined
  try {
    const key = segmentCacheKey(url, range)
    const data = await Promise.race([
      diskCache.loadSegment(key),
      new Promise<null>(resolve => { timer = setTimeout(() => resolve(null), DISK_READ_TIMEOUT_MS) }),
    ])
    if (epoch !== generation || sessionGeneration !== prefetchState.cacheSession) return null
    if (data) {
      touches.delete(key); touches.set(key, Date.now())
      while (touches.size > 2048) touches.delete(touches.keys().next().value!)
    }
    return data
  } catch { return null }
  finally { if (timer !== undefined) clearTimeout(timer) }
}

/** Explicit corruption/user invalidation remains immediate. */
export async function diskDelete(url: string, range?: string | null): Promise<void> {
  const key = segmentCacheKey(url, range)
  pending.delete(key); touches.delete(key)
  try { await diskCache.dropSegment(key) } catch { /* Rejected data is bypassed by the transfer ledger. */ }
}

export async function diskPrune(): Promise<void> {
  try { await diskCache.prune() } catch { /* Optional cache. */ }
}
export async function diskClear(): Promise<void> {
  cancelDiskCommit()
  try { await diskCache.clear() } catch { /* Optional cache. */ }
}
export async function diskCacheInfo(): Promise<DiskCacheInfo> { return diskCache.info() }
export async function diskCachePrune(maxBytes: number): Promise<number> {
  try { return await diskCache.prune(Math.min(maxBytes, MAX_DISK_BYTES)) } catch { return 0 }
}

if (import.meta.hot) import.meta.hot.dispose(() => { playing = true; cancelDiskCommit() })
