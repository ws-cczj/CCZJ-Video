<script setup lang="ts">
defineOptions({ name: 'ShortDrama' })
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { storeToRefs } from 'pinia'
import { useI18n } from 'vue-i18n'
import VideoPlayer from '../components/VideoPlayer.vue'
import Icon from '../components/Icon.vue'
import RemoteImage from '../components/RemoteImage.vue'
import '../player/shortDrama/presentation.css'
import { Button, Empty, Spinner } from '../components/ui'
import CczjMobilePlayerDrawer from '../mobile/CczjMobilePlayerDrawer.vue'
import { presentationProfile } from '../platform/presentation'
import { useSourceStore } from '../stores/source'
import { useErrorStore } from '../stores/error'
import { useViewActivity } from '../composables/useViewActivity'
import { useShortDramaFeed, type ShortDramaEntry } from '../player/shortDrama/useShortDramaFeed'
import { useEpisodeHistory } from '../player/page/useEpisodeHistory'
import { stripHtmlTags, resolveEpisodeUrl, getShortDramaPath } from '../utils'
import type { Video } from '../types'
import { flushEpProgress } from '../utils/episodeProgress'
import { TsCache } from '../utils/tsCache'
import ShortDramaCatalog from '../player/shortDrama/ShortDramaCatalog.vue'
import ShortDramaTheatre from '../player/shortDrama/ShortDramaTheatre.vue'
import { shortDramaScene } from '../player/shortDrama/scene'
import { usePlaybackFavorites } from '../player/page/usePlaybackFavorites'
import FavFolderModal from '../components/FavFolderModal.vue'

const { t } = useI18n(), router = useRouter()
const mobile = presentationProfile().ui === 'mobile'
const { currentSourceKey } = storeToRefs(useSourceStore())
const errors = useErrorStore()
// Mobile scenes inject their preview route before navigation is committed.
// Only the router's actual route may start or retain media playback.
const sceneActive = useViewActivity(() => router.currentRoute.value.path === '/short-drama')
const player = ref<InstanceType<typeof VideoPlayer> | null>(null)
const drawer = ref<'episodes' | 'intro' | ''>('')
const stage = ref<HTMLElement>()
const backdrop = ref<HTMLCanvasElement>()
const controlsHost = ref<HTMLElement>()
const overlayHost = ref<HTMLElement>()
let paintedAt = 0, backdropFailed = false
const entry = computed<ShortDramaEntry | null>(() => {
  const query = router.currentRoute.value.query
  if (typeof query.source !== 'string' || !query.source || typeof query.vod !== 'string' || !query.vod) return null
  const integer = (value: unknown): number => typeof value === 'string' && Number.isSafeInteger(Number(value)) && Number(value) >= 0 ? Number(value) : 0
  return {sourceKey:query.source, vodId:query.vod, globalId:integer(query.global_id), episodeIndex:integer(query.episode), lineIndex:query.line === undefined ? undefined : integer(query.line)}
})
const browsing = computed(() => shortDramaScene(router.currentRoute.value, mobile) === 'theatre')
const active = computed(() => sceneActive.value && !browsing.value)
function showTheatre():void { void router.push(mobile ? '/short-drama?view=theatre' : '/short-drama') }
function openSeries(item:Video):void { void router.push(getShortDramaPath(currentSourceKey.value,item)) }
function randomPlay():void { feed.discover(); void router.push(mobile ? '/short-drama' : '/short-drama?view=feed') }
watch(browsing,show=>{if(show)flush()},{flush:'sync'})
let wheelDistance = 0, wheelAt = 0, navigatedAt = -Infinity
function wheel(event: WheelEvent): void {
  if (mobile || !active.value || drawer.value || loading.value || Math.abs(event.deltaX) > Math.abs(event.deltaY)) return
  if ((event.target as Element).closest('button,input,[role="dialog"]')) return
  const now = performance.now()
  if (now - navigatedAt < 600) return
  if (now - wheelAt > 220) wheelDistance = 0
  wheelAt = now
  wheelDistance += event.deltaY * (event.deltaMode === 1 ? 16 : event.deltaMode === 2 ? 400 : 1)
  if (Math.abs(wheelDistance) < 70) return
  event.preventDefault()
  const direction = wheelDistance > 0 ? 'next' : 'previous'
  wheelDistance = 0; navigatedAt = now
  feed.step(direction)
}
function keydown(event: KeyboardEvent): void {
  if (!active.value || drawer.value || event.repeat || event.altKey || event.ctrlKey || event.metaKey) return
  if ((event.target as Element).closest('button,input,textarea,select,a,[contenteditable="true"]')) return
  if (['ArrowDown','PageDown','ArrowUp','PageUp'].includes(event.key)) {
    event.preventDefault()
    feed.step(event.key === 'ArrowDown' || event.key === 'PageDown' ? 'next' : 'previous')
  } else if (event.code === 'Space') { event.preventDefault(); player.value?.togglePlay() }
}
function flush(): void {
  const media = player.value?.video()
  if (media && Number.isFinite(media.currentTime)) history.syncHistoryToDb(media.currentTime, true)
  flushEpProgress()
  drawer.value = ''
}
const feed = useShortDramaFeed(currentSourceKey, active, flush, entry)
const { items, episodeIndex, video, episodes, episode, url, detail, loading, error, series, completed, canPrevious, canNext, effectiveSource } = feed
function discover(): void {
  if (entry.value) void router.push(mobile ? '/short-drama' : '/short-drama?view=feed')
  else feed.discover()
}
const history = useEpisodeHistory({
  identity: () => ({ sourceKey: feed.playbackSource.value, vodId: String(video.value?.vod_id ?? ''), name: video.value?.vod_name ?? '', globalId: video.value?.global_id }),
  episodes: () => episodes.value, currentIndex: () => episodeIndex.value,
  onPersistenceError: error => errors.fromError(t('shortDrama.historyFailed'), error, 'short-drama.history'),
})
const overview = computed(() => stripHtmlTags(video.value?.vod_content ?? ''))
const videoKey = computed(() => JSON.stringify(['short-drama', feed.playbackSource.value, video.value?.vod_id, episode.value?.ep_num]))
const {isFav,favBusy,showFavFolderModal,refreshFav,toggleFavorite,confirmAddToFolder}=usePlaybackFavorites(()=>({
  source_key:feed.playbackSource.value,vod_id:String(video.value?.vod_id??''),global_id:video.value?.global_id??0,vod_name:video.value?.vod_name??'',
}))
watch([active,video],([visible,item])=>{
  if(!mobile && visible && item)void refreshFav()
  if(!visible)showFavFolderModal.value=false
},{flush:'post'})
watch(videoKey, () => { paintedAt = 0; backdropFailed = false })
function progress(position: number, duration?: number): void {
  history.updateCurrentEpProgress(position, duration)
  const media = player.value?.video(), canvas = backdrop.value, now = performance.now()
  if (mobile || !active.value || backdropFailed || !canvas || !media || media.readyState < 2 || now - paintedAt < 400) return
  paintedAt = now
  // Reuse the player's progress cadence and a tiny canvas: no second decoder,
  // frame loop, image export or network request belongs to this backdrop.
  try { canvas.getContext('2d', {alpha:false})?.drawImage(media, Math.floor(media.videoWidth / 2), 0, 1, media.videoHeight, 0, 0, canvas.width, canvas.height) }
  catch { backdropFailed = true } // The static theatre remains usable if a host cannot draw video.
}
const episodeLabel = computed(() => episode.value?.ep_name || t('detail.episode', { num: episode.value?.ep_num ?? episodeIndex.value + 1 }))
watch(detail, value => {
  history.refreshEpProgressUI()
  if (!value) return
  TsCache.setEpisodes(episodes.value.map(ep => ({
    ...ep, ep_url: resolveEpisodeUrl(ep), source_key: feed.playbackSource.value, vod_id: String(video.value?.vod_id ?? ''),
  })))
  TsCache.setCurrentEpisode(episodeIndex.value)
})
watch(episodeIndex, index => { if (detail.value) TsCache.setCurrentEpisode(index) })
function selectEpisode(index: number): void { feed.selectEpisode(index); drawer.value = '' }
function backOverlay(event: Event): void {
  if (!active.value || !drawer.value) return
  event.preventDefault(); event.stopImmediatePropagation(); drawer.value = ''
}
window.addEventListener('cczj:android-overlay-back', backOverlay)
onBeforeUnmount(() => window.removeEventListener('cczj:android-overlay-back', backOverlay))
</script>

<template>
  <section ref="stage" class="short-drama cczj-relative cczj-flex cczj-flex-col cczj-min-h-0 cczj-overflow-hidden" :class="{ 'cczj-short-drama-surface': !browsing, 'short-drama-mobile': mobile, 'drama-drawer-open': !mobile && !!drawer }" :data-cczj-no-tab-swipe="browsing ? undefined : ''" @wheel="wheel" @keydown="keydown">
    <ShortDramaTheatre v-show="browsing" :active="sceneActive && browsing" :mobile="mobile" @play="openSeries" @random="randomPlay"/>
    <template v-if="!browsing">
    <canvas v-if="!mobile && active" ref="backdrop" class="drama-ambience cczj-absolute cczj-pointer-events-none" width="72" height="128" aria-hidden="true" />
    <header class="drama-header cczj-flex cczj-items-center cczj-justify-between cczj-gap-4" @pointerdown.stop>
      <div class="drama-heading cczj-flex cczj-items-center cczj-gap-5">
        <Button v-if="!mobile || entry" class="drama-return" variant="text" icon :aria-label="t('shortDrama.returnTheatre')" @click="showTheatre"><Icon name="chevron-left" :size="22"/></Button>
        <h1 v-if="!mobile">{{ t('shortDrama.title') }}</h1>
        <Button v-if="mobile && !entry" class="drama-mobile-tab drama-mobile-tab-active" variant="text" :aria-pressed="true" @click="discover">{{t('shortDrama.recommendTab')}}</Button>
        <Button v-if="mobile" class="drama-mobile-tab" variant="text" :aria-pressed="false" @click="showTheatre">{{t('shortDrama.theatre')}}</Button>
        <span v-if="!mobile || series">{{ t(series ? 'shortDrama.series' : 'shortDrama.forYou') }}</span>
      </div>
      <Button class="drama-refresh" variant="text" :aria-label="t(series ? 'shortDrama.backToFeed' : 'shortDrama.refresh')" :title="t(series ? 'shortDrama.backToFeed' : 'shortDrama.refresh')" :loading="loading && !series" :disabled="!effectiveSource" @click="series ? discover() : feed.refresh()"><Icon :name="series ? 'back' : 'refresh'" :size="20" /><span v-if="!mobile || series">{{ t(series ? 'shortDrama.backToFeed' : 'shortDrama.refresh') }}</span></Button>
    </header>
    <div class="drama-stage cczj-flex cczj-flex-1 cczj-items-center cczj-justify-center cczj-min-h-0">
      <div class="drama-frame cczj-relative">
      <div class="drama-screen cczj-h-full cczj-relative cczj-overflow-hidden">
        <VideoPlayer v-if="active && url && !loading && !error" ref="player" presentation="portrait" :portrait-controls-host="mobile ? undefined : controlsHost" :portrait-presentation-host="mobile ? undefined : stage" :url="url" :video-key="videoKey" :autoplay="true" :show-title-bar="false" :resolve-resume="history.resolveDbResumePosition" @swipe="feed.step" @progress="progress" @ended="feed.ended">
          <template #portrait-summary>
            <div class="drama-status-queue cczj-flex cczj-items-center cczj-gap-4 cczj-min-w-0"><Icon name="list" :size="16"/><span class="cczj-truncate">{{t(series ? 'shortDrama.series' : 'shortDrama.forYou')}} · {{t('shortDrama.currentEpisode',{num:episode?.ep_num??episodeIndex+1})}}</span></div>
          </template>
          <template #portrait-actions>
            <Button class="drama-status-episodes" variant="text" :aria-label="t('shortDrama.episodes')" @click="drawer='episodes'"><Icon name="list" :size="18"/><span>{{t('shortDrama.episodes')}}</span></Button>
          </template>
          <template #portrait-info>
            <div class="drama-info cczj-flex cczj-flex-col cczj-gap-2">
              <h2 class="drama-title"><Button class="drama-watch-series cczj-min-w-0 cczj-w-full" variant="text" :aria-label="t('shortDrama.watchSeries')" @click="series ? drawer = 'episodes' : feed.selectEpisode(episodeIndex)"><span class="cczj-line-clamp-2 cczj-min-w-0">{{ video?.vod_name }}</span><Icon name="chevron-right" :size="18" /></Button></h2>
              <span v-if="completed" class="drama-position" role="status">{{ t('shortDrama.completed') }}</span>
              <div class="drama-info-row cczj-flex cczj-items-center cczj-gap-4">
                <Button class="drama-intro cczj-flex-1 cczj-min-w-0" variant="text" :aria-label="t('shortDrama.intro')" @click="drawer = 'intro'"><span class="cczj-truncate">{{ overview || t('shortDrama.intro') }}</span><Icon name="chevron-right" :size="14" /></Button>
                <Button class="drama-episodes cczj-flex-shrink-0" variant="text" :aria-label="t('shortDrama.selectEpisode', { episode: episodeLabel })" :title="episodeLabel" @click="drawer = 'episodes'"><Icon name="list" :size="15" /><span>{{ t('shortDrama.currentEpisode', {num:episode?.ep_num ?? episodeIndex + 1}) }}</span><Icon name="chevron-up" :size="14" /></Button>
              </div>
            </div>
          </template>
        </VideoPlayer>
        <div v-else class="drama-placeholder cczj-text-white cczj-h-full cczj-flex cczj-flex-col cczj-items-center cczj-justify-center cczj-gap-6" role="status" aria-live="polite">
          <template v-if="loading"><Spinner /><p>{{ t('shortDrama.loading') }}</p></template>
          <Empty v-else :title="error ? t('shortDrama.loadFailed') : t('shortDrama.empty')" :description="error || t(effectiveSource ? 'shortDrama.emptyHint' : 'shortDrama.noSource')">
            <Button v-if="effectiveSource" @click="feed.retry">{{ t('common.retry') }}</Button>
            <router-link v-else class="cczj-text-accent" to="/sources">{{ t('sidebar.sources') }}</router-link>
          </Empty>
        </div>
        </div>
      </div>
      <div v-if="!mobile" ref="controlsHost" class="drama-controls-host cczj-absolute cczj-inset-0 cczj-pointer-events-none" />
      <nav v-if="!mobile && items.length" class="drama-switch cczj-absolute cczj-flex cczj-flex-col cczj-items-center cczj-gap-2" :aria-label="t(series ? 'shortDrama.switchEpisode' : 'shortDrama.switchDrama')">
        <Button variant="text" icon :disabled="!canPrevious" :aria-label="t(series ? 'shortDrama.previousEpisode' : 'shortDrama.previous')" :title="t(series ? 'shortDrama.previousEpisode' : 'shortDrama.previous')" @click="feed.step('previous')"><Icon name="chevron-up" :size="22" /></Button>
        <Button variant="text" icon :disabled="!canNext" :aria-label="t(series ? 'shortDrama.nextEpisode' : 'shortDrama.next')" :title="t(series ? 'shortDrama.nextEpisode' : 'shortDrama.next')" @click="feed.step('next')"><Icon name="chevron-down" :size="22" /></Button>
      </nav>
      <nav v-if="!mobile && video" class="drama-actions cczj-absolute cczj-flex cczj-flex-col cczj-gap-6" :aria-label="t('shortDrama.title')">
        <Button class="drama-action" variant="text" :disabled="favBusy" :aria-pressed="isFav" :aria-label="t(isFav?'detail.removeFav':'detail.addFav')" @click="toggleFavorite"><Icon name="star" :size="27"/><span>{{t('sidebar.favorites')}}</span></Button>
        <Button class="drama-action" variant="text" :aria-label="t('shortDrama.episodes')" @click="drawer='episodes'"><Icon name="list" :size="27"/><span>{{t('shortDrama.episodes')}}</span></Button>
        <Button class="drama-action" variant="text" :aria-label="t('shortDrama.intro')" @click="drawer='intro'"><Icon name="info" :size="27"/><span>{{t('shortDrama.intro')}}</span></Button>
      </nav>
    </div>
    <CczjMobilePlayerDrawer v-if="active" class="cczj-short-drama-surface" :open="!!drawer" :title="t(drawer === 'intro' ? 'shortDrama.intro' : 'shortDrama.episodes')" :fullscreen="false" :contained="!mobile" :host="stage" :placement="mobile ? 'bottom' : 'right'" :initial-height="72" solid lazy @close="drawer = ''">
      <div class="drama-drawer-content cczj-flex cczj-flex-col cczj-gap-6" :class="{'cczj-h-full cczj-min-h-0':drawer === 'episodes'}">
        <div class="drama-drawer-heading cczj-flex cczj-items-center cczj-gap-6 cczj-flex-shrink-0">
          <RemoteImage v-if="video?.vod_pic" class="drama-cover cczj-flex-shrink-0" :src="video.vod_pic" alt="" />
          <h2 class="drama-drawer-title cczj-line-clamp-2">{{ video?.vod_name }}</h2>
        </div>
        <template v-if="drawer === 'intro'"><p class="drama-overview">{{ overview || t('shortDrama.noIntro') }}</p><Button @click="drawer = 'episodes'">{{ t('shortDrama.episodes') }} · {{ episodes.length }}</Button></template>
        <ShortDramaCatalog v-else :episodes="episodes" :current="episodeIndex" :open="drawer === 'episodes'" :progress="history.getEpWatchPct" @select="selectEpisode" />
      </div>
    </CczjMobilePlayerDrawer>
    <FavFolderModal v-if="!mobile && active" v-model="showFavFolderModal" :teleport-to="overlayHost" @confirm="confirmAddToFolder" />
    </template>
    <div ref="overlayHost" />
  </section>
</template>

<style scoped>
.short-drama { height: 100%; color: var(--text-primary); --cczj-player-contained-width: min(360px,45%); }
.short-drama.cczj-short-drama-surface { background: var(--cczj-drama-chrome); }
.drama-ambience { inset: -12%; width: 124%; height: 124%; filter: blur(64px); opacity: .24; }
.drama-header { @apply cczj-absolute cczj-flex-shrink-0; inset: 0 0 auto; z-index: 40; padding: 12px 24px 36px; background: linear-gradient(#0007,transparent); pointer-events: none; }
.drama-header > * { @apply cczj-pointer-events-auto; }
.drama-heading h1 { margin: 0; font-size: 18px; font-weight: 600; letter-spacing: .02em; }
.drama-heading > span { color: #ffffffa6; font-size: 12px; }
.drama-return { min-width: 48px; min-height: 48px; border-radius: 50%; background: #0003; color: #ffffffbf; }
.drama-mobile-tab { @apply cczj-relative; min-height: 44px; padding: 4px 0; font-size: 17px; font-weight: 400; color: #ffffff9c; }
.drama-mobile-tab-active { color: #fff; font-weight: 600; }
.drama-mobile-tab-active::after { content: ''; @apply cczj-absolute cczj-bottom-0; left: calc(50% - 9px); width: 18px; height: 2px; background: currentColor; }
.drama-refresh { min-width: 44px; min-height: 44px; color: #ffffffc4; font-weight: 400; }
.drama-stage { @apply cczj-relative; container-type: size; padding: 0 76px 44px; }
.drama-frame { @apply cczj-flex-shrink-0; width: min(calc(100cqh * 9 / 16),100cqw); aspect-ratio: 9 / 16; }
.drama-screen { width: 100%; background: var(--cczj-drama-chrome); }
.drama-screen :deep(.portrait-player) { background: var(--cczj-drama-chrome); }
.drama-screen :deep(.native-video) { background: var(--cczj-drama-chrome); }
.drama-drawer-open .drama-stage,.drama-drawer-open .drama-header { margin-right: var(--cczj-player-contained-width); }
.drama-placeholder { padding: 16px; text-align: center; }
.drama-placeholder :deep(.empty-title), .drama-placeholder :deep(.empty-desc) { color: #e5e7eb; }
.drama-info { @apply cczj-min-w-0 cczj-w-full; text-shadow: 0 1px 5px #0009; transition: opacity .15s; }
.short-drama:not(.short-drama-mobile) .drama-info { max-width: min(360px,calc(100% - 64px)); }
.short-drama:not(.short-drama-mobile) .drama-title { font-size: 16px; }
.short-drama:not(.short-drama-mobile) .drama-info-row { @apply cczj-items-end; gap: 8px; }
.short-drama:not(.short-drama-mobile) .drama-intro span { white-space: normal; display: -webkit-box; -webkit-box-orient: vertical; -webkit-line-clamp: 3; line-height: 1.65; }
.short-drama:not(.short-drama-mobile) .drama-intro :deep(.ui-btn__content) { @apply cczj-items-end; }
.short-drama:not(.short-drama-mobile) .drama-intro { color: #e4e4e7; }
.drama-status-queue { color: #d4d4d8; font-size: 13px; }
.drama-status-episodes { min-height: 36px; font-size: 13px; color: #e5e7eb; }
.drama-title { margin: 0; font-size: 18px; font-weight: 600; line-height: 1.5; color: #fff; overflow-wrap: anywhere; }
.drama-watch-series { @apply cczj-justify-start; min-height: 44px; padding: 0; font: inherit; text-align: left; white-space: normal; color: #fff; }
.drama-watch-series :deep(.ui-btn__content) { @apply cczj-min-w-0; gap: 6px; }
.drama-watch-series :deep(.icon-svg) { @apply cczj-flex-shrink-0; }
.drama-info-row { @apply cczj-w-full; gap: 16px; }
.drama-intro { min-height: 44px; padding: 4px 0; font-size: 13px; font-weight: 400; color: #ffffffb3; text-align: left; }
.drama-intro :deep(.ui-btn__content) { @apply cczj-min-w-0 cczj-w-full; gap: 4px; }
.drama-intro :deep(.icon-svg) { @apply cczj-flex-shrink-0; }
.drama-episodes { min-height: 44px; padding: 4px 0; font-size: 13px; font-weight: 500; color: #ffffffe6; }
.drama-episodes :deep(.ui-btn__content) { @apply cczj-min-w-0; }
.drama-switch { top: 25%; right: 24px; transform: translateY(-50%); z-index: 32; padding: 4px; border: 1px solid #ffffff12; border-radius: 28px; background: #0002; color: #fff; }
.drama-switch .ui-btn { min-width: 44px; min-height: 44px; border-radius: 50%; color: #ffffffbf; }
.drama-actions { top: 41%; right: 24px; z-index: 32; }
.drama-action { min-width: 52px; min-height: 60px; padding: 6px; color: #ffffffe6; }
.drama-action :deep(.ui-btn__content) { @apply cczj-flex-col cczj-gap-3; font-size: 12px; font-weight: 400; }
.drama-action[aria-pressed=true] { color: #facc15; }
.drama-position { font-size: 12px; color: #ffffffb3; }
.drama-drawer-title { margin: 0; font-size: 17px; font-weight: 600; line-height: 1.5; overflow-wrap: anywhere; }
.drama-cover { width: 44px; height: 60px; border-radius: 6px; object-fit: cover; }
.drama-overview { line-height: 1.8; white-space: pre-wrap; overflow-wrap: anywhere; }
.short-drama-mobile .drama-stage { padding: 0; }
.short-drama-mobile .drama-frame { width: 100%; height: 100%; aspect-ratio: auto; }
.short-drama-mobile .drama-screen { border-radius: 0; }
.short-drama-mobile .drama-header { padding: 8px 16px 32px; background: linear-gradient(#000b,transparent); }
.short-drama-mobile .drama-heading h1 { font-size: 18px; }
.short-drama-mobile .drama-heading > span { font-size: 12px; }
.short-drama-mobile .drama-title { font-size: 17px; }
.short-drama-mobile .drama-return { min-width: 44px; min-height: 44px; background: transparent; }
</style>
