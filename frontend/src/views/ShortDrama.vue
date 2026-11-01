<script setup lang="ts">
defineOptions({ name: 'ShortDrama' })
import { computed, defineAsyncComponent, onBeforeUnmount, ref, shallowRef, watch, type ComputedRef } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { storeToRefs } from 'pinia'
import { useI18n } from 'vue-i18n'
import Icon from '../components/Icon.vue'
import RemoteImage from '../components/RemoteImage.vue'
import '../player/shortDrama/presentation.css'
import { Button, Empty } from '../components/ui'
import CczjMobilePlayerDrawer from '../mobile/CczjMobilePlayerDrawer.vue'
import { presentationProfile } from '../platform/presentation'
import { useSourceStore } from '../stores/source'
import { useErrorStore } from '../stores/error'
import { useViewActivity } from '../composables/useViewActivity'
import { useShortDramaSession } from '../player/shortDrama/session'
import { useShortDramaPaging } from '../player/shortDrama/useShortDramaPaging'
import { useDesktopPlaybackKeys } from '../player/page/useDesktopPlaybackKeys'
import { useEpisodeHistory } from '../player/page/useEpisodeHistory'
import { stripHtmlTags, resolveEpisodeUrl, getShortDramaPath, getDetailPath } from '../utils'
import type { Video } from '../types'
import { flushEpProgress } from '../utils/episodeProgress'
import { TsCache } from '../utils/tsCache'
import ShortDramaCatalog from '../player/shortDrama/ShortDramaCatalog.vue'
import ShortDramaTheatre from '../player/shortDrama/ShortDramaTheatre.vue'
import { shortDramaScene } from '../player/shortDrama/scene'
import { usePlaybackFavorites } from '../player/page/usePlaybackFavorites'
import FavFolderModal from '../components/FavFolderModal.vue'
import { useMobilePlayerGestures } from '../mobile/useMobilePlayerGestures'
import { createPlayerPlatform } from '../player/platform'

const { t } = useI18n(), router = useRouter(), route = useRoute()
// Empty feed/theatre browsing does not need to parse the media and GPU stack.
const VideoPlayer = defineAsyncComponent(() => import('../components/VideoPlayer.vue'))
const mobile = presentationProfile().ui === 'mobile'
const sources = useSourceStore()
const { currentSourceKey } = storeToRefs(sources)
const errors = useErrorStore()
// Mobile scenes inject their preview route before navigation is committed.
// Only the router's actual route may start or retain media playback.
const sceneActive = useViewActivity(() => router.currentRoute.value.path === '/short-drama', 'committed')
const sceneVisible = useViewActivity()
watch(sceneActive, active => {
  if (active) void sources.loadSources(true).catch(error => errors.fromError(t('errors.loadSourcesFailed'), error, 'short-drama.sources'))
}, { immediate: true })
const player = ref<InstanceType<typeof VideoPlayer> | null>(null)
const drawer = ref<'episodes' | 'intro' | ''>('')
const introExpanded=ref(false)
const drawerContent=ref<'episodes'|'intro'>('episodes')
watch(drawer,value=>{if(value)drawerContent.value=value},{flush:'sync'})
const stage = ref<HTMLElement>()
const backdrop = ref<HTMLCanvasElement>()
const controlsHost = ref<HTMLElement>()
const overlayHost = ref<HTMLElement>()
const playbackSurface = ref<HTMLElement>(), pagingFrame = ref<HTMLCanvasElement>()
const pagingCover = ref<HTMLElement>(), pagingChrome = ref<HTMLElement>()
let paging: ReturnType<typeof useShortDramaPaging>
let paintedAt = 0, backdropFailed = false
const feed = useShortDramaSession()
const browsing = computed(() => shortDramaScene(route, mobile) === 'theatre')
const active = computed(() => sceneActive.value && !browsing.value)
function showTheatre():void { void router.push(mobile ? '/short-drama?view=theatre' : '/short-drama') }
function openSeries(item:Video):void { void router.push(getShortDramaPath(item.source_key||currentSourceKey.value,item)) }
function randomPlay():void { feed.discover(); void router.push(mobile ? '/short-drama' : '/short-drama?view=feed') }
watch(browsing,show=>{if(show)flush()},{flush:'sync'})
let wheelDistance = 0, wheelAt = 0, navigatedAt = -Infinity
function wheel(event: WheelEvent): void {
  if (mobile || !active.value || drawer.value || loading.value || Math.abs(event.deltaX) > Math.abs(event.deltaY)) return
  if ((event.target as Element).closest('button,input,[role="dialog"],[data-short-drama-info]')) return
  const now = performance.now()
  if (now - navigatedAt < 600) return
  if (now - wheelAt > 220) wheelDistance = 0
  wheelAt = now
  wheelDistance += event.deltaY * (event.deltaMode === 1 ? 16 : event.deltaMode === 2 ? 400 : 1)
  if (Math.abs(wheelDistance) < 70) return
  event.preventDefault()
  const direction = wheelDistance > 0 ? 'next' : 'previous'
  wheelDistance = 0; navigatedAt = now
  paging.step(direction)
}
useDesktopPlaybackKeys({
  enabled:!mobile,video:()=>player.value?.video()??null,active:()=>active.value&&!drawer.value,
  hasPrev:()=>canPrevious.value,hasNext:()=>canNext.value,previous:()=>paging.step('previous'),next:()=>paging.step('next'),
  play:()=>player.value?.togglePlay(),fullscreen:()=>{void player.value?.toggleFullscreen()},pip:()=>{void player.value?.togglePiP()},verticalNavigation:true,
})
function flush(): void {
  const media = player.value?.video()
  if (media && player.value?.canPersistProgress() && Number.isFinite(media.currentTime)) history.syncHistoryToDb(media.currentTime, true)
  if(media&&!sceneActive.value)media.pause()
  flushEpProgress()
  drawer.value = '';introExpanded.value=false
  // A switch captures the outgoing decoder frame immediately before commit.
  // Ordinary history flushes and route departures do not start page motion.
  if (active.value) paging?.capture()
}
const stopObservingChange = feed.observeChange(flush)
onBeforeUnmount(() => { flush(); stopObservingChange() })
// Home can prepare the shared session while this scene is still leaving.
// Retain this scene's presentation until its last visible pixels leave; never
// pass a newly warmed URL into the outgoing decoder or its history identity.
const presentationKeys = ['items', 'episodeIndex', 'video', 'episodes', 'episode', 'url', 'detail', 'loading', 'selecting', 'error', 'series', 'completed', 'canPrevious', 'canNext', 'effectiveSource', 'playbackSource', 'entry'] as const
type Presentation = Pick<typeof feed, typeof presentationKeys[number]>
const snapshot = () => Object.fromEntries(presentationKeys.map(key => [key, feed[key]])) as Presentation
const presented = shallowRef(snapshot())
watch(() => [active.value, sceneVisible.value, ...presentationKeys.map(key => feed[key])], () => {
  if (active.value || !player.value || !sceneVisible.value) presented.value = snapshot()
}, { flush: 'pre' })
const presentation = Object.fromEntries(presentationKeys.map(key => [key, computed(() => presented.value[key])])) as { [K in keyof Presentation]: ComputedRef<Presentation[K]> }
const { items, episodeIndex, video, episodes, episode, url, detail, loading, selecting, error, series, completed, canPrevious, canNext, effectiveSource, playbackSource, entry } = presentation
paging = useShortDramaPaging({
  surface: playbackSurface, canvas: pagingFrame, cover: pagingCover, chrome: pagingChrome,
  video: () => player.value?.video() ?? null, active: () => active.value, visible: () => sceneVisible.value,
  canMove: direction => direction === 'next' ? canNext.value : canPrevious.value,
  step: direction => feed.step(direction),
  canRefresh: () => !feed.series && !feed.entry && feed.selected === 0,
  refresh: () => paging.replace(() => void feed.refresh(true)),
  timeout: () => errors.warn(t('shortDrama.loadFailed'), t('shortDrama.timeout'), '', 'short-drama.media'),
})
watch(error, value => { if (value) paging.reset() })
watch(loading, value => { if (!value && !paging.frozen.value) paging.reset() }, { flush: 'post' })
watch(currentSourceKey, () => paging.reset())
function ended(): void {
  if (series.value && episodeIndex.value + 1 >= episodes.value.length) feed.ended()
  else paging.step('next')
}
// Pause immediately on route exit, then retain the existing frame until the
// scene finishes leaving. A preview must never create a new player.
const hasPlayback = computed(() => (active.value||!!player.value&&sceneVisible.value) && !!url.value)
// The initial/empty page has no VideoPlayer, but owns the same pull-to-refresh
// gesture. The two owners attach to this surface exclusively.
useMobilePlayerGestures({
  enabled: mobile, interaction: 'portrait', device: createPlayerPlatform().device,
  surface: () => active.value && !hasPlayback.value ? stage.value : undefined,
  video: () => null, visible: () => {}, onVolume: () => {},
  onSwipe: paging.step, onSwipeProgress: paging.drag, onSwipeCancel: paging.cancelDrag,
})
function discover(): void {
  if (entry.value) void router.push(mobile ? '/short-drama' : '/short-drama?view=feed')
  else feed.discover()
}
async function dismissRecommendation():Promise<void>{
  const item=video.value,source=playbackSource.value
  if(!active.value||series.value||!item?.vod_id||feed.preferences.busy)return
  if(await feed.preferences.dismiss(source,String(item.vod_id))){
    if(active.value&&!series.value){paging.reset();await feed.refresh(true)}
  }
}
const history = useEpisodeHistory({
  identity: () => ({ sourceKey: playbackSource.value, vodId: String(video.value?.vod_id ?? ''), name: video.value?.vod_name ?? '', globalId: video.value?.global_id }),
  episodes: () => episodes.value, currentIndex: () => episodeIndex.value,
  onPersistenceError: error => errors.fromError(t('shortDrama.historyFailed'), error, 'short-drama.history'),
})
const overview = computed(() => {
  const value=stripHtmlTags(video.value?.vod_content??'').trim()
  const normalized=(text:string)=>text.normalize('NFKC').replace(/[\s\p{P}]/gu,'').toLowerCase()
  return normalized(value)===normalized(video.value?.vod_name??'')?'':value
})
const navigationLabel=computed(()=>t(series.value?'shortDrama.switchEpisode':'shortDrama.switchDrama'))
const previousLabel=computed(()=>t(series.value?'shortDrama.previousEpisodeShort':'shortDrama.previousShort'))
const nextLabel=computed(()=>t(series.value?'shortDrama.nextEpisodeShort':'shortDrama.nextShort'))
function openDetails(): void { if (video.value) void router.push(getDetailPath(playbackSource.value, video.value)) }
const videoKey = computed(() => JSON.stringify(['short-drama', playbackSource.value, video.value?.vod_id, episode.value?.ep_num]))
const {isFav,favBusy,showFavFolderModal,refreshFav,toggleFavorite,confirmAddToFolder}=usePlaybackFavorites(()=>({
  source_key:playbackSource.value,vod_id:String(video.value?.vod_id??''),global_id:video.value?.global_id??0,vod_name:video.value?.vod_name??'',
}))
watch([active,video],([visible,item])=>{
  if(!mobile && visible && item)void refreshFav()
  if(!visible)showFavFolderModal.value=false
},{flush:'post'})
watch(videoKey, () => { paintedAt = -Infinity;backdropFailed = false;introExpanded.value=false;const canvas=backdrop.value;if(canvas){canvas.width=1;canvas.height=1} })
function progress(position: number, duration?: number): void {
  if (!active.value) return
  if(position>0)feed.markWatched()
  history.updateCurrentEpProgress(position, duration)
  paintBackdrop()
}
function paintBackdrop():void {
  const media=player.value?.video(),canvas=backdrop.value,now=performance.now()
  if(mobile||!active.value||!sceneVisible.value||document.hidden||backdropFailed||!canvas||!media||media.readyState<2||!media.videoWidth||!media.videoHeight||now-paintedAt<400)return
  paintedAt=now
  const scale=160/Math.max(media.videoWidth,media.videoHeight)
  const width=Math.max(1,Math.round(media.videoWidth*scale)),height=Math.max(1,Math.round(media.videoHeight*scale))
  if(canvas.width!==width||canvas.height!==height){canvas.width=width;canvas.height=height}
  try {canvas.getContext('2d',{alpha:false})?.drawImage(media,0,0,width,height)}
  catch {backdropFailed=true}
}
function onMediaReady():void {void paging.ready();paintBackdrop()}

const episodeLabel = computed(() => episode.value?.ep_name || t('detail.episode', { num: episode.value?.ep_num ?? episodeIndex.value + 1 }))
watch([detail, series], ([value]) => {
  history.refreshEpProgressUI()
  if (!value) return
  TsCache.setEpisodes((series.value ? episodes.value : episode.value ? [episode.value] : []).map(ep => ({
    ...ep, ep_url: resolveEpisodeUrl(ep), source_key: playbackSource.value, vod_id: String(video.value?.vod_id ?? ''),
  })))
  TsCache.setCurrentEpisode(series.value ? episodeIndex.value : 0)
}, { immediate: true })
watch(episodeIndex, index => { if (detail.value) TsCache.setCurrentEpisode(series.value ? index : 0) })
function selectEpisode(index: number): void { feed.selectEpisode(index); drawer.value = '' }
watch(active, visible => { if (!visible) flush() }, { flush: 'sync' })
function backOverlay(event: Event): void {
  if (!active.value || (!drawer.value&&!introExpanded.value)) return
  event.preventDefault(); event.stopImmediatePropagation(); drawer.value = '';introExpanded.value=false
}
window.addEventListener('cczj:android-overlay-back', backOverlay)
onBeforeUnmount(() => window.removeEventListener('cczj:android-overlay-back', backOverlay))
</script>

<template>
  <section ref="stage" class="short-drama cczj-relative cczj-flex cczj-flex-col cczj-min-h-0 cczj-overflow-hidden" :class="{ 'cczj-short-drama-surface': hasPlayback || selecting, 'short-drama-mobile': mobile, 'drama-drawer-open': !mobile && !!drawer, 'drama-watching':!mobile&&hasPlayback&&!!player?.isPlaying&&!drawer }" :data-cczj-chrome="!browsing && (url || selecting) ? 'cinema' : undefined" :data-cczj-media-active="hasPlayback ? 'true' : undefined" :data-cczj-no-tab-swipe="hasPlayback ? '' : undefined" @wheel="wheel">
    <ShortDramaTheatre v-show="browsing" :active="sceneActive && browsing" :mobile="mobile" @play="openSeries" @random="randomPlay"/>
    <template v-if="!browsing">
    <canvas v-if="!mobile && hasPlayback" ref="backdrop" class="drama-ambience cczj-absolute cczj-pointer-events-none" width="1" height="1" aria-hidden="true" />
    <header class="drama-header cczj-flex cczj-items-center cczj-justify-between cczj-gap-4" @pointerdown.stop>
      <div class="drama-heading cczj-flex cczj-items-center cczj-gap-5">
        <Button v-if="!mobile || entry" class="drama-return" variant="text" icon :aria-label="t('shortDrama.returnTheatre')" @click="showTheatre"><Icon name="chevron-left" :size="22"/></Button>
        <h1 v-if="!mobile">{{ t(series ? 'shortDrama.series' : 'shortDrama.forYou') }}</h1>
        <Button v-if="mobile && !entry" class="drama-mobile-tab drama-mobile-tab-active" variant="text" :aria-pressed="true" @click="discover">{{t('shortDrama.recommendTab')}}</Button>
        <Button v-if="mobile" class="drama-mobile-tab" variant="text" :aria-pressed="false" @click="showTheatre">{{t('shortDrama.theatre')}}</Button>
        <span v-if="mobile && series">{{ t(series ? 'shortDrama.series' : 'shortDrama.forYou') }}</span>
      </div>
      <div class="cczj-flex cczj-items-center cczj-gap-4">
        <Button v-if="hasPlayback" variant="text" icon :aria-label="t('settings.playback')" :title="t('settings.playback')" @click="player?.openPlaybackSettings()"><Icon name="settings" :size="20" /></Button>
        <Button class="drama-refresh" variant="text" :aria-label="t(series ? 'shortDrama.backToFeed' : 'shortDrama.refresh')" :title="t(series ? 'shortDrama.backToFeed' : 'shortDrama.refresh')" :disabled="!effectiveSource || loading || paging.frozen.value" @click="series ? discover() : paging.replace(() => void feed.refresh())"><Icon :name="series ? 'back' : 'refresh'" :size="20" /><span v-if="!mobile || series">{{ t(series ? 'shortDrama.backToFeed' : 'shortDrama.refresh') }}</span></Button>
      </div>
    </header>
    <div class="drama-stage cczj-flex cczj-flex-1 cczj-items-center cczj-justify-center cczj-min-h-0">
      <div class="drama-frame cczj-relative">
      <div class="drama-screen cczj-h-full cczj-relative cczj-overflow-hidden">
        <div ref="playbackSurface" class="drama-playback-surface cczj-h-full cczj-w-full">
        <VideoPlayer v-if="hasPlayback" ref="player" presentation="portrait" quiet-loading :portrait-controls-host="mobile ? undefined : controlsHost" :portrait-presentation-host="stage" :portrait-controls-pinned="!!drawer || introExpanded" :url="url" :video-key="videoKey" :autoplay="true" :show-title-bar="false" :resolve-resume="history.resolveDbResumePosition" @gesture-start="paging.contact" @swipe="paging.step" @swipe-progress="paging.drag" @swipe-cancel="paging.cancelDrag" @media-ready="onMediaReady" @media-error="paging.reset" @progress="progress" @ended="ended">
          <template #portrait-summary>
            <div class="drama-status-queue cczj-flex cczj-items-center cczj-gap-4 cczj-min-w-0"><span class="cczj-truncate">{{t(series ? 'shortDrama.series' : 'shortDrama.forYou')}} · {{t('shortDrama.currentEpisode',{num:episode?.ep_num??episodeIndex+1})}}</span></div>
          </template>
          <template #portrait-actions>
            <Button class="drama-status-episodes" variant="text" :aria-label="t('shortDrama.episodes')" @click="drawer='episodes'"><Icon name="list" :size="18"/><span>{{t('shortDrama.episodes')}}</span></Button>
          </template>
          <template #portrait-info>
            <div data-short-drama-info class="drama-info cczj-flex cczj-flex-col cczj-gap-2" @click.stop @dblclick.stop @pointerdown.stop>
              <h2 class="drama-title cczj-line-clamp-2">{{ video?.vod_name }}</h2>
              <p v-if="!mobile" class="drama-meta">{{t('shortDrama.watchPosition',{episode:episodeLabel,count:episodes.length})}}</p>
              <div v-if="!series" class="cczj-flex cczj-flex-wrap cczj-items-center cczj-gap-4">
                <Button variant="text" :size="mobile?'md':'sm'" :disabled="loading" @click="feed.follow()">{{t('shortDrama.followSeries')}}</Button>
                <Button v-if="feed.preferences.undoItem" variant="text" :size="mobile?'md':'sm'" :loading="feed.preferences.busy" @click="feed.preferences.undo">{{t('shortDrama.undoDismiss')}}</Button>
                <Button variant="text" :size="mobile?'md':'sm'" :loading="feed.preferences.busy" :disabled="loading" @click="dismissRecommendation">{{t('shortDrama.notInterested')}}</Button>
                <Button v-if="feed.preferences.blocked.length" variant="text" :size="mobile?'md':'sm'" :disabled="feed.preferences.busy" @click="feed.preferences.restoreAll">{{t('shortDrama.restoreRecommendations',{count:feed.preferences.blocked.length})}}</Button>
              </div>
              <span v-if="completed" class="drama-position" role="status">{{ t('shortDrama.completed') }}</span>
              <div class="drama-info-row cczj-flex cczj-items-center cczj-gap-4">
                <div v-if="overview" class="drama-description cczj-min-w-0 cczj-flex-1">
                  <p class="drama-overview" :tabindex="introExpanded?0:undefined" :aria-label="t('shortDrama.intro')" @keydown.stop @keydown.escape.prevent="introExpanded=false" :class="{'cczj-line-clamp-2':!introExpanded,'drama-overview-expanded':introExpanded}">{{overview}}</p>
                  <Button class="drama-intro-toggle" variant="text" size="sm" :aria-expanded="introExpanded" @click="introExpanded=!introExpanded">{{t(introExpanded?'common.collapse':'common.expand')}}</Button>
                </div>
                <Button v-if="mobile" class="drama-episodes cczj-flex-shrink-0" variant="text" :aria-label="t('shortDrama.selectEpisode', { episode: episodeLabel })" :title="episodeLabel" @click="drawer = 'episodes'"><Icon name="list" :size="15" /><span>{{ t('shortDrama.currentEpisode', {num:episode?.ep_num ?? episodeIndex + 1}) }}</span><Icon name="chevron-up" :size="14" /></Button>
              </div>
            </div>
          </template>
        </VideoPlayer>
        <div v-else class="drama-placeholder cczj-h-full cczj-flex cczj-flex-col cczj-items-center cczj-justify-center cczj-gap-6" role="status" aria-live="polite" :aria-busy="loading">
          <Empty :title="error ? t('shortDrama.loadFailed') : loading ? video?.vod_name || t('shortDrama.forYou') : t('shortDrama.empty')" :description="error || t(loading ? 'shortDrama.preparingHint' : effectiveSource ? 'shortDrama.emptyHint' : 'shortDrama.noSource')">
            <Button v-if="feed.preferences.blocked.length" variant="text" :loading="feed.preferences.busy" @click="feed.preferences.restoreAll">{{t('shortDrama.restoreRecommendations',{count:feed.preferences.blocked.length})}}</Button>
            <Button v-if="effectiveSource && !loading" @click="feed.retry">{{ t('common.retry') }}</Button>
            <Button v-if="loading" variant="text" @click="showTheatre">{{ t('shortDrama.theatre') }}</Button>
            <router-link v-else-if="!error" class="cczj-text-accent" to="/sources">{{ t('sidebar.sources') }}</router-link>
          </Empty>
        </div>
        </div>
        <div v-show="paging.frozen.value" ref="pagingCover" class="drama-paging-frame cczj-absolute cczj-w-full cczj-h-full cczj-pointer-events-none cczj-top-0 cczj-left-0" inert aria-hidden="true"><canvas ref="pagingFrame" class="cczj-w-full cczj-h-full"/><div ref="pagingChrome"/></div>
        </div>
      </div>
      <div v-if="!mobile" ref="controlsHost" class="drama-controls-host cczj-absolute cczj-inset-0 cczj-pointer-events-none" />
      <nav v-if="!mobile && video" class="drama-rail cczj-absolute cczj-flex cczj-flex-col cczj-items-center cczj-gap-6" :aria-label="navigationLabel" @pointerdown.stop @wheel.stop>
        <div v-if="items.length" class="drama-step-actions cczj-flex cczj-flex-col cczj-gap-3" :aria-label="navigationLabel">
          <Button class="drama-action" variant="text" :disabled="!canPrevious" :aria-label="previousLabel" @click="paging.step('previous')"><Icon name="chevron-up" :size="24"/><span>{{previousLabel}}</span></Button>
          <Button class="drama-action" variant="text" :disabled="!canNext" :aria-label="nextLabel" @click="paging.step('next')"><Icon name="chevron-down" :size="24"/><span>{{nextLabel}}</span></Button>
        </div>
        <Button class="drama-action" variant="text" :disabled="favBusy" :aria-pressed="isFav" :aria-label="t(isFav?'detail.removeFav':'detail.addFav')" @click="toggleFavorite"><Icon name="star" :size="25"/><span>{{t('sidebar.favorites')}}</span></Button>
        <Button class="drama-action" variant="text" :aria-label="t('shortDrama.intro')" :aria-expanded="drawer==='intro'" @click="drawer='intro'"><Icon name="info" :size="25"/><span>{{t('shortDrama.intro')}}</span></Button>
      </nav>
    </div>
    <CczjMobilePlayerDrawer v-if="active" class="cczj-short-drama-surface" :open="!!drawer" :title="t(drawerContent==='intro'?'shortDrama.intro':'shortDrama.episodes')" :fullscreen="false" :contained="!mobile" :host="stage" :placement="mobile ? 'bottom' : 'right'" :initial-height="72" solid lazy @close="drawer = ''">
      <div class="drama-drawer-content cczj-flex cczj-flex-col cczj-gap-6 cczj-h-full cczj-min-h-0">
        <div class="drama-drawer-heading cczj-flex cczj-items-center cczj-gap-6 cczj-flex-shrink-0">
          <RemoteImage v-if="video?.vod_pic" class="drama-cover cczj-flex-shrink-0" :src="video.vod_pic" alt="" />
          <h2 class="drama-drawer-title cczj-line-clamp-2">{{ video?.vod_name }}</h2>
        </div>
        <template v-if="drawerContent==='intro'"><p class="drama-full-overview">{{overview||t('shortDrama.noIntro')}}</p><Button class="cczj-flex-shrink-0" @click="openDetails">{{t('shortDrama.fullDetails')}}</Button></template>
        <ShortDramaCatalog v-else :episodes="episodes" :current="episodeIndex" :open="drawer === 'episodes'" :progress="history.getEpWatchPct" @select="selectEpisode" />
      </div>
    </CczjMobilePlayerDrawer>
    <FavFolderModal v-if="!mobile && active" v-model="showFavFolderModal" :busy="favBusy" :teleport-to="overlayHost" @confirm="confirmAddToFolder" />
    </template>
    <div ref="overlayHost" />
  </section>
</template>

<style scoped>
.short-drama { height: 100%; color: var(--text-primary); --cczj-player-contained-width: min(360px,45%); }
.short-drama.cczj-short-drama-surface { background: var(--cczj-drama-chrome); }
.drama-ambience { inset:-8%;width:116%;height:116%;object-fit:cover;filter:blur(48px) saturate(.8) brightness(.85);opacity:.7; }
.drama-header { @apply cczj-absolute cczj-flex-shrink-0; inset: 0 0 auto; z-index: 40; padding: 12px 24px 36px; pointer-events: none; }
.cczj-short-drama-surface .drama-header { background: linear-gradient(#0007,transparent); }
.drama-header,.drama-rail { transition:opacity var(--cczj-motion-normal); }
.drama-watching .drama-header { opacity:.35; }
.drama-watching .drama-rail { opacity:.55; }
.drama-watching .drama-header:hover,.drama-watching .drama-header:focus-within,.drama-watching .drama-rail:hover,.drama-watching .drama-rail:focus-within { opacity:1; }
.drama-header > * { @apply cczj-pointer-events-auto; }
.drama-heading h1 { margin: 0; font-size: calc(18rem / 14); font-weight: 600; letter-spacing: .02em; }
.drama-heading > span { color: var(--text-muted); font-size: calc(12rem / 14); }
.drama-return { min-width: 48px; min-height: 48px; border-radius: 50%; background: var(--bg-hover); color: var(--text-secondary); }
.drama-mobile-tab { @apply cczj-relative; min-height: 44px; padding: 4px 0; font-size: calc(17rem / 14); font-weight: 400; color: var(--text-secondary); }
.drama-mobile-tab-active { color: var(--text-primary); font-weight: 600; }
.drama-mobile-tab-active::after { content: ''; @apply cczj-absolute cczj-bottom-0; left: calc(50% - 9px); width: 18px; height: 2px; background: currentColor; }
.drama-refresh { min-width: 44px; min-height: 44px; color: var(--text-secondary); font-weight: 400; }
.drama-stage { @apply cczj-relative; container-type: size; padding: 0 76px 44px; }
.drama-frame { @apply cczj-flex-shrink-0; width: min(calc(100cqh * 9 / 16),100cqw); aspect-ratio: 9 / 16; }
.drama-screen { width: 100%; }
.drama-playback-surface,.drama-paging-frame { background: var(--cczj-drama-chrome,#18191c); }
.drama-paging-frame { z-index: 30; }
.cczj-short-drama-surface .drama-screen { background: var(--cczj-drama-chrome); }
.drama-screen :deep(.portrait-player) { background: var(--cczj-drama-chrome); }
.drama-screen :deep(.native-video) { background: var(--cczj-drama-chrome); }
.drama-drawer-open .drama-stage,.drama-drawer-open .drama-header { margin-right: var(--cczj-player-contained-width); }
.drama-placeholder { padding: 16px; text-align: center; }
.drama-info { @apply cczj-min-w-0 cczj-w-full; text-shadow: 0 1px 5px #0009; transition: opacity .15s; }
.short-drama:not(.short-drama-mobile) .drama-info { max-width: min(480px,calc(50% - 32px)); }
.short-drama:not(.short-drama-mobile) .drama-title { font-size: calc(16rem / 14); }
.short-drama:not(.short-drama-mobile) .drama-info-row { @apply cczj-items-end; gap: 8px; }
.short-drama:not(.short-drama-mobile) .drama-intro :deep(.ui-btn__content) { @apply cczj-items-end; }
.short-drama:not(.short-drama-mobile) .drama-meta { margin:0;color:#d4d4d8;font-size:calc(13rem / 14);line-height:1.6; }
.drama-overview { margin:0;color:#e4e4e7;font-size:calc(14rem / 14);line-height:1.65;overflow-wrap:anywhere; }
.drama-overview:focus-visible { outline:2px solid currentColor;outline-offset:3px; }
.drama-overview-expanded { max-height:min(24vh,180px);@apply cczj-overflow-y-auto;overscroll-behavior:contain; }
.drama-intro-toggle { color:#fff; }
.drama-full-overview { @apply cczj-flex-1 cczj-min-h-0 cczj-overflow-y-auto;margin:0;line-height:1.85;white-space:pre-wrap;overflow-wrap:anywhere; }
.drama-episodes { min-height: 44px; padding: 4px 0; font-size: calc(13rem / 14); font-weight: 500; color: #ffffffe6; }
.drama-episodes :deep(.ui-btn__content) { @apply cczj-min-w-0; }
.drama-rail { top:50%;right:20px;transform:translateY(-50%);z-index:32; }
.drama-step-actions { padding-bottom:12px;border-bottom:1px solid #ffffff26; }
.drama-action { min-width: 52px; min-height: 60px; padding: 6px; color: #ffffffe6; }
.drama-action :deep(.ui-btn__content) { @apply cczj-flex-col cczj-gap-3; font-size: calc(12rem / 14); font-weight: 400; }
.drama-action:focus-visible { outline:2px solid currentColor;outline-offset:2px; }
.drama-action[aria-pressed=true] { color: #facc15; }
.drama-position { font-size: calc(12rem / 14); color: #ffffffb3; }
.drama-drawer-title { margin: 0; font-size: calc(17rem / 14); font-weight: 600; line-height: 1.5; overflow-wrap: anywhere; }
.drama-cover { width: 44px; height: 60px; border-radius: 6px; object-fit: cover; }
.short-drama:not(.short-drama-mobile) .drama-header,.drama-rail,.drama-meta,.drama-overview,.drama-full-overview,.drama-status-queue,.drama-status-episodes { font-family:system-ui,sans-serif; }
@media(max-width:760px){.short-drama:not(.short-drama-mobile) .drama-info { max-width:calc(100% - 96px); }.drama-rail { right:8px; }}
@media(max-height:620px){.drama-rail { gap:8px; }.drama-action { min-height:44px;padding:2px; }.drama-step-actions { gap:0;padding-bottom:4px; }}
.short-drama-mobile .drama-stage { padding: 0; }
.short-drama-mobile .drama-frame { width: 100%; height: 100%; aspect-ratio: auto; }
.short-drama-mobile .drama-screen { border-radius: 0; }
.short-drama-mobile .drama-header { padding: 8px 16px 32px; }
.short-drama-mobile.cczj-short-drama-surface .drama-header { background: linear-gradient(#000b,transparent); }
.short-drama-mobile .drama-heading h1 { font-size: calc(18rem / 14); }
.short-drama-mobile .drama-heading > span { font-size: calc(12rem / 14); }
.short-drama-mobile .drama-title { font-size: calc(17rem / 14); }
.short-drama-mobile .drama-info-row { gap: 12px; }
.short-drama-mobile .drama-stage { touch-action: none; @apply cczj-select-none; }
.short-drama-mobile .drama-return { min-width: 44px; min-height: 44px; background: transparent; }
</style>
