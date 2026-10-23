import { computed, onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import * as AppMod from '../../api/app'
import { findBestMatch, probeNameOf, searchSourceWithFallback } from './sourceMatching'

export type SourceProbe = 'probing' | 'local' | 'remote' | 'missing' | 'failed'
export interface SourceOption {
  source_key: string
  name: string
  vod_id?: string  // 该源中对应视频的 vod_id
  probe: SourceProbe
  hit?: any        // probe 为 remote 时的源站搜索原始条目，切换前要先入库
}

/** Discovery state belongs to one page, never to a retained global store. */
export function useSourceCatalog(deps: {
  sources: () => ReadonlyArray<{source_key?:string;name?:string}>
  identity: () => {sourceKey:string;vodId:string;name:string}
}) {
const { t } = useI18n()
const sourceOptions = ref<SourceOption[]>([])
const activeSourceKey = ref('')
const readySourceCount = computed(() => sourceOptions.value.filter((s) => s.probe === 'local' || s.probe === 'remote').length)
const probingCount = computed(() => sourceOptions.value.filter((s) => s.probe === 'probing').length)

const PROBE_CONCURRENCY = 2
const REMOTE_PROBE_TTL = 10 * 60 * 1000
type ProbeOutcome = { probe: SourceProbe; vodId: string | null; hit?: any }
// The cache belongs to this page: repeated probes during its lifetime can reuse outcomes.
const remoteProbeCache = new Map<string, { outcome: ProbeOutcome; at: number }>()
let probeGeneration = 0
let disposed = false
type PendingRequest = Promise<any> & {cancel?:()=>void|Promise<void>}
const requests = new Set<PendingRequest>()
const cancellationRequested = new WeakSet<PendingRequest>()
let requestsStarted = 0, requestsSettled = 0, requestsFailed = 0, cancellationsRequested = 0
async function owned(request: PendingRequest): Promise<any> {
  requestsStarted++
  requests.add(request)
  try { return await request }
  catch (error) { requestsFailed++; throw error }
  finally { requestsSettled++; requests.delete(request) }
}
function cancelRequests(): void {
  for (const request of requests) {
    if (!request.cancel || cancellationRequested.has(request)) continue
    cancellationRequested.add(request)
    cancellationsRequested++
    try { void Promise.resolve(request.cancel?.()).catch(()=>{}) } catch { /* Already completed. */ }
  }
}
function searchCatalogSource(source:string,name:string):Promise<any[]> {
  const generation = probeGeneration
  return searchSourceWithFallback(source,name,{current:()=>!disposed&&generation===probeGeneration,request:owned})
}

async function buildSourceOptions(): Promise<void> {
  if (disposed || !deps.sources().length) return
  cancelRequests()
  const generation = ++probeGeneration
  const vodName = probeNameOf(deps.identity().name)
  sourceOptions.value = deps.sources().map((s: any) => {
    const isCurrent = s.source_key === deps.identity().sourceKey
    return {
      source_key: s.source_key || '',
      name: s.name || s.source_key || '',
      // 能进到这个页面就说明这一条在本库里，当前源直接算 local。
      vod_id: isCurrent ? String(deps.identity().vodId) : undefined,
      probe: isCurrent ? 'local' as SourceProbe : 'probing' as SourceProbe,
    }
  })
  activeSourceKey.value = deps.identity().sourceKey

  await probeLocalLibrary(generation)
  await probeRemoteSources(vodName, generation)
}

/** 本库探测：把已经有同一 global_id 副本的源标成 local。 */
async function probeLocalLibrary(generation: number): Promise<void> {
  let globalId = 0
  try {
    globalId = await owned((AppMod as any).GetGlobalIdForVideo(deps.identity().sourceKey, String(deps.identity().vodId))) as number
  } catch (e) {
    console.log('[Player] global_id 查找失败，改由源站探测补全:', e)
  }
  if (generation !== probeGeneration || globalId <= 0) return
  try {
    const refs = await owned((AppMod as any).FindSourcesByGlobalId(globalId)) as any[]
    if (generation !== probeGeneration) return
    if (!Array.isArray(refs) || refs.length === 0) return
    console.log(`[Player] ✔ global_id=${globalId} 本库命中 ${refs.length} 个源`)
    for (const opt of sourceOptions.value) {
      if (opt.probe !== 'probing') continue
      const ref = refs.find((r: any) => r.source_key === opt.source_key)
      if (ref?.vod_id) {
        opt.probe = 'local'
        opt.vod_id = String(ref.vod_id)
      }
    }
  } catch (e) {
    console.log('[Player] 按 global_id 查源失败:', e)
  }
}

/** 源站探测：本库没命中的源逐个去搜，结果一条一条显示出来。 */
async function probeRemoteSources(vodName: string, generation: number): Promise<void> {
  const pending = sourceOptions.value.filter((o) => o.probe === 'probing')
  if (pending.length === 0) return
  if (!vodName) {
    for (const opt of pending) opt.probe = 'failed'
    return
  }
  let cursor = 0
  const worker = async (): Promise<void> => {
    for (; ;) {
      if (generation !== probeGeneration) return
      const opt = pending[cursor++]
      if (!opt) return
      const outcome = await probeSourceRemote(opt.source_key, vodName)
      if (generation !== probeGeneration) return
      // 用户可能已经手动切过这个源，那一行的结论以本地为准，不覆盖。
      if (opt.source_key === activeSourceKey.value) continue
      opt.probe = outcome.probe
      opt.vod_id = outcome.vodId ?? undefined
      opt.hit = outcome.hit
    }
  }
  await Promise.all(Array.from({ length: Math.min(PROBE_CONCURRENCY, pending.length) }, worker))
}

async function probeSourceRemote(sourceKeyOf: string, vodName: string): Promise<ProbeOutcome> {
  const generation = probeGeneration
  const cacheKey = `${sourceKeyOf}|${vodName}`
  const cached = remoteProbeCache.get(cacheKey)
  if (cached && Date.now() - cached.at < REMOTE_PROBE_TTL) return cached.outcome
  let outcome: ProbeOutcome = { probe: 'failed', vodId: null }
  try {
    const list = await searchCatalogSource(sourceKeyOf, vodName)
    const match = findBestMatch(list, vodName)
    outcome = match?.vod_id
      ? { probe: 'remote', vodId: String(match.vod_id), hit: match }
      : { probe: 'missing', vodId: null }
  } catch {
    outcome = { probe: 'failed', vodId: null }
  }
  if (!disposed && generation === probeGeneration) remoteProbeCache.set(cacheKey, { outcome, at: Date.now() })
  return outcome
}

function probeLabel(probe: SourceProbe): string {
  switch (probe) {
    case 'local': return t('player.probeLocal')
    case 'remote': return t('player.probeRemote')
    case 'missing': return t('player.probeMissing')
    case 'failed': return t('player.probeFailed')
    default: return t('player.probeProbing')
  }
}

/** 探测出结果的源可以直接切；没结果的（missing/probing）不给点，failed 留给点击重试。 */
function sourceSelectable(src: SourceOption): boolean {
  if (src.source_key === activeSourceKey.value) return false
  if (src.vod_id) return true
  return src.probe === 'failed'
}


onBeforeUnmount(() => { disposed = true; probeGeneration++; cancelRequests(); remoteProbeCache.clear() })
return { sourceOptions, activeSourceKey, readySourceCount, probingCount, buildSourceOptions, probeLabel, sourceSelectable, searchCatalogSource,
  snapshot:()=>({pending_requests:requests.size,requests_started:requestsStarted,requests_settled:requestsSettled,
    requests_failed:requestsFailed,cancellations_requested:cancellationsRequested,source_count:sourceOptions.value.length,disposed}) }
}
