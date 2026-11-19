import { shallowRef } from 'vue'
import type { SourceOption } from './useSourceCatalog'

export type MediaIdentity = { sourceKey: string; vodId: string; name: string; globalId?: number }
export type ProbeOutcome = Pick<SourceOption, 'probe' | 'vod_id' | 'hit'>
export type LineOutcome = Pick<SourceOption, 'lines' | 'qualityState'>
export interface Cached<T> {
  value: T
  refreshAt: number
  expiresAt: number
  retryAt?: number
}
interface DiscoveryEntry extends Cached<ProbeOutcome> {
  origin: MediaIdentity
  sourceKey: string
}
interface QualityEntry extends Cached<LineOutcome> {
  sourceKey: string
  vodId: string
}

export const FRESH_MS = 10 * 60_000
const RETAIN_MS = 60 * 60_000
export const FAILURE_MS = 60_000
const MAX_RECORDS = 256
const MAX_RECORD_CHARS = 8 * 1024

// The document owns these bounded results. Each player owns its pending reads.
export const discovery = new Map<string, DiscoveryEntry>()
export const quality = new Map<string, QualityEntry>()
export const invalidation = shallowRef<{ sourceKey: string; vodIds?: readonly string[] }>()
export const sourceIdentity = (sourceKey: string, vodId: string) => JSON.stringify([sourceKey, vodId])
const workIdentity = (item: MediaIdentity) => item.globalId && item.globalId > 0
  ? JSON.stringify(['global', item.globalId])
  : sourceIdentity(item.sourceKey, item.vodId)
export const discoveryKey = (item: MediaIdentity, sourceKey: string) => JSON.stringify([workIdentity(item), sourceKey])

export function read<T extends { expiresAt: number }>(cache: Map<string, T>, key: string): T | undefined {
  const item = cache.get(key)
  if (!item) return
  cache.delete(key)
  if (item.expiresAt <= Date.now()) return
  cache.set(key, item)
  return item
}

export function retain<T>(cache: Map<string, T>, key: string, item: T): void {
  // Bound bytes as well as entries. A huge upstream result works for the
  // current page without entering the shared cache.
  cache.delete(key)
  if (JSON.stringify(item).length > MAX_RECORD_CHARS) return
  cache.set(key, item)
  if (cache.size > MAX_RECORDS) cache.delete(cache.keys().next().value!)
}

export function shouldProbe(item: Cached<unknown> | undefined, manual: boolean): boolean {
  const now = Date.now()
  return !(item?.retryAt && item.retryAt > now) && (manual || !item || item.refreshAt <= now)
}

export function cacheResult<T>(value: T, freshFor = FRESH_MS, retainFor = RETAIN_MS): Cached<T> {
  const now = Date.now()
  return { value, refreshAt: now + freshFor, expiresAt: now + retainFor }
}

export function invalidatePlaybackProbes(sourceKey: string, vodIds?: readonly string[]): void {
  const matches = (source: string, vodId?: string) => source === sourceKey && (!vodIds || !!vodId && vodIds.includes(vodId))
  for (const [key, row] of discovery) {
    if (matches(row.sourceKey, row.value.vod_id) || ((vodIds || !row.origin.globalId) && matches(row.origin.sourceKey, row.origin.vodId))) discovery.delete(key)
  }
  for (const [key, row] of quality) if (matches(row.sourceKey, row.vodId)) quality.delete(key)
  invalidation.value = { sourceKey, vodIds }
}

