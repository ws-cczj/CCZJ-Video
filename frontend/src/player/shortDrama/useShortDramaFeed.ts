import { computed, onScopeDispose, ref, shallowRef, watch, type Ref } from 'vue'
import { GetVideoDetail, GetShortDramaRecommendations, GetShortDramaPage, SHORT_DRAMA_FRESH_MS } from '../../api/video'
import { ApiFailure, normalizeApiError } from '../../api/errors'
import { createRequestScope } from '../../api/requestScope'
import { useGeneration } from '../../composables/useGeneration'
import { resolveEpisodeUrl } from '../../utils'
import { tr } from '../../locales'
import type { Video, VideoDetailResponse } from '../../types'
import { recordDiagnosticEvent } from '../../diagnostics/recording'

export interface ShortDramaEntry {
  sourceKey: string
  vodId: string
  globalId: number
  episodeIndex: number
  lineIndex?: number
}

/** One batch and one hydrated title belong to this view. Hidden tabs release
 * media and in-flight detail work; late replies cannot replace a newer choice. */
export function useShortDramaFeed(source: Ref<string>, active: Ref<boolean>, beforeChange: () => void, entry: Ref<ShortDramaEntry | null> = shallowRef(null), options: { playing?: () => boolean; backgroundError?: (cause: unknown) => void; choose?: (batch:Video[],fallback:string,current?:{source:string;vod:string}) => Video[]; remember?: (source:string,vod:string) => void; preferenceVersion?: () => unknown } = {}) {
  const items = shallowRef<Video[]>([])
  const selected = ref(0), episodeIndex = ref(0)
  const detail = shallowRef<VideoDetailResponse | null>(null)
  const loading = ref(false), error = ref('')
  const selecting = ref(false)
  const playbackSource = ref(source.value)
  const series = ref(false), completed = ref(false)
  const effectiveSource = computed(() => entry.value?.sourceKey || source.value)
  const emptySource = ref('')
  const knownEmpty = computed(() => !!emptySource.value && emptySource.value === effectiveSource.value && !entry.value)
  const requests = createRequestScope(), generation = useGeneration()
  const nextRequests = createRequestScope(), nextGeneration = useGeneration()
  type Prepared = { sourceKey: string; nextEpisode: number; detail: VideoDetailResponse }
  type Remote = { source: string; category: string; page: number; hasMore: boolean }
  const nextItem = shallowRef<{ batch: Video[]; index: number; prepared: Prepared; remote?: Remote } | null>(null)
  const video = computed(() => detail.value?.video ?? items.value[selected.value] ?? null)
  const episodes = computed(() => detail.value?.episodes ?? [])
  const episode = computed(() => episodes.value[episodeIndex.value])
  const url = computed(() => episode.value ? resolveEpisodeUrl(episode.value) : '')
  const canPrevious = computed(() => (!loading.value || selecting.value) && (series.value ? !!url.value && episodeIndex.value > 0 : selected.value > 0))
  const canNext = computed(() => !series.value && (selecting.value || (!loading.value && !detail.value)) ? !!items.value[selected.value + 1] : !loading.value && !!episodes.value.length && (!series.value || episodeIndex.value + 1 < episodes.value.length))
  let batchSource = ''
  let entryKey = ''
  let retryBatch = false
  let remoteSource='', remoteCategory='', remotePage=0, remoteHasMore=true
  let deadline: ReturnType<typeof setTimeout> | undefined
  let identitySource = ''
  const nextMedia = computed(() => {
    if (series.value) {
      const ep = episodes.value[episodeIndex.value + 1]
      return ep && video.value ? { ...ep, ep_url: resolveEpisodeUrl(ep), source_key: playbackSource.value, vod_id: String(video.value.vod_id ?? '') } : null
    }
    const next = nextItem.value, ep = next?.prepared.detail.episodes[next.prepared.nextEpisode]
    return next && ep ? { ...ep, ep_url: resolveEpisodeUrl(ep), source_key: next.prepared.sourceKey, vod_id: String(next.prepared.detail.video?.vod_id ?? next.batch[next.index]?.vod_id ?? '') } : null
  })
  const updateRequests=createRequestScope(),updateGeneration=useGeneration()
  let updateBusy=false,updatedSource='',updatedAt=0,updatePage=0,updateCursor=''
  function cancelUpdate():void {updateGeneration.invalidate();updateRequests.cancel();updateBusy=false}
  async function updateCatalog():Promise<void> {
    const key=source.value
    if(!active.value||entry.value||!key||updateBusy||(updatedSource===key&&performance.now()-updatedAt<SHORT_DRAMA_FRESH_MS))return
    if(updatedSource!==key){updatePage=0;updateCursor=''}
    const token=updateGeneration.begin();updateBusy=true
    try {
      const page=await updateRequests.run(()=>GetShortDramaPage(key,updatePage+1,'',key,updateCursor))
      if(!updateGeneration.isCurrent(token)||key!==source.value)return
      updatedSource=key;updatedAt=performance.now()
      updatePage=page.has_more?updatePage+1:0;updateCursor=page.has_more?page.category_id:''
    }catch(cause){
      if(updateGeneration.isCurrent(token)&&normalizeApiError(cause).code!=='CANCELLED') {
        updatePage=0;updateCursor=''
        void recordDiagnosticEvent('short_drama_catalog_failed',{code:normalizeApiError(cause).code})
      }
    }finally{if(updateGeneration.isCurrent(token))updateBusy=false}
  }
  function randomBatch(batch: Video[]): Video[] {
    const result = batch.slice()
    for (let i = result.length - 1; i > 0; i--) {
      const j = Math.floor(Math.random() * (i + 1))
      ;[result[i], result[j]] = [result[j]!, result[i]!]
    }
    return options.choose?.(result,effectiveSource.value,video.value?.vod_id?{source:playbackSource.value,vod:String(video.value.vod_id)}:undefined) ?? result
  }

  function invalidate(clear = true): void {
    if (clear) beforeChange()
    generation.invalidate()
    requests.cancel()
    clearTimeout(deadline); deadline = undefined
    if (clear) detail.value = null
    loading.value = false
    selecting.value = false
    if (clear) completed.value = false
  }
  function fail(cause: unknown): void {
    const failure = normalizeApiError(cause)
    if (failure.code === 'CANCELLED') return
    error.value = failure.message
    if (url.value) options.backgroundError?.(cause)
  }
  function begin(timeout: number): number {
    invalidate(false)
    const token = generation.begin()
    loading.value = true; error.value = ''
    deadline = setTimeout(() => {
      if (!generation.isCurrent(token)) return
      invalidate(false)
      fail({ code: 'TIMEOUT', message: tr('shortDrama.timeout') })
    }, timeout)
    return token
  }
  function finish(token: number): void {
    if (!generation.isCurrent(token)) return
    clearTimeout(deadline); deadline = undefined; loading.value = false
  }
  async function readDetail(item: Video, requestedEpisode: number, scope = requests): Promise<Prepared> {
    const sourceKey = item.source_key || effectiveSource.value
    const result = await scope.run(() => GetVideoDetail({ source_key: sourceKey, vod_id: item.vod_id!, global_id: item.global_id ?? 0, refresh: false }))
    if (result.error) throw new ApiFailure({ code: 'UNAVAILABLE', message: tr('shortDrama.detailUnavailable') }, result.error)
    const chosenLine = result.lines?.find(line => line?.index === entry.value?.lineIndex)
    const available = chosenLine?.episodes?.length ? chosenLine.episodes
      : result.episodes.length ? result.episodes : result.lines?.find(line => line?.episodes?.length)?.episodes ?? []
    const nextEpisode = Math.min(requestedEpisode, Math.max(0, available.length - 1))
    if (!available[nextEpisode] || !resolveEpisodeUrl(available[nextEpisode])) throw { code: 'UNAVAILABLE', message: tr('shortDrama.noPlayable') }
    return { sourceKey, nextEpisode, detail: { ...result, episodes: available } }
  }
  async function hydrate(item: Video, token: number, requestedEpisode: number) {
    const next = await readDetail(item, requestedEpisode)
    return generation.isCurrent(token) ? next : undefined
  }
  function commit(batch: Video[], index: number, next: NonNullable<Awaited<ReturnType<typeof hydrate>>>, staged = false): void {
    if (!staged) beforeChange()
    items.value = batch; selected.value = index; playbackSource.value = next.sourceKey
    episodeIndex.value = next.nextEpisode; completed.value = false; detail.value = next.detail
    emptySource.value = ''; retryBatch = false
  }
  async function open(index: number, resumeEpisode = false, recoverMissing = true, staged = false): Promise<void> {
    const batch = items.value
    const item = batch[index]
    if (!active.value || !item?.vod_id) return
    const token = begin(15000)
    const requestedEpisode = resumeEpisode ? episodeIndex.value : 0
    if (staged) {
      // Commit presentation identity before I/O. A later swipe can cancel this
      // detail lookup instead of leaving the old page locked until it returns.
      beforeChange()
      selecting.value = true
      selected.value = index; playbackSource.value = item.source_key || effectiveSource.value
      episodeIndex.value = 0; completed.value = false; detail.value = null
    }
    const started = performance.now()
    retryBatch = false
    try {
      const next = await hydrate(item, token, requestedEpisode)
      if (next) {
        commit(batch, index, next, staged)
        void recordDiagnosticEvent('short_drama_detail_ready', { elapsed_ms: Math.round(performance.now() - started), staged })
      }
    } catch (cause) {
      if (!generation.isCurrent(token)) return
      const failure = normalizeApiError(cause)
      if (failure.code === 'NOT_FOUND' && !entry.value) {
        // A kept-alive batch can outlive source removal or catalog recycling.
        // Re-read once; retry must also query the catalog, not this retired ID.
        retryBatch = true
        if (recoverMissing) { finish(token); await refresh(); return }
      }
      fail(cause)
    } finally { if (generation.isCurrent(token)) selecting.value = false; finish(token) }
  }
  async function refresh(force = false): Promise<void> {
    if (entry.value) return
    if (!active.value || !source.value) return
    if (loading.value && !force) return
    const token = begin(30000), sourceKey = source.value
    series.value = false
    const previous = batchSource === source.value ? items.value.map(item => item.vod_id!).filter(Boolean) : []
    if (batchSource !== source.value) items.value = []
    retryBatch = true
    try {
      let batch: Video[] = []
      const failedTitles=new Set<string>()
      // Recommendations and remote discovery share the selected source. Local
      // catalogs from unrelated sources must not override that selection.
      let cachedFailure: unknown
      try {
        const cached = await requests.run(() => GetShortDramaRecommendations(sourceKey, previous))
        if (!generation.isCurrent(token)) return
        const cachedBatch = randomBatch(cached.map(item => ({ ...item, source_key: sourceKey })))
        if (cachedBatch.length) {
          failedTitles.add(String(cachedBatch[0]!.vod_id))
          const next = await hydrate(cachedBatch[0]!, token, 0)
          if (!generation.isCurrent(token)) return
          if (next) { commit(cachedBatch, 0, next); batchSource = sourceKey; void updateCatalog(); return }
        }
      } catch (cause) {
        if (!generation.isCurrent(token)) return
        if (normalizeApiError(cause).code === 'CANCELLED') throw cause
        cachedFailure = cause
      }
      // A small library can run out of unseen titles; cycle through its actual
      // contents instead of filling the feed with unrelated videos.
      if (!generation.isCurrent(token)) return
      if (!batch.length && previous.length) {
        const cachedSource = items.value[0]?.source_key || sourceKey
        batch = randomBatch((await requests.run(() => GetShortDramaRecommendations(cachedSource, []))).filter(item=>!failedTitles.has(String(item.vod_id))).map(item => ({ ...item, source_key: cachedSource })))
      }
      if (!generation.isCurrent(token)) return
      if(!batch.length){
        if(batchSource!==sourceKey){remoteSource='';remoteCategory='';remotePage=0;remoteHasMore=true}
        const rediscover=(force&&items.value.length>0)||!remoteHasMore
        const page=await requests.run(()=>GetShortDramaPage(sourceKey,rediscover?1:remotePage+1,'',rediscover?'':remoteSource,rediscover?'':remoteCategory))
        if(!generation.isCurrent(token))return
        remoteSource=page.source_key;remoteCategory=page.category_id;remotePage=rediscover?1:remotePage+1;remoteHasMore=page.has_more
        updatedSource=sourceKey;updatedAt=performance.now()
        // The remote page already belongs to the resolved short-drama category.
        // Reuse it directly instead of waiting for another catalog query.
        batch=randomBatch(page.videos.filter(item=>!failedTitles.has(String(item.vod_id))))
      }
      if (!batch.length && cachedFailure) throw cachedFailure
      batchSource = sourceKey
      emptySource.value = batch.length||remoteHasMore ? '' : sourceKey
      if (batch.length) {
        const next = await hydrate(batch[0]!, token, 0)
        if (next) {commit(batch, 0, next);void updateCatalog()}
      } else if(video.value||remoteHasMore)fail({code:'NOT_FOUND',message:tr('shortDrama.noOtherRecommendations')})
    } catch (cause) {
      if(generation.isCurrent(token)&&normalizeApiError(cause).code==='VALIDATION'){
        remoteSource='';remoteCategory='';remotePage=0;remoteHasMore=true
      }
      if (generation.isCurrent(token)) fail(cause)
    } finally { finish(token) }
  }
  function selectEpisode(index: number): void {
    if (!active.value || loading.value || !episodes.value[index]) return
    series.value = true
    if (index === episodeIndex.value) return
    changeEpisode(index)
  }
  function follow(): void {
    if(!active.value||!episodes.value.length)return
    series.value=true
  }
  function changeEpisode(index: number): void {
    beforeChange()
    completed.value = false
    episodeIndex.value = index
  }
  function discover(): void {
    const finished = completed.value
    series.value = false; completed.value = false
    if (finished) step('next')
  }
  function step(direction: 'previous' | 'next'): void {
    if (!active.value || (loading.value && !selecting.value)) return
    const delta = direction === 'next' ? 1 : -1
    if (series.value) {
      const index = episodeIndex.value + delta
      if (episodes.value[index]) changeEpisode(index)
      return
    }
    if (direction === 'next' && nextItem.value) {
      if(options.choose&&!options.choose([nextItem.value.batch[nextItem.value.index]!],effectiveSource.value).length)nextItem.value=null
    }
    if (direction === 'next' && nextItem.value) {
      invalidate(false)
      const next = nextItem.value
      if (next.remote) { remoteSource = next.remote.source; remoteCategory = next.remote.category; remotePage = next.remote.page; remoteHasMore = next.remote.hasMore }
      commit(next.batch, next.index, next.prepared)
      void recordDiagnosticEvent('short_drama_step', { prepared: true })
      return
    }
    let index = selected.value + delta
    while(index>=0&&index<items.value.length&&options.choose&&!options.choose([items.value[index]!],effectiveSource.value).length)index+=delta
    if (index >= 0 && index < items.value.length) {
      void recordDiagnosticEvent('short_drama_step', { prepared: false })
      void open(index, false, true, true)
    }
    else if (direction === 'next') void refresh()
  }
  function ended(): void {
    if (!active.value || loading.value || !episodes.value.length) return
    if (series.value && episodeIndex.value + 1 >= episodes.value.length) {
      beforeChange()
      completed.value = true
      return
    }
    step('next')
  }

  watch([effectiveSource, active, entry], ([key, enabled, target]) => {
    if (!enabled) {
      cancelUpdate()
      invalidate(false)
      // Keep presentation data for the still-visible leave transition. The
      // page pauses media now and unmounts it when visual activity ends.
      return
    }
    if (identitySource && identitySource !== key) { cancelUpdate();updatedSource='';updatedAt=0; invalidate(); items.value = []; batchSource = ''; entryKey = '' }
    identitySource = key
    if (!key) {
      invalidate(); items.value = []; error.value = ''; playbackSource.value = ''
      batchSource = ''; entryKey = ''; selected.value = 0; episodeIndex.value = 0
      series.value = false; retryBatch = true
      return
    }
    if (target) {
      const nextKey = JSON.stringify(target)
      if (nextKey !== entryKey) {
        invalidate()
        items.value = [{ vod_id:target.vodId, global_id:target.globalId }]
        selected.value = 0; episodeIndex.value = target.episodeIndex
        series.value = true; entryKey = nextKey; batchSource = ''
      }
      if (!detail.value || error.value) void open(0, true)
      return
    }
    if (entryKey) { entryKey = ''; items.value = []; batchSource = '' }
    if (batchSource !== key || !items.value.length) void refresh()
    else if (!detail.value || error.value) {void open(selected.value, true);void updateCatalog()}
    else void updateCatalog()
  }, { immediate: true, flush: 'post' })
  watch([detail, episodeIndex, series, active, () => options.playing?.() ?? active.value, () => options.preferenceVersion?.()], async () => {
    nextGeneration.invalidate(); nextRequests.cancel(); nextItem.value = null
    if (!active.value || !(options.playing?.() ?? active.value) || series.value || !detail.value) return
    const token = nextGeneration.begin()
    // Only the immediate next title owns a prepared detail; no hidden player
    // or unbounded queue is created. The same choice is consumed by step().
    const timer = setTimeout(() => { if (nextGeneration.isCurrent(token)) { nextGeneration.invalidate(); nextRequests.cancel() } }, 15000)
    try {
      let batch = items.value, index = selected.value + 1, remote: Remote | undefined
      while(index<batch.length&&options.choose&&!options.choose([batch[index]!],effectiveSource.value).length)index++
      if (!batch[index]) {
        const exclude = batch.filter(item => (item.source_key || source.value) === playbackSource.value).map(item => item.vod_id!).filter(Boolean)
        const rows = await nextRequests.run(() => GetShortDramaRecommendations(playbackSource.value, exclude))
        if (!nextGeneration.isCurrent(token)) return
        let extra = randomBatch(rows.map(item => ({ ...item, source_key: playbackSource.value })))
        if (!extra.length) {
          const pageNumber = remoteHasMore ? remotePage + 1 : 1
          const page = await nextRequests.run(() => GetShortDramaPage(source.value, pageNumber, '', remoteSource, remoteCategory))
          if (!nextGeneration.isCurrent(token)) return
          const recommended=await nextRequests.run(()=>GetShortDramaRecommendations(playbackSource.value,exclude))
          if(!nextGeneration.isCurrent(token))return
          extra=randomBatch(recommended.map(item=>({...item,source_key:playbackSource.value})))
          remote = { source: page.source_key, category: page.category_id, page: pageNumber, hasMore: page.has_more }
        }
        if (!extra.length) return
        batch = [...batch.slice(-19), ...extra]; index = batch.length - extra.length
      }
      const prepared = await readDetail(batch[index]!, 0, nextRequests)
      if (nextGeneration.isCurrent(token)) nextItem.value = { batch, index, prepared, remote }
    } catch (cause) {
      const failure = normalizeApiError(cause)
      if (nextGeneration.isCurrent(token) && failure.code !== 'CANCELLED') void recordDiagnosticEvent('short_drama_next_failed', { code: failure.code })
    }
    finally { clearTimeout(timer) }
  }, { flush: 'post' })
  onScopeDispose(() => { cancelUpdate();invalidate(); nextGeneration.invalidate(); nextRequests.cancel() })
  return { items, selected, episodeIndex, video, episodes, episode, url, detail, loading, selecting, error, playbackSource, effectiveSource, knownEmpty, series, completed, canPrevious, canNext, nextMedia, refresh, open, selectEpisode, follow, discover, step, ended,
    markWatched:()=>{if(!series.value&&video.value?.vod_id)options.remember?.(playbackSource.value,String(video.value.vod_id))},
    retry: () => retryBatch || !items.value.length ? refresh(true) : open(selected.value, true) }
}
