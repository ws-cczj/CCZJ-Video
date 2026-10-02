<script setup lang="ts">
defineOptions({ name: 'Detail' })
import { ref, computed, onMounted, onBeforeUnmount, onActivated, watch, nextTick } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { GetRecentHistory, SaveWatchHistory, AddFavorite, RemoveFavorite, IsFavorite, DeleteVideo, GetVideoList, GetSimilarVideos, DoubanUpdateVideo, normalizeApiError, FindSourcesByGlobalId } from '../api/app'
import { useSourceStore } from '../stores/source'
import { useVideoStore } from '../stores/video'
import { useDownloadStore } from '../stores/download'
import { useConfirmStore } from '../stores/confirm'
import { useErrorStore } from '../stores/error'
import Icon from '../components/Icon.vue'
import PlayLinePicker from '../components/PlayLinePicker.vue'
import RemoteImage from '../components/RemoteImage.vue'
import { Button, Modal, Tag, Spinner as LoadingSpinner } from '../components/ui'
import { getDetailPath, getSearchPath, getPlayerPath, humanizeBytes, buildEpisodeFilename, buildSingleFilename, sanitizeFilename, resolveEpisodeUrl, stripHtmlTags } from '../utils'
import { TsCache } from '../utils/tsCache'
import { proxyHlsURL } from '../player/hls/proxy'
import { computeRecommendations, type RecommendItem, extractYear } from '../utils/recommend'
import { epProgressKey, loadEpProgress, getEpProgressPct, flushEpProgress } from '../utils/episodeProgress'
import { bumpFavoritesRefresh } from '../stores/favoritesSync'
import type { Video, HistoryItem, Episode } from '../types'
import { readStorage, writeStorage } from '../platform/storage'

const { t } = useI18n()

const route = useRoute()
const router = useRouter()
const sourceStore = useSourceStore()
const videoStore = useVideoStore()
const downloadStore = useDownloadStore()
const confirmStore = useConfirmStore()
const errorStore = useErrorStore()

// ==================== 路由参数解析 ====================
const vodId = computed(() => {
  const p = route.query.vod || route.params.vodId
  if (Array.isArray(p)) return p[0] || ''
  if (p) return String(p)
  return String(route.query.id || '')
})
const globalId = computed(() => Number(route.params.globalId || route.query.global_id || 0))

const sourceKey = computed(() => {
  const p = route.params.sourceKey
  if (Array.isArray(p)) return p[0] || ''
  if (p) return String(p)
  return String(route.query.source || sourceStore.currentSourceKey || '')
})

// ==================== 页面状态 ====================
const loading = ref(false)
const refreshing = ref(false) // 后台刷新标记
const error = ref<string | null>(null)
const video = computed(() => videoStore.currentVideo)

/* ==================== 每集播放进度（独立存储，超过 1 个月自动淘汰） ====================
 * - 数据存于 localStorage key: cczj_ep_prog_v1
 * - 每项：{ position, duration?, updatedAt }
 * - 同时也会读取后端最近历史作为补充（在本地进度不存在时写入一条）。
 */
const epProgressMap = ref<Record<string, number>>({}) // key -> 0-100
const lastWatched = ref<{ epNum: number; epIdx: number; epName: string; position: number } | null>(null)

function epKeyOf(ep: Episode | undefined | null, idx: number): string {
  return epProgressKey(video.value?.global_id, video.value?.vod_name, ep?.ep_num ?? idx)
}

function refreshEpProgress(): void {
  try {
    // 先强制写入确保内存缓存已同步到 localStorage
    flushEpProgress()
    const local = loadEpProgress()
    const out: Record<string, number> = {}
    for (const k of Object.keys(local)) {
      out[k] = getEpProgressPct(local[k])
    }
    epProgressMap.value = out
    // Debug: 检查当前视频的进度是否正确加载
    const vName = video.value?.vod_name
    const gid = video.value?.global_id
    if (vName) {
      const prefix = gid ? String(gid) : vName
      const epKeys = Object.keys(out).filter(k => k.startsWith(prefix + '-'))
      if (epKeys.length > 0) {
        console.log(`[Detail] ✔ 进度已加载: global_id=${gid}, vodName="${vName}", 已观看 ${epKeys.length} 集`, epKeys.map(k => `${k}=${Math.round(out[k])}%`).join(', '))
      } else {
        console.log(`[Detail] ⚠ 未找到进度: global_id=${gid}, vodName="${vName}", localStorage 中共 ${Object.keys(out).length} 条记录`)
      }
    }
  } catch (e) { console.warn('[Detail] refreshEpProgress 失败:', e) }
}
refreshEpProgress()

// 当视频或剧集列表变化时自动刷新进度（处理异步加载的时序问题）
watch(
  () => [video.value?.vod_name, videoStore.episodes.length],
  () => {
    if (video.value?.vod_name && videoStore.episodes.length > 0) {
      nextTick(() => refreshEpProgress())
    }
  },
)

// 页面可见性变化时刷新进度（从播放页返回时同步）
function onVisibilityChange(): void {
  if (document.visibilityState === 'visible') {
    refreshEpProgress()
    refreshLastWatched()
  }
}

// 自定义事件监听：播放页实时同步进度到详情页
function onEpProgressUpdated(): void {
  flushEpProgress() // 确保内存缓存已写入 localStorage
  refreshEpProgress()
}
function onEpProgressFlushed(): void {
  refreshEpProgress()
  refreshLastWatched()
}

// localStorage 变化监听（跨标签页同步进度）
function onStorageChange(e: StorageEvent): void {
  if (e.key === 'cczj_ep_prog_v1') {
    // 其他标签页修改了进度数据，重新加载
    refreshEpProgress()
  }
}

async function refreshLastWatched(): Promise<void> {
  try {
    const history = (await GetRecentHistory(50)) as HistoryItem[] | null | undefined
    const eps = videoStore.episodes
    const epNumToIdx = new Map<number, number>()
    for (let i = 0; i < eps.length; i++) {
      const num = Number(eps[i].ep_num)
      if (!isNaN(num)) epNumToIdx.set(num, i)
    }

    let found: HistoryItem | null = null
    if (Array.isArray(history) && history.length > 0) {
      const matches = history.filter((h) => {
        if (h.ep_num == null) return false
        if (video.value?.global_id) return h.global_id === video.value.global_id
        return String(h.vod_id) === String(vodId.value)
      })
      // 回到真正看过的那一集：history 按时间倒序，只点了几秒的记录会把"继续观看"带回片头。
      found = matches.find((h) => Number(h.position) > 5) || matches[0] || null
    }

    if (found && found.ep_num != null) {
      const idx = epNumToIdx.get(Number(found.ep_num))
      if (idx !== undefined) {
        const ep = videoStore.episodes[idx]
        lastWatched.value = {
          epNum: Number(found.ep_num),
          epIdx: idx,
          epName: formatEpisodeName(ep, idx),
          // 后端保存的是秒数；按钮展示的是百分比，不能直接把秒数当百分比。
          position: getEpProgressPct(loadEpProgress()[epKeyOf(ep, idx)]),
        }
        return
      }
    }

    const localStore = loadEpProgress()
    let maxUpdatedAt = 0
    let bestKey: string | null = null
    for (const k of Object.keys(localStore)) {
      const prog = localStore[k]
      if (prog.updatedAt && prog.updatedAt > maxUpdatedAt && prog.position && prog.position > 0) {
        maxUpdatedAt = prog.updatedAt
        bestKey = k
      }
    }

    if (bestKey) {
      for (let i = 0; i < eps.length; i++) {
        const k = epKeyOf(eps[i], i)
        if (k === bestKey) {
          lastWatched.value = {
            epNum: Number(eps[i].ep_num) || (i + 1),
            epIdx: i,
            epName: formatEpisodeName(eps[i], i),
            position: getEpProgressPct(localStore[bestKey]),
          }
          return
        }
      }
    }

    lastWatched.value = null
  } catch { /* ignore */ }
}

function getEpPct(ep: Episode | undefined | null, idx: number): number {
  return epProgressMap.value[epKeyOf(ep, idx)] ?? 0
}
function isWatched(ep: Episode | undefined | null, idx: number): boolean {
  return getEpPct(ep, idx) > 0
}

// 简介展开 + 去 HTML 标签
const expandOverview = ref(false)
const overviewText = computed(() => stripHtmlTags(video.value?.vod_content || ''))

/* ==================== 收藏状态 ==================== */
const isFav = ref(false)
const favBusy = ref(false)
const showFavFolderModal = ref(false)

interface FavFolder { id: string; name: string; default: boolean }
const favFolders = ref<FavFolder[]>([
  { id: 'default', name: t('detail.defaultFolder'), default: true },
])
const favTargetFolderId = ref<string>('default')

function loadFavFolders(): void {
  try {
    const raw = JSON.stringify(readStorage<unknown>('cczj_fav_folders', null))
    if (raw) {
      const parsed = JSON.parse(raw) as FavFolder[]
      if (Array.isArray(parsed) && parsed.length > 0) {
        favFolders.value = parsed
        return
      }
    }
  } catch { /* ignore */ }
  favFolders.value = [{ id: 'default', name: t('detail.defaultFolder'), default: true }]
}

async function refreshFav(): Promise<void> {
  if (!vodId.value) return
  try {
    const val = await IsFavorite({ source_key: sourceKey.value, vod_id: String(vodId.value), global_id: video.value?.global_id || 0 }) as boolean
    isFav.value = !!val
  } catch { /* ignore */ }
}
async function toggleFavorite(): Promise<void> {
  if (!vodId.value || favBusy.value) return
  // 若已收藏 → 直接取消
  if (isFav.value) {
    favBusy.value = true
    try {
      await RemoveFavorite({ source_key: sourceKey.value, vod_id: String(vodId.value), global_id: video.value?.global_id || 0 })
      // 清除 mapping
      const key = `${sourceKey.value}-${vodId.value}`
      try {
        const raw = JSON.stringify(readStorage<Record<string, string>>('cczj_fav_mapping', {}))
        if (raw) {
          const obj = JSON.parse(raw) as Record<string, string>
          delete obj[key]
          writeStorage('cczj_fav_mapping', obj)
        }
      } catch { /* ignore */ }
      isFav.value = false
      bumpFavoritesRefresh()
    } catch (e) {
      // 点「取消收藏」却什么都没发生，是最容易被当成软件坏了的静默失败。
      errorStore.error(t('detail.favRemoveFailed'), normalizeApiError(e).message, '', 'Detail')
    } finally {
      favBusy.value = false
    }
    return
  }
  // 未收藏 → 先展示选择文件夹弹窗
  loadFavFolders()
  favTargetFolderId.value = 'default'
  showFavFolderModal.value = true
}

async function confirmAddToFolder(): Promise<void> {
  if (!vodId.value) return
  favBusy.value = true
  showFavFolderModal.value = false
  try {
    await AddFavorite({
      source_key: sourceKey.value,
      vod_id: String(vodId.value),
      vod_name: video.value?.vod_name || '',
    } as any)
    // 写入 mapping：关联到选择的文件夹
    try {
      const key = `${sourceKey.value}-${vodId.value}`
      const raw = JSON.stringify(readStorage<Record<string, string>>('cczj_fav_mapping', {}))
      const obj: Record<string, string> = raw ? JSON.parse(raw) : {}
      obj[key] = favTargetFolderId.value
      writeStorage('cczj_fav_mapping', obj)
    } catch { /* ignore */ }
    isFav.value = true
    bumpFavoritesRefresh()
  } catch (e) {
    errorStore.error(t('detail.favAddFailed'), normalizeApiError(e).message, '', 'Detail')
  } finally {
    favBusy.value = false
  }
}

// ==================== 元数据标签 ====================
const directorList = computed(() => {
  if (!video.value?.vod_director) return []
  return video.value.vod_director.split(/[,，\/、]+/).map((s) => s.trim()).filter(Boolean)
})

const actorList = computed(() => {
  if (!video.value?.vod_actor) return []
  return video.value.vod_actor.split(/[,，\/、]+/).map((s) => s.trim()).filter(Boolean)
})

// ==================== 扩展元数据（评分/热度/信息） ====================
const hasMetaRow = computed(() => {
  const v = video.value
  if (!v) return false
  return !!(v.vod_douban_score || v.vod_score || v.vod_hits)
})

const hasInfoRow = computed(() => {
  const v = video.value
  if (!v) return false
  return !!(v.vod_version || v.vod_state || v.vod_isend || v.vod_pubdate || videoStore.lines.length > 1)
})

function formatHits(raw: string | undefined | null): string {
  if (!raw) return ''
  const n = parseInt(raw, 10)
  if (isNaN(n)) return raw
  if (n >= 100000000) return (n / 100000000).toFixed(1) + t('detail.hundredMillion')
  if (n >= 10000) return (n / 10000).toFixed(1) + t('detail.tenThousand')
  if (n >= 1000) return (n / 1000).toFixed(1) + 'k'
  return String(n)
}

function searchByKeyword(kw: string | undefined | null): void {
  if (!kw) return
  router.push(getSearchPath(kw, sourceKey.value))
}

// ==================== 下载弹窗 ====================
const showDownloadModal = ref(false)
const downloadError = ref<string | null>(null)
const downloadingEpKeys = ref<Set<string>>(new Set())

function openDownload(): void {
  showDownloadModal.value = true
  downloadError.value = null
  if (!downloadStore.dir) {
    try { downloadStore.init() } catch { }
  }
}

function closeDownload(): void {
  showDownloadModal.value = false
}

function epKey(ep: Episode): string {
  return `${String(video.value?.vod_id || '')}_${ep.ep_num}_${ep.ep_url}`
}

async function downloadEpisode(ep: Episode): Promise<void> {
  if (!video.value || !ep.ep_url) return
  const key = epKey(ep)
  if (downloadingEpKeys.value.has(key)) return

  // 检查是否已有下载任务（重复下载提示覆盖）
  const existing = downloadStore.findStatusForVideo({ vod_id: String(video.value.vod_id || ''), ep_num: ep.ep_num })
  if (existing) {
    const ok = await confirmStore.confirm({
      title: t('detail.duplicateDownload'),
      message: t('detail.duplicateDownloadMessage', { name: ep.ep_name || t('detail.episode', { num: ep.ep_num }) }),
      okText: t('detail.duplicateDownloadOk'),
      cancelText: t('common.cancel'),
    })
    if (!ok) return
  }

  downloadingEpKeys.value.add(key)
  try {
    const fn = buildEpisodeFilename(video.value.vod_name || t('search.video'), ep.ep_num, ep.ep_name, ep.ep_url)
    await downloadStore.startDownload({
      url: resolveEpisodeUrl(ep),
      filename: fn,
      vod_name: video.value.vod_name,
      ep_name: ep.ep_name,
      source_key: sourceKey.value,
      vod_id: String(video.value.vod_id || ''),
      ep_num: ep.ep_num,
      force: true,
    })
  } catch (e: any) {
    const msg = normalizeApiError(e).message || t('detail.downloadFailed')
    if (msg !== t('common.cancel')) downloadError.value = msg
  } finally {
    setTimeout(() => downloadingEpKeys.value.delete(key), 200)
  }
}

async function downloadAllEpisodes(): Promise<void> {
  if (videoStore.episodes.length === 0) return
  const ok = await confirmStore.confirm({
    title: t('detail.downloadAll'),
    message: t('detail.downloadAllMessage', { count: videoStore.episodes.length }),
    okText: t('detail.downloadAllOk'),
    cancelText: t('common.cancel'),
  })
  if (!ok) return
  for (const ep of videoStore.episodes) {
    downloadEpisode(ep)
  }
}

// ==================== 剧集播放/下载模式 ====================
const episodeMode = ref<'play' | 'download'>('play') // 'play'=播放, 'download'=下载
const episodeSortAsc = ref(true) // true=正序, false=倒序

function toggleEpisodeMode(): void {
  episodeMode.value = episodeMode.value === 'play' ? 'download' : 'play'
}

function toggleEpisodeSort(): void {
  episodeSortAsc.value = !episodeSortAsc.value
}

// 排序后的剧集列表
const sortedEpisodes = computed(() => {
  const eps = [...videoStore.episodes]
  if (!episodeSortAsc.value) eps.reverse()
  return eps
})

/** 排序索引 → 原始数组索引（倒序时映射回去） */
function origIdx(sortedI: number): number {
  return episodeSortAsc.value ? sortedI : videoStore.episodes.length - 1 - sortedI
}

function onEpisodeClick(sortedI: number, ep: Episode): void {
  if (episodeMode.value === 'download') {
    downloadEpisode(ep)
    return
  }
  playEpisode(origIdx(sortedI))
}

function playEpisode(epIndex: number): void {
  const v = video.value
  if (!v) return
  // 点击即记录观看历史
  try {
    const ep = videoStore.episodes[epIndex]
    const epNum = ep?.ep_num ?? (epIndex + 1)
    const progKey = epProgressKey(v.global_id, v.vod_name, epNum)
    const entry = loadEpProgress()[progKey]
    SaveWatchHistory({
      source_key: sourceKey.value,
      vod_id: String(vodId.value),
      vod_name: v.vod_name || '',
      ep_num: epNum,
      position: entry?.position ?? 0,
    } as any).catch(() => { })
    // 预取点击剧集的 m3u8 文本（轻量，跳转到播放页时可命中缓存）
    TsCache.setCurrentEpisode(epIndex)
    const epUrl = resolveEpisodeUrl(ep)
    if (epUrl) TsCache.fetchAndParseM3u8(proxyHlsURL(epUrl)).catch(() => { })
  } catch { /* ignore */ }
  router.push(getPlayerPath(sourceKey.value, v, epIndex))
}

function playFromHistory(): void {
  // 尝试恢复历史记录
  GetRecentHistory(1).then((history) => {
    const list = history as HistoryItem[] | null | undefined
    if (Array.isArray(list) && list.length > 0) {
      const last = list[0]
      if (String(last.vod_id) === String(vodId.value)) {
        const idx = videoStore.episodes.findIndex((e) => Number(e.ep_num) === Number(last.ep_num))
        if (idx >= 0) {
          playEpisode(idx)
          return
        }
      }
    }
    // 默认播放第1集
    playEpisode(0)
  }).catch(() => {
    playEpisode(0)
  })
}

function formatEpisodeName(ep: Episode, i: number): string {
  if (ep.ep_name) return ep.ep_name
  const num = ep.ep_num ?? (i + 1)
  return t('detail.episode', { num: String(num) })
}

// ==================== 类似推荐 ====================
const similarVideos = ref<RecommendItem[]>([])
const similarLoading = ref(false)
const MAX_SIMILAR_INITIAL = 7

const displayedSimilar = computed(() => {
  return similarVideos.value.slice(0, MAX_SIMILAR_INITIAL)
})

const hasMoreSimilar = computed(() => similarVideos.value.length > MAX_SIMILAR_INITIAL)

function goToRecommendations(): void {
  const v = video.value
  if (!v) return
  router.push({
    path: '/recommendations',
    query: {
      sourceKey: sourceKey.value,
      vodId: String(v.vod_id || ''),
      vodName: v.vod_name || '',
    },
  })
}

async function loadSimilar(): Promise<void> {
  if (!sourceKey.value || !video.value) return
  similarLoading.value = true
  try {
    // 候选池自己取同类型的一页：原先读的是别的列表页留下的 videoStore.videos，
    // 从哪个页面进来就推荐那个页面的内容，直接打开详情页则什么都没有。
    const typeId = video.value.type_id ? String(video.value.type_id) : ''
    const pool = (await (GetVideoList as any)({
      source_key: sourceKey.value,
      type_id: typeId,
      year: '',
      area: '',
      keyword: '',
      sort: '',
      recent_days: 0,
      cursor: '',
      page: 1,
      page_size: 100,
    })) as any
    const list: Video[] = Array.isArray(pool?.videos) ? pool.videos : []
    const currentId = String(video.value.vod_id || '')
    const currentName = video.value.vod_name || ''
    const year = extractYear(video.value.vod_year)

    // 1. 前端基于内容的推荐
    let results = computeRecommendations(list, currentId, currentName, year)
    
    // 2. 如果前端推荐为空，调用后端兜底（按类型推荐）
    if (results.length === 0) {
      try {
        const similar = await GetSimilarVideos({
          source_key: sourceKey.value,
          type_id: typeId,
          limit: 8,
          exclude_ids: [currentId]
        })
        
        if (similar && similar.length > 0) {
          // 转换为 RecommendItem 格式
          results = similar.map((v: any) => ({
            vod_id: String(v.vod_id || ''),
            vod_name: v.vod_name || '',
            vod_pic: v.vod_pic,
            vod_remarks: v.vod_remarks,
            score: 0,
            matchKey: t('detail.sameTypeRecommend')
          }))
        }
      } catch (e) {
        console.warn('后端相似推荐失败', e)
        // 前端算不出推荐、后端兜底又失败时，推荐区只是一片空白，
        // 用户分不清"没有可推荐"和"推荐没加载出来"。
        errorStore.warn(t('detail.recommendFailed'), normalizeApiError(e).message, '', 'Detail')
      }
    }
    
    similarVideos.value = results
  } catch (e) {
    similarVideos.value = []
    errorStore.warn(t('detail.recommendFailed'), normalizeApiError(e).message, '', 'Detail')
  } finally {
    similarLoading.value = false
  }
}

function openSimilarVideo(item: RecommendItem): void {
  router.push(getDetailPath(sourceKey.value, { vod_id: item.vod_id }))
}

// ==================== 详情加载 ====================

// 视频数据刷新限制：localStorage key 前缀，5 分钟内只允许刷新一次
const REFRESH_KEY_PREFIX = 'cczj_video_refresh_'
const REFRESH_INTERVAL_MS = 5 * 60 * 1000 // 5 分钟

function canRefresh(sourceKey: string, vodId: string): boolean {
  try {
    const key = REFRESH_KEY_PREFIX + sourceKey + '_' + vodId
    const last = readStorage<string | null>(key, null)
    if (!last) return true
    const elapsed = Date.now() - parseInt(last, 10)
    return elapsed >= REFRESH_INTERVAL_MS
  } catch {
    return true
  }
}

function markRefreshed(sourceKey: string, vodId: string): void {
  try {
    const key = REFRESH_KEY_PREFIX + sourceKey + '_' + vodId
    writeStorage(key, String(Date.now()))
  } catch { /* ignore */ }
}

// ==================== 删除视频 ====================
const deleting = ref(false)

async function deleteThisVideo(): Promise<void> {
  if (!video.value || deleting.value) return
  const ok = await confirmStore.confirm({
    title: t('detail.deleteConfirm'),
    message: t('detail.deleteConfirmMessage', {
      name: video.value.vod_name,
      source: sourceStore.sources.find(s => s.source_key === sourceKey.value)?.name || sourceKey.value
    }),
    okText: t('detail.deleteConfirmOk'),
    cancelText: t('common.cancel'),
  })
  if (!ok) return
  deleting.value = true
  try {
    await DeleteVideo({ source_key: sourceKey.value, vod_id: String(vodId.value) })
    // 通知所有列表页移除该视频
    videoStore.notifyDeletion(sourceKey.value, String(vodId.value))
    router.back()
  } catch (e: any) {
    error.value = `${t('detail.deleteFailed')}: ${normalizeApiError(e).message}`
  } finally {
    deleting.value = false
  }
}

// ==================== 同一部片在其它源（F7）====================
interface OtherSourceRef {
  sourceKey: string
  name: string
  vodId: string
}

const currentGlobalId = computed(() => Number(video.value?.global_id || globalId.value || 0))
const otherSources = ref<OtherSourceRef[]>([])

/**
 * 只按 global_id 查本地关联，不碰网络：一次 SELECT 就能给出「换源看」入口。
 * 取不到就留空 —— 这是补充入口，缺它不影响详情页本身，因此不打扰用户。
 */
async function loadOtherSources(): Promise<void> {
  otherSources.value = []
  const gid = currentGlobalId.value
  if (!gid) return
  try {
    const refs = (await FindSourcesByGlobalId(gid)) as any[]
    otherSources.value = (Array.isArray(refs) ? refs : [])
      .filter((r: any) => r?.source_key && r.source_key !== sourceKey.value)
      .map((r: any) => ({
        sourceKey: String(r.source_key),
        vodId: String(r.vod_id || ''),
        name: String(sourceStore.sources.find((s: any) => s.source_key === r.source_key)?.name || r.source_key),
      }))
  } catch {
    otherSources.value = []
  }
}

function openOnOtherSource(item: OtherSourceRef): void {
  router.push(getDetailPath(item.sourceKey, { global_id: currentGlobalId.value, vod_id: item.vodId }))
}

async function loadDetail(): Promise<void> {
  if (!sourceKey.value || (!vodId.value && !globalId.value)) {
    error.value = t('detail.videoNotFound')
    return
  }
  loading.value = true
  error.value = null
  similarVideos.value = []

  // 检查是否允许从源站刷新数据（5 分钟内只允许刷新一次）
  const mayRefresh = canRefresh(sourceKey.value, vodId.value)
  if (mayRefresh) {
    markRefreshed(sourceKey.value, vodId.value)
  }

  // 阶段1: 先加载本地数据（refresh=false），立即展示
  const loadedFromCache = await videoStore.loadDetail(sourceKey.value, vodId.value, false, globalId.value)
  // 只有真拿到 localStorage 数据才需要再补一次后台刷新：未命中时上面那次调用内部
  // 已经走了一趟远程（stores/video.ts 未命中就落到 GetVideoDetail），再判它「没缓存所以
  // 该刷」会把同一部片连抓两遍——真机日志里同一条 ac=detail&ids= 相隔 362ms 出现两次就是这里。
  const shouldRefresh = mayRefresh && loadedFromCache

  if (!video.value) {
    // 本地无数据时，强制从源站获取
    await videoStore.loadDetail(sourceKey.value, vodId.value, true, globalId.value)
  }

  if (!video.value) {
    error.value = t('detail.videoNotFound')
    loading.value = false
    return
  }

  loading.value = false // 本地数据已就绪，关闭 loading

  loadSimilar()
  loadOtherSources()
  // 仅注册剧集列表 + 预取第一集 m3u8 文本（轻量），
  // 真正的 TS 片段预取交给播放器页面处理（避免预取错误集数）
  const eps = videoStore.episodes
  if (eps.length > 0) {
    TsCache.setEpisodes(eps.map((e: any) => ({
      source_key: sourceKey.value,
      vod_id: String(vodId.value),
      ep_url: e.ep_url,
      ep_num: e.ep_num,
      ep_name: e.ep_name,
    })))
    TsCache.setCurrentEpisode(0)
    TsCache.enable()
    // 只预取 m3u8 文本（约 10KB，几乎无成本），用户点击播放时命中文本缓存即可
    const firstUrl = resolveEpisodeUrl(eps[0])
    if (firstUrl) TsCache.fetchAndParseM3u8(proxyHlsURL(firstUrl)).catch(() => { })
  }

  // 阶段2: 后台异步刷新源站数据（不阻塞 UI）
  if (shouldRefresh) {
    refreshing.value = true
    videoStore.refreshDetail(sourceKey.value, vodId.value, globalId.value).then(() => {
      refreshing.value = false
      // 刷新后重新加载相似推荐（可能数据更丰富了）
      loadSimilar()
    }).catch(() => {
      refreshing.value = false
    })
  }

  // 异步更新豆瓣热度（有豆瓣 id 且无豆瓣评分时才更新，避免每次访问都请求豆瓣）
  const doubanId = (video.value as any)?.vod_douban_id || ''
  const doubanScore = (video.value as any)?.vod_douban_score || ''
  if (doubanId && !doubanScore) {
    const vName = video.value?.vod_name || ''
    if (vName) {
      DoubanUpdateVideo({ keyword: vName }).catch(() => { })
    }
  }

  // 选集加载完成后刷新进度百分比 & 最近观看（不阻塞 UI）
  // 先 flush 确保播放器写入的最新进度已落盘，再读取
  flushEpProgress()
  refreshEpProgress()
  refreshLastWatched()
  // 延迟再刷一次，确保从播放页返回时进度已同步
  setTimeout(() => {
    refreshEpProgress()
    refreshLastWatched()
  }, 200)
}

onMounted(async () => {
  await sourceStore.loadSources()
  try { await downloadStore.init() } catch { }
  await loadDetail()
  refreshEpProgress() // 视频加载完成后刷新进度
  await refreshLastWatched() // 从播放页返回时刷新“继续观看”
  refreshFav().catch(() => { })
  // 页面可见性变化时刷新进度（从播放页返回时同步）
  document.addEventListener('visibilitychange', onVisibilityChange)
  // 监听播放页实时进度更新事件
  window.addEventListener('cczj-ep-progress-updated', onEpProgressUpdated)
  window.addEventListener('cczj-ep-progress-flushed', onEpProgressFlushed)
  // 监听 localStorage 变化（跨标签页同步）
  window.addEventListener('storage', onStorageChange)
})

// KeepAlive 激活时刷新进度（若将来 Detail 被加入缓存）
onActivated(() => {
  nextTick(() => {
    refreshEpProgress()
    refreshLastWatched()
  })
})

// 路由级别监听：同一组件复用时参数变化也刷新
watch(() => route.fullPath, (newPath, oldPath) => {
  if (newPath !== oldPath && newPath.includes('/detail/')) {
    nextTick(() => {
      refreshEpProgress()
      refreshLastWatched()
    })
  }
})

// 源也要进依赖：「换源看同一部」时两源的 vod_id 可能相同，只盯 vodId 就不重载。
watch([sourceKey, vodId], async () => {
  if (vodId.value || globalId.value) {
    await loadDetail()
    refreshFav().catch(() => { })
  }
})

onBeforeUnmount(() => {
  document.removeEventListener('visibilitychange', onVisibilityChange)
  window.removeEventListener('cczj-ep-progress-updated', onEpProgressUpdated)
  window.removeEventListener('cczj-ep-progress-flushed', onEpProgressFlushed)
  window.removeEventListener('storage', onStorageChange)
})
</script>

<template>
  <div class="detail-page cczj-max-w-full cczj-text-primary">
    <!-- 顶部面包屑 / 返回 -->
    <div class="detail-nav cczj-flex cczj-items-center cczj-gap-3 cczj-mb-4">
      <Button variant="text" size="md" @click="router.back()" class="cczj-flex cczj-items-center cczj-gap-1">
        <Icon name="back" :size="14" />
        <span>{{ t('detail.back') }}</span>
      </Button>
      <div v-if="video" class="nav-title cczj-truncate cczj-flex-1">
        <span>{{ video.vod_name }}</span>
      </div>
      <button v-if="video" class="delete-btn-nav cczj-ml-auto cczj-flex-shrink-0 cczj-w-32 cczj-h-32 cczj-flex cczj-items-center cczj-justify-center cczj-border cczj-rounded-md cczj-bg-card cczj-text-muted cczj-cursor-pointer cczj-transition-fast" :disabled="deleting" :title="t('detail.deleteVideo')" @click="deleteThisVideo">
        <Icon name="trash" :size="14" />
      </button>
    </div>

    <div v-if="loading" class="center-pad cczj-text-center cczj-py-8 cczj-flex cczj-flex-col cczj-items-center cczj-justify-center">
      <LoadingSpinner :label="t('detail.loadingDetail')" />
    </div>

    <div v-else-if="error" class="center-pad cczj-text-center cczj-py-8 cczj-flex cczj-flex-col cczj-items-center cczj-justify-center">
      <div class="error-box cczj-flex cczj-flex-col cczj-items-center cczj-gap-3 cczj-bg-card cczj-border cczj-text-center">
        <div class="error-emoji cczj-mb-6">⚠️</div>
        <div class="error-title cczj-text-lg cczj-font-semibold">{{ t('detail.loadFailed') }}</div>
        <div class="error-msg cczj-text-13 cczj-text-muted cczj-mb-10">{{ error }}</div>
        <button class="btn-primary cczj-cursor-pointer cczj-rounded cczj-px-4 cczj-py-2" @click="loadDetail">{{ t('detail.retry') }}</button>
      </div>
    </div>

    <template v-else-if="video">
      <!-- 顶部信息区 -->
      <section class="detail-header cczj-flex cczj-gap-6 cczj-mb-6 cczj-bg-card cczj-border">
        <div class="poster-column cczj-flex cczj-flex-col cczj-gap-4 cczj-flex-shrink-0">
          <div class="poster-frame cczj-relative cczj-rounded-lg cczj-w-full cczj-overflow-hidden cczj-bg-secondary">
            <RemoteImage v-if="video.vod_pic" :src="video.vod_pic" :alt="video.vod_name" class="poster cczj-w-full cczj-h-full cczj-rounded" loading="lazy" />
            <div v-else class="poster poster-placeholder cczj-flex cczj-items-center cczj-justify-center cczj-rounded cczj-text-muted cczj-opacity-40">
              <Icon name="film" :size="64" />
            </div>
            <span v-if="video.vod_remarks" class="badge-float cczj-absolute cczj-top-2 cczj-right-2 cczj-rounded cczj-px-2 cczj-py-1 cczj-text-xs">{{ video.vod_remarks }}</span>
          </div>
          <div class="poster-actions cczj-flex cczj-flex-col cczj-gap-2">
            <div v-if="videoStore.episodes.length > 0" class="poster-action-group">
              <!-- 继续上次观看 → 有历史时显示 -->
              <button v-if="lastWatched" class="continue-btn cczj-inline-flex cczj-items-center cczj-justify-center cczj-gap-4 cczj-cursor-pointer cczj-rounded cczj-px-3 cczj-py-2 cczj-w-full cczj-text-13 cczj-font-semibold cczj-transition" @click="playEpisode(lastWatched.epIdx)">
                <Icon name="play" :size="12" />
                <span class="continue-main cczj-flex-1">{{ t('detail.continueWatching') }} · {{ lastWatched.epName }}</span>
                <span v-if="lastWatched.position > 0" class="continue-pct cczj-text-xs cczj-text-muted">{{ Math.min(100,
                  Math.round(lastWatched.position)) }}%</span>
              </button>
            </div>
            <Button :variant="isFav ? 'primary' : 'secondary'" size="md" :disabled="favBusy" @click="toggleFavorite"
              :title="isFav ? t('detail.removeFav') : t('detail.addFav')" class="cczj-flex cczj-items-center cczj-gap-2">
              <span>{{ isFav ? '★' : '☆' }}</span>
              <span>{{ isFav ? t('detail.removeFav') : t('detail.addFav') }}</span>
            </Button>
          </div>
        </div>

        <div class="info-column cczj-flex-1 cczj-flex cczj-flex-col cczj-gap-4">
          <h1 class="title cczj-text-2xl cczj-font-bold cczj-flex cczj-items-center cczj-gap-6">
            {{ video.vod_name }}
            <span v-if="refreshing" class="refreshing-badge cczj-inline-flex cczj-items-center cczj-gap-1 cczj-text-xs cczj-text-accent cczj-rounded cczj-px-2 cczj-py-1" :title="t('detail.updating')">
              <svg class="refreshing-spin" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <path d="M21 12a9 9 0 11-6.219-8.56"/>
              </svg>
              {{ t('detail.updating') }}
            </span>
          </h1>

          <div class="meta-tags cczj-flex cczj-flex-wrap cczj-gap-2">
            <button v-if="video.type_name" class="tag clickable cczj-cursor-pointer cczj-rounded cczj-px-2 cczj-py-1 cczj-text-xs cczj-flex cczj-items-center cczj-gap-1" @click="searchByKeyword(video.type_name)">
              <Icon name="tag" :size="10" />
              <span>{{ video.type_name }}</span>
            </button>
            <button v-if="video.vod_year" class="tag clickable cczj-cursor-pointer cczj-rounded cczj-px-2 cczj-py-1 cczj-text-xs cczj-flex cczj-items-center cczj-gap-1" @click="searchByKeyword(video.vod_year)">
              <Icon name="calendar" :size="10" />
              <span>{{ video.vod_year }}</span>
            </button>
            <button v-if="video.vod_area" class="tag clickable cczj-cursor-pointer cczj-rounded cczj-px-2 cczj-py-1 cczj-text-xs cczj-flex cczj-items-center cczj-gap-1" @click="searchByKeyword(video.vod_area)">
              <Icon name="map-pin" :size="10" />
              <span>{{ video.vod_area }}</span>
            </button>
          </div>

          <div v-if="directorList.length > 0" class="meta-row cczj-flex cczj-items-start cczj-gap-2">
            <span class="meta-label cczj-text-sm cczj-font-semibold cczj-text-muted cczj-w-16 cczj-flex-shrink-0">{{ t('detail.director') }}</span>
            <div class="meta-values cczj-flex cczj-flex-wrap cczj-gap-1 cczj-flex-1">
              <button v-for="(d, i) in directorList" :key="'d-' + i" class="meta-chip clickable cczj-cursor-pointer cczj-rounded cczj-px-2 cczj-py-1 cczj-text-xs cczj-bg-secondary cczj-hover-bg-accent-alpha"
                @click="searchByKeyword(d)">{{ d }}</button>
            </div>
          </div>

          <div v-if="actorList.length > 0" class="meta-row cczj-flex cczj-items-start cczj-gap-2">
            <span class="meta-label cczj-text-sm cczj-font-semibold cczj-text-muted cczj-w-16 cczj-flex-shrink-0">{{ t('detail.actors') }}</span>
            <div class="meta-values cczj-flex cczj-flex-wrap cczj-gap-1 cczj-flex-1">
              <button v-for="(a, i) in actorList" :key="'a-' + i" class="meta-chip clickable cczj-cursor-pointer cczj-rounded cczj-px-2 cczj-py-1 cczj-text-xs cczj-bg-secondary cczj-hover-bg-accent-alpha"
                @click="searchByKeyword(a)">{{ a }}</button>
            </div>
          </div>

          <div v-if="hasMetaRow" class="meta-row cczj-flex cczj-items-start cczj-gap-2">
            <span class="meta-label cczj-text-sm cczj-font-semibold cczj-text-muted cczj-w-16 cczj-flex-shrink-0">{{ t('detail.rating') }}</span>
            <div class="meta-values cczj-flex cczj-flex-wrap cczj-gap-2 cczj-flex-1">
              <Tag v-if="video.vod_douban_score" variant="success" size="sm" class="cczj-flex cczj-items-center cczj-gap-1">
                <Icon name="star" :size="10" />
                <span>{{ t('detail.doubanScoreValue', { score: video.vod_douban_score }) }}</span>
              </Tag>
              <Tag v-if="video.vod_score" variant="primary" size="sm" class="cczj-flex cczj-items-center cczj-gap-1">
                <Icon name="star" :size="10" />
                <span>{{ t('detail.scoreValue', { score: video.vod_score }) }}</span>
              </Tag>
              <Tag v-if="video.vod_hits" size="sm" class="cczj-flex cczj-items-center cczj-gap-1">
                <Icon name="flame" :size="10" />
                <span>{{ t('detail.hitsValue', { hits: formatHits(video.vod_hits) }) }}</span>
              </Tag>
            </div>
          </div>

          <div v-if="hasInfoRow" class="meta-row cczj-flex cczj-items-start cczj-gap-2">
            <span class="meta-label cczj-text-sm cczj-font-semibold cczj-text-muted cczj-w-16 cczj-flex-shrink-0">{{ t('detail.info') }}</span>
            <div class="meta-values cczj-flex cczj-flex-wrap cczj-gap-2 cczj-flex-1">
              <Tag v-if="video.vod_version" size="sm">{{ video.vod_version }}</Tag>
              <Tag v-if="video.vod_state" size="sm">{{ video.vod_state }}</Tag>
              <Tag v-if="video.vod_isend === '1'" variant="success" size="sm">{{ t('detail.completed') }}</Tag>
              <Tag v-else-if="video.vod_isend" size="sm">{{ t('detail.ongoing') }}</Tag>
              <Tag v-if="video.vod_pubdate" size="sm">{{ t('detail.pubdateValue', { date: video.vod_pubdate }) }}</Tag>
              <Tag v-if="videoStore.lines.length > 1" size="sm">{{ t('playLine.count', { count: videoStore.lines.length }) }}</Tag>
            </div>
          </div>

          <div class="overview-block cczj-flex cczj-flex-col cczj-gap-2">
            <div class="overview-label-row cczj-flex cczj-items-center cczj-justify-between">
              <span class="overview-label cczj-text-sm cczj-font-semibold">{{ t('detail.summary') }}</span>
              <Button v-if="overviewText && overviewText.length > 120" variant="text" size="sm" class="expand-btn cczj-text-xs cczj-text-accent cczj-cursor-pointer"
                @click="expandOverview = !expandOverview">
                {{ expandOverview ? t('common.collapse') : t('common.expand') }}
              </Button>
            </div>
            <div class="overview-text cczj-text-sm cczj-text-secondary" :class="{ expanded: expandOverview }">
              <template v-if="overviewText">{{ overviewText }}</template>
              <span v-else class="text-muted cczj-text-muted">{{ t('detail.noSummary') }}</span>
            </div>
          </div>
        </div>
      </section>

      <!-- 剧集列表 -->
      <section v-if="videoStore.episodes.length > 0" class="episodes-section cczj-mt-6 cczj-bg-card cczj-border">
        <div class="section-head cczj-flex cczj-items-center cczj-justify-between cczj-gap-4 cczj-mb-4">
          <div class="section-head-left cczj-flex cczj-items-center cczj-gap-2">
            <h3 class="cczj-text-lg cczj-font-semibold">{{ episodeMode === 'download' ? t('detail.episodesDownloadHint') : t('detail.episodesPlayHint') }}</h3>
            <!-- 共 X 集信息（始终显示为次要信息） -->
            <span class="cczj-text-sm cczj-text-muted">{{ t('detail.totalEpisodes', { count: videoStore.episodes.length }) }}</span>
          </div>
          <div class="section-head-right cczj-flex cczj-items-center cczj-gap-2">
            <Button variant="secondary" size="sm" @click="toggleEpisodeSort"
              :title="episodeSortAsc ? t('detail.sortAscTip') : t('detail.sortDescTip')" class="cczj-flex cczj-items-center cczj-gap-1">
              <Icon :name="episodeSortAsc ? 'chevron-down' : 'chevron-up'" :size="14" />
              <span>{{ episodeSortAsc ? t('detail.ascOrder') : t('detail.descOrder') }}</span>
            </Button>
            <Button :variant="episodeMode === 'download' ? 'primary' : 'secondary'" size="sm"
              @click="toggleEpisodeMode" class="cczj-flex cczj-items-center cczj-gap-1">
              <Icon name="download" :size="14" />
              <span>{{ episodeMode === 'download' ? t('detail.backToPlayMode') : t('detail.downloadMode') }}</span>
            </Button>
            <Button v-if="episodeMode === 'download'" variant="primary" size="sm" @click="downloadAllEpisodes" class="cczj-flex cczj-items-center cczj-gap-1">
              <Icon name="layers" :size="14" />
              <span>{{ t('detail.batchDownload') }}</span>
            </Button>
          </div>
        </div>
        <PlayLinePicker v-if="video && videoStore.lines.length > 1" class="cczj-mb-4" :lines="videoStore.lines"
          :model-value="videoStore.activeLineIndex" :source-key="sourceKey" :vod-id="String(vodId)"
          :global-id="Number(video.global_id || 0)" @update:model-value="videoStore.setActiveLine(Number($event))" />
        <div class="episodes-grid cczj-grid cczj-gap-2">
          <button v-for="(ep, i) in sortedEpisodes" :key="'ep-' + (ep.ep_num || i)" class="episode-btn cczj-relative cczj-rounded cczj-px-3 cczj-py-2 cczj-cursor-pointer cczj-flex cczj-items-center cczj-gap-1 cczj-text-sm cczj-transition" :class="{
            'download-mode': episodeMode === 'download',
            'in-download': downloadingEpKeys.has(epKey(ep)),
            'watched': episodeMode !== 'download' && isWatched(ep, origIdx(i)),
          }" @click="onEpisodeClick(i, ep)"
            :title="formatEpisodeName(ep, origIdx(i)) + (isWatched(ep, origIdx(i)) ? t('detail.watchedProgress', { pct: Math.round(getEpPct(ep, origIdx(i))) }) : '')">
            <Icon v-if="episodeMode === 'download'" name="download" :size="11" />
            <span class="ep-num cczj-flex-1 cczj-truncate">{{ formatEpisodeName(ep, origIdx(i)) }}</span>
            <div v-if="episodeMode !== 'download' && getEpPct(ep, origIdx(i)) > 0" class="ep-progress-fill cczj-absolute cczj-bottom-0 cczj-left-0 cczj-bg-accent"
              :style="{ width: getEpPct(ep, origIdx(i)) + '%' }"></div>
            <span v-if="episodeMode !== 'download' && getEpPct(ep, origIdx(i)) > 0" class="ep-progress-pct cczj-text-xs cczj-text-accent">{{
              Math.round(getEpPct(ep, origIdx(i))) }}%</span>
          </button>
        </div>
      </section>

      <!-- 同一部片在其它源：只列本地已采到的，点进去换一条源看 -->
      <section v-if="otherSources.length" class="other-sources-section cczj-mt-6 cczj-p-4 cczj-rounded cczj-bg-card cczj-border cczj-motion-reveal">
        <div class="section-head cczj-flex cczj-items-center cczj-gap-2 cczj-mb-4">
          <h3 class="cczj-text-lg cczj-font-semibold">{{ t('detail.otherSources') }}</h3>
        </div>
        <div class="other-sources-row cczj-flex cczj-flex-wrap cczj-gap-2">
          <button v-for="item in otherSources" :key="item.sourceKey"
            class="other-source-btn cczj-flex cczj-items-center cczj-gap-1 cczj-rounded cczj-px-3 cczj-py-2 cczj-text-sm cczj-cursor-pointer cczj-transition-fast"
            @click="openOnOtherSource(item)">
            <Icon name="globe" :size="13" />
            <span class="cczj-truncate">{{ item.name }}</span>
          </button>
        </div>
      </section>

      <!-- 相似推荐 -->
      <section class="similar-section cczj-mt-6 cczj-p-4 cczj-rounded cczj-bg-card cczj-border cczj-motion-reveal">
        <div class="section-head cczj-flex cczj-items-center cczj-justify-between cczj-gap-2 cczj-mb-4">
          <h3 class="cczj-text-lg cczj-font-semibold">{{ t('detail.similar') }}</h3>
          <div class="section-head-right cczj-flex cczj-items-center cczj-gap-2">
            <span v-if="similarVideos.length > 0" class="section-sub cczj-text-sm cczj-text-muted">{{ t('detail.similarCount', { count: similarVideos.length }) }}</span>
            <button v-if="hasMoreSimilar" class="show-more-btn cczj-cursor-pointer cczj-text-sm cczj-text-accent cczj-flex cczj-items-center cczj-gap-1 cczj-hover-underline" @click="goToRecommendations">
              {{ t('detail.viewMore') }}
              <span class="show-more-arrow">→</span>
            </button>
          </div>
        </div>

        <div v-if="similarLoading" class="similar-loading cczj-text-center cczj-py-4">
          <LoadingSpinner size="sm" :label="t('common.loading')" />
        </div>

        <div v-else-if="similarVideos.length === 0" class="similar-empty cczj-text-center cczj-py-4 cczj-text-muted">
          <span>{{ t('detail.noSimilar') }}</span>
        </div>

        <div v-else class="similar-grid cczj-grid cczj-gap-3">
          <div v-for="(item, i) in displayedSimilar" :key="'sim-' + item.vod_id + '-' + i" class="similar-card cczj-rounded cczj-overflow-hidden cczj-cursor-pointer cczj-transition cczj-motion-reveal-scale"
            @click="openSimilarVideo(item)"
            :style="{ '--cczj-motion-delay': `${i * 60}ms` }"
          >
            <div class="similar-cover cczj-relative cczj-rounded cczj-overflow-hidden cczj-bg-secondary cczj-border">
              <RemoteImage v-if="item.vod_pic" :src="item.vod_pic" :alt="item.vod_name" loading="lazy" class="cczj-w-full cczj-h-full cczj-block" />
              <div v-else class="similar-cover-empty cczj-flex cczj-items-center cczj-justify-center cczj-bg-secondary cczj-text-muted">
                <Icon name="film" :size="24" />
              </div>
              <span v-if="item.vod_remarks" class="similar-remarks cczj-absolute cczj-top-1 cczj-right-1 cczj-text-xs cczj-rounded">{{ item.vod_remarks }}</span>
              <div class="similar-overlay cczj-absolute cczj-inset-0 cczj-flex cczj-items-center cczj-justify-center cczj-opacity-0">
                <Icon name="play" :size="18" />
              </div>
            </div>
            <div class="similar-name cczj-mt-2 cczj-text-sm cczj-font-medium cczj-truncate" :title="item.vod_name">{{ item.vod_name }}</div>
            <div class="similar-match cczj-text-xs cczj-text-muted cczj-truncate">{{ item.matchKey }}</div>
          </div>
        </div>
      </section>
    </template>

    <!-- ==================== 下载弹窗 ==================== -->
    <Modal :model-value="showDownloadModal" :title="t('detail.selectEpisodeDownload')" width="640px" :show-footer="true"
      @update:model-value="(v: boolean) => !v && closeDownload()">
      <div v-if="downloadError" class="modal-error cczj-flex cczj-items-center cczj-gap-2 cczj-p-3 cczj-rounded cczj-bg-danger-alpha cczj-text-danger cczj-mb-3">
        <Icon name="alert-triangle" :size="14" />
        <span class="cczj-text-sm">{{ downloadError }}</span>
      </div>

      <div v-if="videoStore.episodes.length === 0" class="modal-empty cczj-text-center cczj-py-6 cczj-text-muted">
        {{ t('detail.noEpisodes') }}
      </div>

      <template v-else>
        <div class="modal-toolbar cczj-flex cczj-items-center cczj-justify-between cczj-gap-2 cczj-mb-3">
          <span class="modal-toolbar-tip cczj-text-sm cczj-text-muted">{{ t('detail.singleDownloadHint') }}</span>
          <Button variant="primary" size="sm" @click="downloadAllEpisodes" class="cczj-flex cczj-items-center cczj-gap-1">
            <Icon name="layers" :size="14" />
            <span>{{ t('detail.downloadAllCount', { count: videoStore.episodes.length }) }}</span>
          </Button>
        </div>

        <div class="modal-episodes cczj-grid cczj-gap-2 cczj-max-h-64 cczj-overflow-y-auto">
          <div v-for="(ep, i) in videoStore.episodes" :key="'dl-' + (ep.ep_num || i)" class="modal-episode cczj-flex cczj-items-center cczj-gap-2 cczj-p-2 cczj-rounded cczj-border cczj-cursor-pointer cczj-transition cczj-hover-bg-accent-alpha"
            :class="{ downloading: downloadingEpKeys.has(epKey(ep)) }" @click="downloadEpisode(ep)">
            <span class="modal-ep-index cczj-text-sm cczj-font-medium cczj-w-8">{{ ep.ep_num ?? (i + 1) }}</span>
            <span class="modal-ep-name cczj-truncate cczj-flex-1 cczj-text-sm">{{ formatEpisodeName(ep, i) }}</span>
            <span class="modal-ep-action cczj-text-accent">
              <Icon name="download" :size="14" />
            </span>
          </div>
        </div>
      </template>

      <!-- 下载任务状态 -->
      <div v-if="downloadStore.tasks.length > 0" class="modal-tasks cczj-mt-4 cczj-p-3 cczj-rounded cczj-bg-secondary cczj-border">
        <div class="modal-tasks-title cczj-flex cczj-items-center cczj-justify-between cczj-gap-2 cczj-mb-2">
          <span class="cczj-text-sm cczj-font-semibold">{{ t('detail.downloadTasks') }}</span>
          <span class="modal-tasks-count cczj-text-xs cczj-text-muted">{{ t('detail.taskCount', { count: downloadStore.tasks.length }) }}</span>
        </div>
        <div class="modal-tasks-list cczj-flex cczj-flex-col cczj-gap-2">
          <div v-for="task in downloadStore.tasks.slice(0, 6)" :key="task.task_id" class="task-row cczj-flex cczj-flex-col cczj-gap-1">
            <div class="task-name cczj-truncate cczj-text-sm cczj-font-medium">{{ sanitizeFilename(task.filename) }}</div>
            <div class="task-progress cczj-flex cczj-flex-col cczj-gap-1">
              <div class="task-bar cczj-h-1.5 cczj-rounded cczj-bg-border cczj-overflow-hidden">
                <div class="task-bar-fill cczj-h-full cczj-bg-accent cczj-transition-all"
                  :style="{ width: (task.total > 0 ? Math.round((task.downloaded / task.total) * 100) : 0) + '%' }">
                </div>
              </div>
              <div class="task-meta cczj-text-xs cczj-text-muted cczj-flex cczj-items-center cczj-gap-2">
                <template v-if="task.status === 'downloading'">
                  {{ humanizeBytes(task.speed_bps || 0) }}/s ·
                  {{ task.total > 0 ? Math.round((task.downloaded / task.total) * 100) : 0 }}%
                </template>
                <template v-else-if="task.status === 'done'">{{ t('detail.completedMark') }}</template>
                <template v-else-if="task.status === 'error'">{{ t('common.failed') }}: {{ task.error || t('common.unknownError') }}</template>
                <template v-else-if="task.status === 'paused'">{{ t('detail.paused') }}</template>
                <template v-else-if="task.status === 'queued'">{{ t('detail.waiting') }}</template>
                <template v-else>{{ task.status }}</template>
              </div>
            </div>
          </div>
        </div>
      </div>

      <template #footer>
        <Button variant="secondary" size="md" @click="closeDownload">{{ t('common.close') }}</Button>
      </template>
    </Modal>

    <!-- ==================== 选择收藏夹弹窗 ==================== -->
    <Modal :model-value="showFavFolderModal" :title="t('detail.favToFolder')" width="420px" :show-footer="true"
      @update:model-value="(v: boolean) => !v && (showFavFolderModal = false)">
      <div class="folder-select-list">
        <label v-for="folder in favFolders" :key="folder.id" class="folder-select-item"
          :class="{ active: favTargetFolderId === folder.id }">
          <input type="radio" v-model="favTargetFolderId" :value="folder.id" />
          <span class="folder-radio" />
          <Icon :name="folder.default ? 'star' : 'list'" :size="14" />
          <span class="folder-name">{{ folder.default ? t('detail.defaultFolder') : folder.name }}</span>
        </label>
      </div>
      <template #footer>
        <Button variant="secondary" size="md" @click="showFavFolderModal = false">{{ t('common.cancel') }}</Button>
        <Button variant="primary" size="md" @click="confirmAddToFolder">{{ t('detail.confirmFav') }}</Button>
      </template>
    </Modal>
  </div>
</template>

<style scoped src="../styles/views/detail.css"></style>
