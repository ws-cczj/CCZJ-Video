<script setup lang="ts">
defineOptions({ name: 'Player' })
import { ref, computed, reactive, onMounted, onBeforeUnmount, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import { useRoute, useRouter } from 'vue-router'
import { GetRecentHistory, normalizeApiError } from '../api/app'
import * as AppMod from '../api/app'
import { useSourceStore } from '../stores/source'
import { useVideoStore } from '../stores/video'
import { useErrorStore } from '../stores/error'
import VideoPlayer from '../components/VideoPlayer.vue'
import Icon from '../components/Icon.vue'
import PlayerInformationPanel from '../player/views/PlayerInformationPanel.vue'
import PlaybackRouteList from '../player/views/PlaybackRouteList.vue'
import PlayerPanelToggle from '../player/views/PlayerPanelToggle.vue'
import type { PlayerPanelModel } from '../player/page/panelModel'
import DoubanComments from '../components/DoubanComments.vue'
import { Button, Modal } from '../components/ui'
import FavFolderModal from '../components/FavFolderModal.vue'
import { resolveEpisodeUrl, stripHtmlTags } from '../utils'
import { TsCache } from '../utils/tsCache'
import { loadEpProgress, mergeEpHistory, flushEpProgress } from '../utils/episodeProgress'
import { presentationProfile } from '../platform/presentation'
const mobile = presentationProfile().ui === 'mobile'
import { usePlaybackFavorites } from '../player/page/usePlaybackFavorites'
import { useSourceCatalog, type SourceOption } from '../player/page/useSourceCatalog'
import { findBestMatch, probeNameOf } from '../player/page/sourceMatching'
import { useEpisodeHistory } from '../player/page/useEpisodeHistory'
import { useDesktopPlaybackKeys } from '../player/page/useDesktopPlaybackKeys'
import type { HistoryItem } from '../types'
import { recordDiagnosticEvent } from '../diagnostics/recording'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const sourceStore = useSourceStore()
const videoStore = useVideoStore()
const errorStore = useErrorStore()
const playerView=ref<InstanceType<typeof VideoPlayer>|null>(null)

// ==================== 路由参数解析 ====================
const vodId = computed(() => {
  const p = route.query.vod || route.params.vodId
  if (Array.isArray(p)) return p[0] || ''
  return String(p || route.query.id || '')
})
const globalId = computed(() => Number(route.params.globalId || route.query.global_id || 0))

const sourceKey = computed(() => {
  const p = route.params.sourceKey
  if (Array.isArray(p)) return p[0] || ''
  return String(p || route.query.source || sourceStore.currentSourceKey || '')
})

const epIndexParam = computed(() => {
  const p = route.params.epIndex
  if (Array.isArray(p)) return parseInt(p[0] || '0', 10)
  return parseInt(String(p || route.query.ep || '0'), 10)
})

// ==================== 页面状态 ====================
const loading = ref(true)
const pageLoadStarted=performance.now()
function logPageStartup(stage:string):void {
  void AppMod.WriteLog({level:'INFO',source:'player',message:`[player.startup] page_${stage}`,detail:JSON.stringify({elapsed_ms:Math.round(performance.now()-pageLoadStarted)})}).catch(()=>{})
}
const video = computed(() => videoStore.currentVideo)
const episodes = computed(() => videoStore.episodes)

/** 当前正在播放的集数（0-based） */
const currentEpIndex = ref(-1) // 初始化为 -1，避免加载前错误触发第 0 集

/** 从右侧选集面板触发的集数切换信号（VideoPlayer 监听用） */
const _playToken = ref(0)

/** 当前集的播放地址 */
const currentUrl = computed(() => {
  const ep = episodes.value[currentEpIndex.value]
  return ep ? resolveEpisodeUrl(ep) : ''
})

/** 给视频组件一个"播放历史 key"，让进度记忆按集分开 */
const currentVideoKey = computed(() => {
  return `player_${String(vodId.value)}_${String(currentEpIndex.value)}`
})

// Source identity and real episode number isolate bindings from same-named items.
const danmakuKey=computed(()=>JSON.stringify(['video',sourceKey.value,String(vodId.value),episodes.value[currentEpIndex.value]?.ep_num??episodes.value[currentEpIndex.value]?.ep_name??currentEpIndex.value]))
const currentEpName = computed(() => {
  const ep = episodes.value[currentEpIndex.value]
  if (!ep) return ''
  if (ep.ep_name) return ep.ep_name
  const num = ep.ep_num ?? (currentEpIndex.value + 1)
  return t('detail.episode', { num })
})

const hasPrev = computed(() => currentEpIndex.value > 0)
const hasNext = computed(() => currentEpIndex.value < episodes.value.length - 1)

/* ==================== 侧面板收起/展开 ==================== */
const sidePanelCollapsed = ref(false)
const panelToggle = ref<InstanceType<typeof PlayerPanelToggle>|null>(null)

/* ==================== 源切换 ==================== */
/**
 * 一个源对"这部片在哪儿"的回答。
 * - local：本库里就有这一条（当前源，或和本条同 global_id 的源）
 * - remote：本库没有，去源站搜到了
 * - missing / failed：源站回答"没有" / 源站根本没答上来（失败的那只允许再点一次重试）
 */
const { sourceOptions, activeSourceKey, readySourceCount, probingCount,
  buildSourceOptions, probeLabel, sourceSelectable, searchCatalogSource, snapshot:sourceCatalogSnapshot } = useSourceCatalog({
  sources: () => sourceStore.sources,
  identity: () => ({sourceKey:sourceKey.value,vodId:String(vodId.value),name:video.value?.vod_name || ''}),
  episode:()=>Number(episodes.value[currentEpIndex.value]?.ep_num||0),
})
const sourceSearchLoading = ref(false)
/* ==================== 选集正序/倒序 ==================== */
const episodeSortAsc = ref(true)

function toggleEpisodeSort(): void {
  episodeSortAsc.value = !episodeSortAsc.value
}

const sortedEpisodes = computed(() => {
  const eps = [...episodes.value]
  if (!episodeSortAsc.value) eps.reverse()
  return eps
})

/** 排序索引 → 原始数组索引（倒序时映射回去） */
function origIdx(sortedI: number): number {
  return episodeSortAsc.value ? sortedI : episodes.value.length - 1 - sortedI
}

/* ==================== "已观看" 集数进度（从独立 localStorage 存储读取，1 个月自动淘汰） ====================
 * 键：`${sourceKey}-${vodId}-${epNum}`；值：{ position, duration?, updatedAt }
 * 每集独立一条记录，播放时持续写入，读取时自动淘汰过期记录。
 */
// 缓存每一集的进度百分比（0-100）
const { epKeyOf, refreshEpProgressUI, getEpWatchPct, isWatchedEp, syncHistoryToDb,
  resolveDbResumePosition, updateCurrentEpProgress, snapshot:historySnapshot } = useEpisodeHistory({
  identity: () => ({sourceKey:sourceKey.value,vodId:String(vodId.value),name:video.value?.vod_name || '',globalId:video.value?.global_id}),
  episodes: () => episodes.value, currentIndex: () => currentEpIndex.value,
})


/* ==================== 自定义快捷键 ==================== */
const mouseInside = ref(false)
useDesktopPlaybackKeys({
  enabled: !mobile, video: () => playerView.value?.video() || null,
  active: () => mouseInside.value, hasPrev: () => hasPrev.value, hasNext: () => hasNext.value,
  previous: prevEpisode, next: nextEpisode,
  fullscreen:()=>{void playerView.value?.toggleFullscreen()},pip:()=>{void playerView.value?.togglePiP()},
})

let pageDisposed = false

const {isFav,favBusy,showFavFolderModal,refreshFav,toggleFavorite,confirmAddToFolder}=usePlaybackFavorites(()=>({
  source_key:sourceKey.value,vod_id:String(vodId.value),global_id:video.value?.global_id||0,vod_name:video.value?.vod_name||'',
}))
const showCommentsModal=ref(false)
const playerOverlayTarget=ref<string|HTMLElement>('body')
watch([showCommentsModal,showFavFolderModal],([comments,favorite])=>{
  if(mobile&&(comments||favorite))playerOverlayTarget.value=playerView.value?.overlayHost()||'body'
})

/* ==================== 记录观看历史（点击就记录，无需等 30 秒 ==================== */
// ==================== 简介文本（去除 HTML 标签） ====================
const overviewText = computed(() => stripHtmlTags(video.value?.vod_content || ''))

// ==================== 集数切换 & 路由更新 ====================
function goToEpisode(idx: number): void {
  if (idx < 0 || idx >= episodes.value.length) return
  if (idx === currentEpIndex.value) return
  try {
    const v = playerView.value?.video()
    if (v && playerView.value?.canPersistProgress() && !isNaN(v.currentTime)) {
      syncHistoryToDb(v.currentTime, true, v.duration)
    }
  } catch { /* ignore */ }
  currentEpIndex.value = idx
  _playToken.value++
  try { TsCache.setCurrentEpisode(idx) } catch { }
  const canonicalID = video.value?.global_id || globalId.value || vodId.value
  router.replace(`/player/${sourceKey.value}/${canonicalID}/${idx}?vod=${encodeURIComponent(String(vodId.value))}`).catch(() => { })
}
function prevEpisode(): void { if (hasPrev.value) goToEpisode(currentEpIndex.value - 1) }
function nextEpisode(): void { if (hasNext.value) goToEpisode(currentEpIndex.value + 1) }

/**
 * 切到另一条播放线路：同一部影片的另一份集表，不用重新请求详情。
 *
 * 先记住当前集的 ep_num，切过去后按它找回同一集（找不到才回到首集）。ep_url 变了
 * VideoPlayer 会自己重建播放器并保住全屏，进度仍按 ep_num 从历史里续。
 */
function onLineChange(index: number): void {
  const prevEpNum = episodes.value[currentEpIndex.value]?.ep_num
  videoStore.setActiveLine(index)
  if (videoStore.activeLineIndex !== index) return
  const list = episodes.value
  if (list.length === 0) return
  const matched = prevEpNum == null ? -1 : list.findIndex((ep) => Number(ep.ep_num) === Number(prevEpNum))
  const targetIdx = matched >= 0 ? matched : 0
  currentEpIndex.value = targetIdx
  _playToken.value++
  try {
    TsCache.setEpisodes(
      list.map((ep) => ({
        source_key: sourceKey.value,
        vod_id: String(vodId.value),
        ep_url: resolveEpisodeUrl(ep),
        ep_name: ep.ep_name || '',
        ep_num: ep.ep_num ?? 0,
      })),
    )
    TsCache.setCurrentEpisode(targetIdx)
  } catch { /* 缓存映射失败不该挡住切线路 */ }
  const canonicalID = video.value?.global_id || globalId.value || vodId.value
  router.replace(`/player/${sourceKey.value}/${canonicalID}/${targetIdx}?vod=${encodeURIComponent(String(vodId.value))}`).catch(() => { })
}

function goBack(): void {
  router.back()
}

// ==================== 加载流程 ====================
async function loadData(): Promise<void> {
  if (pageDisposed || !sourceKey.value || (!vodId.value && !globalId.value)) return
  loading.value = true
  try {
    const currentVodId = video.value?.vod_id
    if (String(currentVodId || '') !== String(vodId.value)) {
      await videoStore.loadDetail(sourceKey.value, vodId.value, false, globalId.value)
    }
    if (pageDisposed) return
    if (!video.value) { loading.value = false; return }

    let targetIdx = 0
    if (!isNaN(epIndexParam.value) && epIndexParam.value >= 0 && epIndexParam.value < episodes.value.length) {
      targetIdx = epIndexParam.value
    } else {
      try {
        const history = (await GetRecentHistory(1)) as HistoryItem[] | null | undefined
        if (pageDisposed) return
        if (Array.isArray(history) && history.length > 0) {
          const last = history[0]
          // 优先按 global_id 跨源匹配，fallback 到 vod_id
          const globalMatch = video.value?.global_id ? (last.global_id === video.value.global_id) : null
          if (globalMatch === true || (globalMatch === null && String(last.vod_id) === String(vodId.value))) {
            const idx = episodes.value.findIndex((e) => Number(e.ep_num) === Number(last.ep_num))
            if (idx >= 0) targetIdx = idx
          }
        }
      } catch { }
    }
    currentEpIndex.value = targetIdx
    _playToken.value++

    try {
      TsCache.setEpisodes(
        episodes.value.map((ep) => ({
          source_key: sourceKey.value,
          vod_id: String(vodId.value),
          ep_url: resolveEpisodeUrl(ep),
          ep_name: ep.ep_name || '',
          ep_num: ep.ep_num ?? 0,
        })),
      )
      TsCache.setCurrentEpisode(targetIdx)
    } catch { }

    // 剧集加载完成后刷新播放进度（首次调用时 episodes 可能为空，此处补充）
    refreshEpProgressUI()
  } catch { }
  finally { loading.value = false }
}

// ==================== 源探测：先问本库，再问源站 ====================
/**
 * 进播放页时对"这部片还能在哪儿看"做一次探测，分两步：
 *  1) 本库：用 global_id 查哪些源已经有同一部片（纯本地查询，不花钱）。
 *  2) 源站：本库没有副本的那些源，各发一次源站搜索。
 * 第 2 步以前只在 global_id 查不到时才跑，于是绝大多数情况下探测直接早退，选源列表里
 * 永远只剩当前那一个源——本库里的片子彼此不同 global_id，跨源同片本来就少。
 * 现在两步都跑，结果逐条回写到对应的源上。
 */
/**
 * 源站探测命中的那条只存在于源站，本库目录里没有它；而 GetVideoDetail 必须先解析目录身份，
 * 于是直接切过去只会落到「暂无播放资源」。所以切换前把这一条按「入库」写进目录——
 * 与源搜索的入库按钮走同一个接口，只写目录字段、不带任何播放地址。
 */
async function ensureCatalogEntry(opt: SourceOption): Promise<void> {
  if (opt.probe !== 'remote' || !opt.hit) return
  try {
    await (AppMod as any).ImportSourceVideos(opt.source_key, [opt.hit])
    if (pageDisposed) return
    opt.probe = 'local'
  } catch (e) {
    console.log('[Player] 源站命中条目入库失败:', e)
  }
}

async function switchToSource(sk: string,lineIndex?:number): Promise<void> {
  if (pageDisposed || sourceSearchLoading.value || !sk) return
  if(sk===activeSourceKey.value){if(lineIndex!==undefined){const index=videoStore.lines.findIndex(line=>line.index===lineIndex);if(index>=0)onLineChange(index)};return}
  sourceSearchLoading.value = true
  try {
    // 探测已经拿到 vod_id 的源，直接切（来自本库命中或源站搜索）
    const existing = sourceOptions.value.find((s) => s.source_key === sk)
    if (existing?.vod_id) {
      await ensureCatalogEntry(existing)
      if (pageDisposed) return
      await loadFromSource(sk, existing.vod_id,lineIndex)
      return
    }
    // 回退：远端搜索该源中的同名视频
    const vodName = probeNameOf(video.value?.vod_name || '')
    if (!vodName) return
    const list = await searchCatalogSource(sk, vodName)
    if (pageDisposed) return
    const match = findBestMatch(list, vodName)
    if (!match?.vod_id) {
      console.log(`[Player] ❗ 源 "${sk}" 中未找到 "${vodName}"`)
      // 点了源却停在原源，用户看不出发生了什么，必须给出可见结论。
      errorStore.warn(t('player.sourceNotFound'), t('player.sourceNotFoundDetail', { source: sk }), '', 'Player')
      const idx = sourceOptions.value.findIndex((s) => s.source_key === sk)
      if (idx >= 0) sourceOptions.value[idx].probe = 'missing'
      return
    }
    const idx = sourceOptions.value.findIndex((s) => s.source_key === sk)
    if (idx >= 0) {
      sourceOptions.value[idx].vod_id = String(match.vod_id)
      sourceOptions.value[idx].probe = 'remote'
      sourceOptions.value[idx].hit = match
      await ensureCatalogEntry(sourceOptions.value[idx])
    }
    if (pageDisposed) return
    await loadFromSource(sk, String(match.vod_id),lineIndex)
  } catch (e) {
    if (pageDisposed) return
    console.error('[Player] 切换源失败:', e)
    errorStore.error(t('player.switchSourceFailed'), normalizeApiError(e).message, '', 'Player')
    const idx = sourceOptions.value.findIndex((s) => s.source_key === sk)
    if (idx >= 0 && sourceOptions.value[idx].source_key !== activeSourceKey.value) {
      sourceOptions.value[idx].probe = 'failed'
    }
  } finally {
    sourceSearchLoading.value = false
  }
}

async function loadFromSource(sk: string, vid: string,lineIndex?:number): Promise<void> {
  if (pageDisposed) return
  const prevEpNum = episodes.value[currentEpIndex.value]?.ep_num ?? undefined
  loading.value = true
  try {
    await videoStore.loadDetail(sk, vid)
    if (pageDisposed) return
    if(lineIndex!==undefined){const index=videoStore.lines.findIndex(line=>line.index===lineIndex);if(index>=0)videoStore.setActiveLine(index)}
    if (!video.value || !episodes.value.length) {
      loading.value = false
      errorStore.warn(t('player.sourceNoEpisodes'), t('player.sourceNoEpisodesDetail', { source: sk }), '', 'Player')
      return
    }
    activeSourceKey.value = sk

    // 尽量保持当前集数：按 ep_num 匹配，匹配不到则回退到第 0 集
    let targetIdx = 0
    if (prevEpNum != null) {
      const idx = episodes.value.findIndex((e) => Number(e.ep_num) === Number(prevEpNum))
      if (idx >= 0) targetIdx = idx
    }
    currentEpIndex.value = targetIdx
    _playToken.value++

    // 更新路由
    // The route's third segment is exclusively global_id. Source switching
    // starts from a source vod_id, so use the freshly resolved global_id and
    // retain vod_id in the compatibility query parameter.
    const canonicalID = video.value?.global_id || 0
    router.replace(`/player/${sk}/${canonicalID}/${targetIdx}?vod=${encodeURIComponent(String(vid))}`).catch(() => { })

    // 重新初始化 TsCache 集数映射（不清除，LRU 自动淘汰旧源的数据）
    try {
      TsCache.setEpisodes(
        episodes.value.map((ep) => ({
          source_key: sk,
          vod_id: String(vid),
          ep_url: resolveEpisodeUrl(ep),
          ep_name: ep.ep_name || '',
          ep_num: ep.ep_num ?? 0,
        })),
      )
      TsCache.setCurrentEpisode(targetIdx)
    } catch { }

    // 刷新进度和收藏状态
    refreshEpProgressUI()
    refreshFav().catch(() => { })
  } catch (e) {
    if (pageDisposed) return
    console.error('[Player] loadFromSource 失败:', e)
    errorStore.error(t('player.sourceLoadFailed'), normalizeApiError(e).message, '', 'Player')
  } finally {
    loading.value = false
  }
}

watch(currentEpIndex, (idx) => {
  // 播放中 → 把该集加入进度（即使没有具体位置，也标记为"开始看过"）
  try { TsCache.setCurrentEpisode(idx) } catch { }
})

// 同一视频内直接修改播放地址（例如浏览器前进/后退）时，页面实例会被保留，
// 因此需要将路由集数同步回播放器状态。通过播放器控制条切集时，当前集已先更新，
// 此处会自然跳过，避免重复加载。
watch(epIndexParam, (idx) => {
  if (!Number.isInteger(idx) || idx < 0 || idx >= episodes.value.length || idx === currentEpIndex.value) return
  currentEpIndex.value = idx
  _playToken.value++
  try { TsCache.setCurrentEpisode(idx) } catch { }
})

// 源切换完成后刷新选集进度；媒体进度由 VideoPlayer 统一上报。
watch(loading, (val) => {
  if (!val) {
    refreshEpProgressUI()
  }
})

// 页面可见性变化时刷新进度（从其他页面返回时同步）
function onVisibilityChange(): void {
  if (document.visibilityState === 'visible') {
    refreshEpProgressUI()
  }
}

onMounted(async () => {
  logPageStartup('mounted')
  await sourceStore.loadSources()
  if (pageDisposed) return
  logPageStartup('sources_ready')
  await loadData()
  if (pageDisposed) return
  logPageStartup('detail_ready')
  // 探测要打源站，慢的时候几秒才回一条，不能挡住播放器起播：结果自己逐条回填。
  void buildSourceOptions()
  // 从后端加载已观看集数（含 position）作为补充
  try {
    const history = await GetRecentHistory(50) as any[]
    if (pageDisposed) return
    if (Array.isArray(history) && history.length > 0) {
      const epNumToIdx = new Map<number, number>()
      for (let i = 0; i < episodes.value.length; i++) {
        const num = Number(episodes.value[i].ep_num)
        if (!isNaN(num)) epNumToIdx.set(num, i)
      }
      for (const h of history) {
        // 优先按 global_id 跨源匹配，fallback 到 vod_id
        const globalMatch = video.value?.global_id ? (h.global_id === video.value.global_id) : null
        const isMatch = (globalMatch === true) || (globalMatch === null && String(h.vod_id) === String(vodId.value))
        if (isMatch && h.ep_num != null) {
          const idx = epNumToIdx.get(Number(h.ep_num))
          if (idx !== undefined) {
            const k = epKeyOf(idx)
            // 仅在本地无数据时用后端历史作为占位（避免覆盖用户实际播放进度）
            mergeEpHistory(k,h)
          }
        }
      }
      refreshEpProgressUI()
    }
  } catch { /* ignore */ }
  if (pageDisposed) return
  // 刷新收藏状态
  refreshFav().catch(() => { })

  // 页面级键盘监听

  // 页面可见性变化时刷新进度（从其他页面返回时同步）
  document.addEventListener('visibilitychange', onVisibilityChange)
})

onBeforeUnmount(() => {
  pageDisposed = true
  // 让在飞的源站探测结果作废：离开页面之后再回写没有意义，也会拖住退出。
  flushEpProgress()
  try {
    const v = playerView.value?.video()
    if (v && playerView.value?.canPersistProgress() && !isNaN(v.currentTime)) {
      syncHistoryToDb(v.currentTime, true, v.duration)
      console.log(`[Player] ✔ 强制同步历史记录到后端: position=${v.currentTime.toFixed(1)}s`)
    }
  } catch { /* ignore */ }
  try { window.dispatchEvent(new CustomEvent('cczj-ep-progress-flushed')) } catch { /* ignore */ }
  document.removeEventListener('visibilitychange', onVisibilityChange)
  void recordDiagnosticEvent('playback_page_closed', {history:historySnapshot(),source_catalog:sourceCatalogSnapshot()})
})

/* ==================== 小工具：生成集数显示 ==================== */
function epLabel(i: number, ep: { ep_num?: number; ep_name?: string }): string {
  if (ep.ep_name) return ep.ep_name
  const num = ep.ep_num ?? (i + 1)
  return t('detail.episode', { num })
}

const panelModel:PlayerPanelModel = reactive({
  details: {
    get video(){return video.value},get overview(){return overviewText.value},
    get sourceKey(){return sourceKey.value},get vodId(){return String(vodId.value)},get globalId(){return globalId.value},
    get lines(){return videoStore.lines},get activeLineIndex(){return videoStore.activeLineIndex},
  },
  catalog: {
    get items(){return sourceOptions.value},get activeKey(){return activeSourceKey.value},
    get readyCount(){return readySourceCount.value},get probingCount(){return probingCount.value},
    get loading(){return sourceSearchLoading.value},label:probeLabel,selectable:sourceSelectable,retry:()=>{void buildSourceOptions()},
  },
  episodes: {
    get items(){return sortedEpisodes.value},get current(){return currentEpIndex.value},get ascending(){return episodeSortAsc.value},
    get currentNumber(){return Number(episodes.value[currentEpIndex.value]?.ep_num || 0)},
    originalIndex:origIdx,percent:getEpWatchPct,watched:isWatchedEp,label:epLabel,
  },
})
</script>

<template>
  <div class="player-page" :class="{ 'cczj-mobile-player': mobile }" @mouseenter="mouseInside = true" @mouseleave="mouseInside = false">
    <div v-if="loading" class="player-loading cczj-flex cczj-items-center cczj-justify-center cczj-gap-3">
      <div class="spinner"></div>
      <span>{{ t('common.loading') }}</span>
    </div>

    <template v-else-if="currentUrl">
      <div class="player-layout cczj-flex">
        <!-- ============= 左侧视频区 ============= -->
        <div class="player-col-main cczj-flex-1 cczj-relative" @mouseenter="mouseInside = true" @pointermove="panelToggle?.approach($event)" @dblclick.stop>
          <VideoPlayer ref="playerView" :url="currentUrl" :autoplay="true" :has-prev="hasPrev" :has-next="hasNext"
            :video-key="currentVideoKey" :danmaku-key="danmakuKey" :media-title="video?.vod_name||''" :media-year="video?.vod_year||''" :media-kind="video?.type_name||''" :episode-label="currentEpName" :show-title-bar="true" :title="mobile ? `${video?.vod_name || ''} · ${currentEpName}` : currentEpName" :is-fav="isFav"
            :fav-busy="favBusy" :douban-id="video?.vod_douban_id || ''"
            :resolve-resume="resolveDbResumePosition" :resume-immediately="route.query.resume === '1'"
            @progress="updateCurrentEpProgress"
            @toggle-favorite="toggleFavorite" @back="goBack" @prev="prevEpisode" @next="nextEpisode"
            @show-comments="showCommentsModal = true"
            :force-play-token="_playToken">
            <template v-if="mobile" #lines="{close}">
              <PlaybackRouteList mobile :sources="sourceOptions" :active-key="activeSourceKey" :active-line="videoStore.lines[videoStore.activeLineIndex]?.index??-1" :lines="videoStore.lines" :busy="sourceSearchLoading" :selectable="sourceSelectable" @choose="(key,line)=>{void switchToSource(key,line);close()}" />
              <Button class="cczj-mt-4" variant="overlay" @click="buildSourceOptions">{{t('playLine.retryQuality')}}</Button>
            </template>
            <template v-if="mobile && episodes.length" #episodes="{close}">
              <div class="cczj-mobile-drawer-episode-head"><span>{{t('detail.totalEpisodes',{count:episodes.length})}}</span><button @click="toggleEpisodeSort"><Icon :name="episodeSortAsc?'chevron-down':'chevron-up'" :size="14" />{{t(episodeSortAsc?'detail.ascOrder':'detail.descOrder')}}</button></div>
              <div class="cczj-mobile-drawer-episode-grid"><button v-for="(ep,i) in sortedEpisodes" :key="origIdx(i)" :aria-current="origIdx(i)===currentEpIndex?'step':undefined" :title="epLabel(origIdx(i),ep)" @click="goToEpisode(origIdx(i));close()"><span>{{epLabel(origIdx(i),ep)}}</span><small v-if="getEpWatchPct(origIdx(i))>0">{{Math.round(getEpWatchPct(origIdx(i)))}}%</small></button></div>
            </template>
          </VideoPlayer>


          <!-- 侧面板折叠/展开按钮（视频区右侧中间） -->
          <PlayerPanelToggle v-if="!mobile" ref="panelToggle" :collapsed="sidePanelCollapsed" @toggle="sidePanelCollapsed=!sidePanelCollapsed" />
        </div>

        <!-- ============= 右侧选集面板 ============= -->
        <PlayerInformationPanel :model="panelModel" :collapsed="sidePanelCollapsed" @source="switchToSource" @episode="goToEpisode" @sort="toggleEpisodeSort" @close="goBack" />
      </div>
    </template>

    <div v-else class="player-error-page cczj-flex cczj-flex-col cczj-items-center cczj-justify-center cczj-gap-4">
      <div class="error-msg cczj-text-lg cczj-text-muted">{{ t('player.noPlayableMedia') }}</div>
      <Button variant="text" size="md" @click="goBack"><span>{{ t('common.back') }}</span></Button>
    </div>

    <!-- 豆瓣评论弹窗 -->
    <Modal :teleport-to="playerOverlayTarget" :model-value="showCommentsModal" title="" width="680px" :show-footer="false"
      @update:model-value="(v: boolean) => !v && (showCommentsModal = false)">
      <DoubanComments v-if="video?.vod_douban_id && showCommentsModal" :douban-id="String(video.vod_douban_id)" />
    </Modal>

    <!-- 收藏夹选择弹窗 -->
    <FavFolderModal
      v-model="showFavFolderModal" :busy="favBusy"
      :teleport-to="playerOverlayTarget"
      @confirm="confirmAddToFolder"
    />
  </div>
</template>

<style scoped src="../styles/views/player.css"></style>
