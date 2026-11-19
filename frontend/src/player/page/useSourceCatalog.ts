import { computed, onScopeDispose, ref, watch } from 'vue'
import { FindSourcesByGlobalId, GetGlobalIdForVideo } from '../../api/app'
import { ProbePlayLines, type PlayLineQuality } from '../../api/playback'
import { normalizeApiError } from '../../api/errors'
import { findBestMatch, probeNameOf, searchSourceWithFallback } from './sourceMatching'
import { cacheResult, discovery, discoveryKey, FAILURE_MS, FRESH_MS, invalidation, quality, read, retain, shouldProbe, sourceIdentity,
  type Cached, type LineOutcome, type MediaIdentity, type ProbeOutcome } from './sourceProbeCache'

export type SourceProbe = 'probing' | 'local' | 'remote' | 'missing' | 'failed'
export interface SourceOption {
  source_key: string
  name: string
  vod_id?: string
  probe: SourceProbe
  hit?: any
  lines?: PlayLineQuality[]
  qualityState?: 'pending' | 'ready' | 'failed' | 'limited'
}

type SourceRow = { source_key?: string; name?: string; enabled?: number | boolean }
const PROBE_CONCURRENCY = 2

type Pending<T = unknown> = Promise<T> & { cancel?: () => unknown }

export function useSourceCatalog(deps: {
  sources: () => ReadonlyArray<SourceRow>
  identity: () => MediaIdentity | undefined
  episode?: () => number
  active?: () => boolean
}) {
  const sourceOptions = ref<SourceOption[]>([])
  const activeSourceKey = ref('')
  const readySourceCount = computed(() => sourceOptions.value.filter(s => s.probe === 'local' || s.probe === 'remote').length)
  const refreshing = ref(false)

  let generation = 0
  let disposed = false
  let running = false
  const probeRequests = new Set<Pending>()
  const switchRequests = new Set<Pending>()
  let requestsStarted = 0, requestsSettled = 0, requestsFailed = 0, cancellationsRequested = 0

  async function owned<T>(request: Pending<T>, requests = probeRequests): Promise<T> {
    requestsStarted++
    requests.add(request)
    try { return await request }
    catch (error) { requestsFailed++; throw error }
    finally { requestsSettled++; requests.delete(request) }
  }

  function cancelRequests(requests: Set<Pending>): void {
    for (const request of requests) {
      if (!request.cancel) continue
      cancellationsRequested++
      try { void Promise.resolve(request.cancel()).catch(() => {}) } catch { /* Already settled. */ }
    }
    requests.clear()
  }

  function stopProbe(): void {
    generation++
    running = refreshing.value = false
    cancelRequests(probeRequests)
  }

  function searchCatalogSource(source: string, name: string, current = () => !disposed): Promise<any[]> {
    return searchSourceWithFallback(source, name, { current, request: request => owned(request, switchRequests) })
  }

  function restore(item: MediaIdentity, sources: ReadonlyArray<SourceRow>): void {
    activeSourceKey.value = item.sourceKey
    const enabled = sources.filter(s => !!s.source_key && (s.source_key === item.sourceKey || (s.enabled !== 0 && s.enabled !== false)))
    if (!enabled.some(s => s.source_key === item.sourceKey)) enabled.unshift({ source_key: item.sourceKey })
    sourceOptions.value = enabled.map(s => {
      const sourceKey = s.source_key!
      const result = sourceKey === item.sourceKey
        ? { probe: 'local' as const, vod_id: item.vodId }
        : read(discovery, discoveryKey(item, sourceKey))?.value ?? { probe: 'probing' as const }
      const measurement = result.vod_id ? read(quality, sourceIdentity(sourceKey, result.vod_id))?.value : undefined
      return { source_key: sourceKey, name: s.name || sourceKey, ...result, ...measurement }
    })
  }

  async function findLocalCopies(item: MediaIdentity, current: () => boolean): Promise<Map<string, string>> {
    try {
      const id = item.globalId || await owned(GetGlobalIdForVideo(item.sourceKey, item.vodId))
      if (id <= 0 || !current()) return new Map()
      const rows = await owned(FindSourcesByGlobalId(id))
      const copies = new Map<string, string>()
      for (const row of rows ?? []) if (row?.source_key && row.vod_id) copies.set(row.source_key, String(row.vod_id))
      return copies
    } catch {
      return new Map()
    }
  }

  async function updateDiscovery(option: SourceOption, item: MediaIdentity, localId: string | undefined, current: () => boolean): Promise<void> {
    const key = discoveryKey(item, option.source_key)
    const previous = read(discovery, key)
    let entry: Cached<ProbeOutcome>
    if (localId) {
      entry = cacheResult({ probe: 'local', vod_id: localId })
    } else {
      try {
        const title = probeNameOf(item.name)
        if (!title) throw new Error('Missing title')
        const matches = await searchSourceWithFallback(option.source_key, title, { current, request: owned })
        const match = findBestMatch(matches, title)
        if (match?.vod_id && match.in_catalog) {
          entry = cacheResult({ probe: 'local', vod_id: String(match.vod_id) })
        } else if (match?.vod_id) {
          entry = cacheResult({ probe: 'remote', vod_id: String(match.vod_id), hit: match })
        } else {
          entry = cacheResult({ probe: 'missing' }, FRESH_MS, FRESH_MS)
        }
      } catch (error) {
        entry = previous
          ? { ...previous, refreshAt: Date.now() + FAILURE_MS }
          : cacheResult({ probe: 'failed' }, FAILURE_MS, FAILURE_MS)
        const failure = normalizeApiError(error)
        const retry = failure.retryAfterSeconds ?? (failure.code === 'RATE_LIMITED' ? 300 : 0)
        if (retry) {
          entry.retryAt = Date.now() + retry * 1000
          entry.expiresAt = Math.max(entry.expiresAt, entry.retryAt)
        }
      }
    }
    if (!current()) return
    Object.assign(option, { vod_id: undefined, hit: undefined }, entry.value)
    retain(discovery, key, { ...entry, origin: item, sourceKey: option.source_key })
  }

  async function updateQuality(option: SourceOption, episode: number, manual: boolean, current: () => boolean): Promise<void> {
    const vodId = option.vod_id
    if (!vodId) return
    const key = sourceIdentity(option.source_key, vodId)
    const previous = read(quality, key)
    const needsProbe = shouldProbe(previous, manual)
    Object.assign(option, { lines: undefined, qualityState: needsProbe ? 'pending' : undefined }, previous?.value)
    if (!needsProbe) return
    let entry: Cached<LineOutcome>
    try {
      const lines = await owned(ProbePlayLines({ source_key: option.source_key, vod_id: vodId, global_id: 0, ep_num: episode }))
      const remembered = lines.map(line => {
        const good = previous?.value.lines?.find(row => row.index === line.index && row.name === line.name && row.ok)
        return !line.ok && line.code && good ? { ...good, code: line.code, retry_after_seconds: line.retry_after_seconds } : line
      })
      const limited = lines.length > 0 && lines.every(line => line.code === 'RATE_LIMITED')
      const retry = Math.max(0, ...lines.map(line => line.retry_after_seconds || 0)) || (limited ? 300 : 0)
      const unmeasured = lines.length > 0 && lines.every(line => !line.ok)
      const value: LineOutcome = { lines: remembered, qualityState: limited ? 'limited' : 'ready' }
      entry = unmeasured && previous
        ? { ...previous, value, refreshAt: Date.now() + FAILURE_MS, retryAt: undefined }
        : cacheResult(value, unmeasured ? FAILURE_MS : FRESH_MS)
      if (retry) entry.retryAt = Date.now() + retry * 1000
    } catch (error) {
      const failure = normalizeApiError(error)
      if (!current() || failure.code === 'CANCELLED') return
      const limited = failure.code === 'RATE_LIMITED'
      const retry = failure.retryAfterSeconds ?? (limited ? 300 : 0)
      const value: LineOutcome = { lines: previous?.value.lines, qualityState: limited ? 'limited' : previous?.value.lines?.some(line => line.ok) ? 'ready' : 'failed' }
      entry = previous ? { ...previous, value, refreshAt: Date.now() + FAILURE_MS } : cacheResult(value, FAILURE_MS, FAILURE_MS)
      if (retry) entry.retryAt = Date.now() + retry * 1000
    }
    if (!current()) return
    if (entry.retryAt) entry.expiresAt = Math.max(entry.expiresAt, entry.retryAt)
    Object.assign(option, entry.value)
    retain(quality, key, { ...entry, sourceKey: option.source_key, vodId })
  }

  async function buildSourceOptions(manual = true): Promise<void> {
    const item = deps.identity()
    if (disposed || !item?.sourceKey || !item.vodId || deps.active?.() === false) return
    if (running) {
      if (manual) refreshing.value = true
      return
    }
    const episode = deps.episode?.() || 0
    restore(item, deps.sources())
    const rows = sourceOptions.value
    const needsDiscovery = new Set(rows.filter(row => row.source_key !== item.sourceKey
      && shouldProbe(read(discovery, discoveryKey(item, row.source_key)), manual)))
    const pending = rows.filter(row => needsDiscovery.has(row) || !!row.vod_id
      && shouldProbe(read(quality, sourceIdentity(row.source_key, row.vod_id)), manual))
    if (!pending.length) return

    const token = ++generation
    const current = () => !disposed && token === generation
    running = true
    refreshing.value = manual
    try {
      const copies = needsDiscovery.size ? await findLocalCopies(item, current) : new Map<string, string>()
      let index = 0
      const worker = async () => {
        while (current()) {
          const option = pending[index++]
          if (!option) return
          if (needsDiscovery.has(option)) await updateDiscovery(option, item, copies.get(option.source_key), current)
          if (!current()) return
          await updateQuality(option, episode, manual, current)
        }
      }
      await Promise.all(Array.from({ length: Math.min(PROBE_CONCURRENCY, pending.length) }, worker))
    } finally {
      if (current()) running = refreshing.value = false
    }
  }

  if (deps.active) watch(() => [deps.active?.(), deps.identity(), deps.sources()] as const, ([active]) => {
    stopProbe()
    if (active) void buildSourceOptions(false)
  }, { immediate: true })

  watch(invalidation, change => {
    const item = deps.identity()
    if (!change || !item) return
    const { sourceKey, vodIds } = change
    const option = sourceOptions.value.find(row => row.source_key === sourceKey)
    if (!option || (vodIds && option.vod_id && !vodIds.includes(option.vod_id))) return
    stopProbe()
    restore(item, deps.sources())
  }, { flush: 'sync' })

  onScopeDispose(() => {
    disposed = true
    stopProbe()
    cancelRequests(switchRequests)
  })

  return {
    sourceOptions, activeSourceKey, readySourceCount, refreshing, buildSourceOptions, searchCatalogSource,
    sourceSelectable: (source: SourceOption) => source.source_key !== activeSourceKey.value && !!source.vod_id,
    snapshot: () => ({
      pending_requests: probeRequests.size + switchRequests.size, requests_started: requestsStarted, requests_settled: requestsSettled,
      requests_failed: requestsFailed, cancellations_requested: cancellationsRequested, source_count: sourceOptions.value.length, disposed,
    }),
  }
}
