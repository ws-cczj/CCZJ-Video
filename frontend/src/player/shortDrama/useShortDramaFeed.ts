import { computed, onBeforeUnmount, ref, shallowRef, watch, type Ref } from 'vue'
import { GetVideoDetail, GetShortDramaRecommendations } from '../../api/video'
import { normalizeApiError } from '../../api/errors'
import { createRequestScope } from '../../api/requestScope'
import { useGeneration } from '../../composables/useGeneration'
import { resolveEpisodeUrl } from '../../utils'
import type { Video, VideoDetailResponse } from '../../types'

export interface ShortDramaEntry {
  sourceKey: string
  vodId: string
  globalId: number
  episodeIndex: number
  lineIndex?: number
}

/** One batch and one hydrated title belong to this view. Hidden tabs release
 * media and in-flight detail work; late replies cannot replace a newer choice. */
export function useShortDramaFeed(source: Ref<string>, active: Ref<boolean>, beforeChange: () => void, entry: Ref<ShortDramaEntry | null> = shallowRef(null)) {
  const items = shallowRef<Video[]>([])
  const selected = ref(0), episodeIndex = ref(0)
  const detail = shallowRef<VideoDetailResponse | null>(null)
  const loading = ref(false), error = ref('')
  const playbackSource = ref(source.value)
  const series = ref(false), completed = ref(false)
  const effectiveSource = computed(() => entry.value?.sourceKey || source.value)
  const requests = createRequestScope(), generation = useGeneration()
  const video = computed(() => detail.value?.video ?? items.value[selected.value] ?? null)
  const episodes = computed(() => detail.value?.episodes ?? [])
  const episode = computed(() => episodes.value[episodeIndex.value])
  const url = computed(() => episode.value ? resolveEpisodeUrl(episode.value) : '')
  const canPrevious = computed(() => !loading.value && !error.value && (series.value ? episodeIndex.value > 0 : selected.value > 0))
  const canNext = computed(() => !loading.value && !error.value && !!episodes.value.length && (!series.value || episodeIndex.value + 1 < episodes.value.length))
  let batchSource = ''
  let entryKey = ''
  let retryBatch = false

  function invalidate(): void {
    beforeChange()
    generation.invalidate()
    requests.cancel()
    detail.value = null
    loading.value = false
    completed.value = false
  }
  async function open(index: number, resumeEpisode = false): Promise<void> {
    const item = items.value[index]
    if (!active.value || !item?.vod_id) return
    invalidate()
    selected.value = index
    playbackSource.value = effectiveSource.value
    if (!resumeEpisode) episodeIndex.value = 0
    const token = generation.begin(), sourceKey = effectiveSource.value
    loading.value = true; error.value = ''
    retryBatch = false
    try {
      const result = await requests.run(() => GetVideoDetail({ source_key: sourceKey, vod_id: item.vod_id!, global_id: item.global_id ?? 0, refresh: false }))
      if (!generation.isCurrent(token)) return
      if (result.error) { error.value = result.error.message; return }
      // Some providers return only line-based episodes. Choose the first usable
      // line without mounting the landscape page or changing its global store.
      const chosenLine = result.lines?.find(line => line?.index === entry.value?.lineIndex)
      const available = chosenLine?.episodes?.length ? chosenLine.episodes
        : result.episodes.length ? result.episodes : result.lines?.find(line => line?.episodes?.length)?.episodes ?? []
      detail.value = { ...result, episodes: available }
      episodeIndex.value = Math.min(episodeIndex.value, Math.max(0, available.length - 1))
    } catch (cause) {
      if (generation.isCurrent(token) && normalizeApiError(cause).code !== 'CANCELLED') error.value = normalizeApiError(cause).message
    } finally { if (generation.isCurrent(token)) loading.value = false }
  }
  async function refresh(): Promise<void> {
    if (entry.value) return
    if (!active.value || !source.value) return
    invalidate()
    series.value = false
    const previous = batchSource === source.value ? items.value.map(item => item.vod_id!).filter(Boolean) : []
    if (batchSource !== source.value) items.value = []
    const token = generation.begin(), sourceKey = source.value
    loading.value = true; error.value = ''
    retryBatch = true
    try {
      let batch = await requests.run(() => GetShortDramaRecommendations(sourceKey, previous))
      // A small library can run out of unseen titles; cycle through its actual
      // contents instead of filling the feed with unrelated videos.
      if (!generation.isCurrent(token)) return
      if (!batch.length && previous.length) batch = await requests.run(() => GetShortDramaRecommendations(sourceKey, []))
      if (!generation.isCurrent(token)) return
      items.value = batch; batchSource = sourceKey; selected.value = 0; episodeIndex.value = 0
      loading.value = false
      if (batch.length) await open(0)
    } catch (cause) {
      if (generation.isCurrent(token) && normalizeApiError(cause).code !== 'CANCELLED') error.value = normalizeApiError(cause).message
    } finally { if (generation.isCurrent(token)) loading.value = false }
  }
  function selectEpisode(index: number): void {
    if (!active.value || loading.value || !episodes.value[index]) return
    series.value = true
    if (index === episodeIndex.value) return
    changeEpisode(index)
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
    if (!active.value || loading.value || error.value) return
    const delta = direction === 'next' ? 1 : -1
    if (series.value) {
      const index = episodeIndex.value + delta
      if (episodes.value[index]) changeEpisode(index)
      return
    }
    const index = selected.value + delta
    if (index >= 0 && index < items.value.length) void open(index)
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
    if (!enabled) { invalidate(); return }
    if (!key) { invalidate(); items.value = []; return }
    if (target) {
      const nextKey = JSON.stringify(target)
      if (nextKey !== entryKey) {
        invalidate()
        items.value = [{ vod_id:target.vodId, global_id:target.globalId }]
        selected.value = 0; episodeIndex.value = target.episodeIndex
        series.value = true; entryKey = nextKey; batchSource = ''
      }
      void open(0, true)
      return
    }
    if (entryKey) { entryKey = ''; items.value = []; batchSource = '' }
    if (batchSource !== key || !items.value.length) void refresh()
    else void open(selected.value, true)
  }, { immediate: true, flush: 'post' })
  onBeforeUnmount(invalidate)
  return { items, selected, episodeIndex, video, episodes, episode, url, detail, loading, error, playbackSource, effectiveSource, series, completed, canPrevious, canNext, refresh, open, selectEpisode, discover, step, ended,
    retry: () => retryBatch || !items.value.length ? refresh() : open(selected.value, true) }
}
