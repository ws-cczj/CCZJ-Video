import { computed, onScopeDispose, watch } from 'vue'
import { defineStore, storeToRefs } from 'pinia'
import { useRouter } from 'vue-router'
import { useSourceStore } from '../../stores/source'
import { useErrorStore } from '../../stores/error'
import { tr } from '../../locales'
import { useShortDramaFeed, type ShortDramaEntry } from './useShortDramaFeed'
import { shortDramaScene } from './scene'
import { presentationProfile } from '../../platform/presentation'
import { prefetchMediaStart } from '../../utils/tsCache'
import { resolveEpisodeUrl } from '../../utils'
import { recordDiagnosticEvent } from '../../diagnostics/recording'
import { useShortDramaPreferences } from './recommendationPreferences'

/** One bounded recommendation session is shared by Home warm-up and playback.
 * Home prepares data/bytes, never a hidden decoder or audible playback. */
export const useShortDramaSession = defineStore('short-drama-session', () => {
  const router = useRouter(), sources = useSourceStore(), errors = useErrorStore()
  const { currentSourceKey } = storeToRefs(sources)
  const mobile = presentationProfile().ui === 'mobile'
  const preferences=useShortDramaPreferences()
  void preferences.load()
  const home = computed(() => mobile && router.currentRoute.value.path === '/')
  const playing = computed(() => router.currentRoute.value.path === '/short-drama' && shortDramaScene(router.currentRoute.value, mobile) !== 'theatre')
  const enabled = computed(() => preferences.ready && (home.value || playing.value))
  const entry = computed<ShortDramaEntry | null>(() => {
    if (!playing.value) return null
    const query = router.currentRoute.value.query
    if (typeof query.source !== 'string' || !query.source || typeof query.vod !== 'string' || !query.vod) return null
    const integer = (value: unknown) => typeof value === 'string' && Number.isSafeInteger(Number(value)) && Number(value) >= 0 ? Number(value) : 0
    return { sourceKey: query.source, vodId: query.vod, globalId: integer(query.global_id), episodeIndex: integer(query.episode), lineIndex: query.line === undefined ? undefined : integer(query.line) }
  })
  let beforeChange: (() => void) | undefined
  function observeChange(handler: () => void): () => void {
    beforeChange = handler
    return () => { if (beforeChange === handler) beforeChange = undefined }
  }
  const feed = useShortDramaFeed(currentSourceKey, enabled, () => beforeChange?.(), entry, {
    sources: () => sources.sources.filter(source => source.enabled !== 0 && source.enabled !== false).map(source => source.source_key || '').filter(Boolean),
    playing: () => playing.value,
    choose: (batch,fallback,current) => preferences.choose(batch,fallback,current),
    remember: (source,vod) => preferences.remember(source,vod),
    preferenceVersion: () => preferences.blocked,
    backgroundError: cause => { if (playing.value) errors.fromError(tr('shortDrama.loadFailed'), cause, 'short-drama.refresh') },
  })
  const warmMedia = computed(() => {
    if (!enabled.value) return null
    if (!home.value) return feed.nextMedia.value
    const ep = feed.episode.value, video = feed.video.value
    return ep && video ? { ...ep, ep_url: resolveEpisodeUrl(ep), source_key: feed.playbackSource.value, vod_id: String(video.vod_id ?? '') } : null
  })
  let warm: AbortController | undefined
  watch([warmMedia, home], ([media, idle]) => {
    warm?.abort(); warm = undefined
    if (!media?.ep_url) return
    const owned = new AbortController()
    warm = owned
    void prefetchMediaStart(media, owned.signal, idle).catch(cause => {
      if (!owned.signal.aborted) void recordDiagnosticEvent('short_drama_prefetch_failed', { message: String(cause) })
    })
  }, { flush: 'post' })
  onScopeDispose(() => { warm?.abort(); beforeChange = undefined })
  return { ...feed, entry, observeChange, preferences }
})
