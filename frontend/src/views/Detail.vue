<script setup lang="ts">
defineOptions({ name: 'Detail' })
import { ref, computed, onMounted, onBeforeUnmount, onActivated, watch, nextTick } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { GetRecentHistory, SaveWatchHistory, AddFavorite, RemoveFavorite, IsFavorite, DeleteVideo } from '../../bindings/cczjVideo/app'
import { useSourceStore } from '../stores/source'
import { useVideoStore } from '../stores/video'
import { useDownloadStore } from '../stores/download'
import { useConfirmStore } from '../stores/confirm'
import Icon from '../components/Icon.vue'
import { Button, Modal, Tag, Spinner as LoadingSpinner } from '../components/ui'
import { getDetailPath, getSearchPath, getPlayerPath, humanizeBytes, buildEpisodeFilename, buildSingleFilename, sanitizeFilename, resolveEpisodeUrl, stripHtmlTags } from '../utils'
import { TsCache } from '../utils/tsCache'
import { computeRecommendations, type RecommendItem, extractYear } from '../utils/recommend'
import { epProgressKey, loadEpProgress, getEpProgressPct, flushEpProgress } from '../utils/episodeProgress'
import { bumpFavoritesRefresh } from '../stores/favoritesSync'
import type { Video, HistoryItem, Episode } from '../types'

const route = useRoute()
const router = useRouter()
const sourceStore = useSourceStore()
const videoStore = useVideoStore()
const downloadStore = useDownloadStore()
const confirmStore = useConfirmStore()

// ==================== 路由参数解析 ====================
const vodId = computed(() => {
  const p = route.params.vodId
  if (Array.isArray(p)) return p[0] || ''
  if (p) return String(p)
  return String(route.query.id || '')
})

const sourceKey = computed(() => {
  const p = route.params.sourceKey
  if (Array.isArray(p)) return p[0] || ''
  if (p) return String(p)
  return String(route.query.source || sourceStore.currentSourceKey || '')
})

// ==================== 页面状态 ====================
const loading = ref(false)
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
      for (const h of history) {
        const globalMatch = video.value?.global_id ? (h.global_id === video.value.global_id) : null
        if (globalMatch === true) { found = h; break }
        if (globalMatch === null && String(h.vod_id) === String(vodId.value)) { found = h; break }
        if (h.ep_num == null) continue
      }
    }

    if (found && found.ep_num != null) {
      const idx = epNumToIdx.get(Number(found.ep_num))
      if (idx !== undefined) {
        const ep = videoStore.episodes[idx]
        lastWatched.value = {
          epNum: Number(found.ep_num),
          epIdx: idx,
          epName: formatEpisodeName(ep, idx),
          position: found.position || 0,
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
  { id: 'default', name: '默认收藏夹', default: true },
])
const favTargetFolderId = ref<string>('default')

function loadFavFolders(): void {
  try {
    const raw = localStorage.getItem('cczj_fav_folders')
    if (raw) {
      const parsed = JSON.parse(raw) as FavFolder[]
      if (Array.isArray(parsed) && parsed.length > 0) {
        favFolders.value = parsed
        return
      }
    }
  } catch { /* ignore */ }
  favFolders.value = [{ id: 'default', name: '默认收藏夹', default: true }]
}

async function refreshFav(): Promise<void> {
  if (!vodId.value) return
  try {
    const val = await IsFavorite({ source_key: sourceKey.value, vod_id: String(vodId.value) }) as boolean
    isFav.value = !!val
  } catch { /* ignore */ }
}
async function toggleFavorite(): Promise<void> {
  if (!vodId.value || favBusy.value) return
  // 若已收藏 → 直接取消
  if (isFav.value) {
    favBusy.value = true
    try {
      await RemoveFavorite({ source_key: sourceKey.value, vod_id: String(vodId.value) })
      // 清除 mapping
      const key = `${sourceKey.value}-${vodId.value}`
      try {
        const raw = localStorage.getItem('cczj_fav_mapping')
        if (raw) {
          const obj = JSON.parse(raw) as Record<string, string>
          delete obj[key]
          localStorage.setItem('cczj_fav_mapping', JSON.stringify(obj))
        }
      } catch { /* ignore */ }
      isFav.value = false
      bumpFavoritesRefresh()
    } catch { /* ignore */ }
    finally {
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
      const raw = localStorage.getItem('cczj_fav_mapping')
      const obj: Record<string, string> = raw ? JSON.parse(raw) : {}
      obj[key] = favTargetFolderId.value
      localStorage.setItem('cczj_fav_mapping', JSON.stringify(obj))
    } catch { /* ignore */ }
    isFav.value = true
    bumpFavoritesRefresh()
  } catch { /* ignore */ }
  finally {
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
  return !!(v.vod_version || v.vod_state || v.vod_isend || v.vod_pubdate || v.vod_play_from)
})

function formatHits(raw: string | undefined | null): string {
  if (!raw) return ''
  const n = parseInt(raw, 10)
  if (isNaN(n)) return raw
  if (n >= 100000000) return (n / 100000000).toFixed(1) + '亿'
  if (n >= 10000) return (n / 10000).toFixed(1) + '万'
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
    try { downloadStore.init() } catch {}
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
      title: '重复下载',
      message: `「${ep.ep_name || ('第' + ep.ep_num + '集')}」已存在下载记录，是否重新下载？`,
      okText: '重新下载',
      cancelText: '取消',
    })
    if (!ok) return
  }

  downloadingEpKeys.value.add(key)
  try {
    const fn = buildEpisodeFilename(video.value.vod_name || '视频', ep.ep_num, ep.ep_name, ep.ep_url)
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
    const msg = e?.message || String(e) || '下载启动失败'
    if (msg !== '取消') downloadError.value = msg
  } finally {
    setTimeout(() => downloadingEpKeys.value.delete(key), 200)
  }
}

async function downloadAllEpisodes(): Promise<void> {
  if (videoStore.episodes.length === 0) return
  const ok = await confirmStore.confirm({
    title: '下载全部剧集',
    message: `即将下载 ${videoStore.episodes.length} 个视频，是否继续？`,
    okText: '全部下载',
    cancelText: '取消',
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
    } as any).catch(() => {})
    // 预取点击剧集的 m3u8 文本（轻量，跳转到播放页时可命中缓存）
    TsCache.setCurrentEpisode(epIndex)
    const epUrl = resolveEpisodeUrl(ep)
    if (epUrl) TsCache.fetchAndParseM3u8(epUrl).catch(() => {})
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
  return '第' + String(num) + '集'
}

// ==================== 类似推荐 ====================
const similarVideos = ref<RecommendItem[]>([])
const similarLoading = ref(false)
const MAX_SIMILAR_INITIAL = 10

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
    const list: Video[] = Array.isArray(videoStore.videos) ? videoStore.videos : []
    const currentId = String(video.value.vod_id || '')
    const currentName = video.value.vod_name || ''
    const year = extractYear(video.value.vod_year)

    similarVideos.value = computeRecommendations(list, currentId, currentName, year)
  } catch {
    similarVideos.value = []
  } finally {
    similarLoading.value = false
  }
}

function openSimilarVideo(item: RecommendItem): void {
  router.push(getDetailPath(sourceKey.value, { vod_id: item.vod_id }))
}

// ==================== 详情加载 ====================

// ==================== 删除视频 ====================
const deleting = ref(false)

async function deleteThisVideo(): Promise<void> {
  if (!video.value || deleting.value) return
  const ok = await confirmStore.confirm({
    title: '删除视频',
    message: `确定要删除「${video.value.vod_name}」吗？\n此操作将从「${sourceStore.sources.find(s => s.source_key === sourceKey.value)?.name || sourceKey.value}」源中永久删除该视频及其相关收藏和历史记录。`,
    okText: '确认删除',
    cancelText: '取消',
  })
  if (!ok) return
  deleting.value = true
  try {
    await DeleteVideo({ source_key: sourceKey.value, vod_id: String(vodId.value) })
    // 通知所有列表页移除该视频
    videoStore.notifyDeletion(sourceKey.value, String(vodId.value))
    router.back()
  } catch (e: any) {
    error.value = `删除失败: ${e?.message || e}`
  } finally {
    deleting.value = false
  }
}

async function loadDetail(): Promise<void> {
  if (!sourceKey.value || !vodId.value) {
    error.value = '缺少视频标识，无法加载'
    return
  }
  loading.value = true
  error.value = null
  similarVideos.value = []

  await videoStore.loadDetail(sourceKey.value, vodId.value)

  if (!video.value) {
    error.value = '视频不存在或加载失败'
    loading.value = false
    return
  }

  loadSimilar()
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
    if (firstUrl) TsCache.fetchAndParseM3u8(firstUrl).catch(() => {})
  }
  loading.value = false

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
  try { await downloadStore.init() } catch {}
  await loadDetail()
  refreshEpProgress() // 视频加载完成后刷新进度
  await refreshLastWatched() // 从播放页返回时刷新“继续观看”
  refreshFav().catch(() => {})
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

watch(vodId, () => {
  if (vodId.value) {
    loadDetail()
    refreshFav().catch(() => {})
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
  <div class="detail-page">
    <!-- 顶部面包屑 / 返回 -->
    <div class="detail-nav">
      <Button variant="text" size="md" @click="router.back()">
        <Icon name="back" :size="14" />
        <span>返回</span>
      </Button>
      <div v-if="video" class="nav-title">
        <span>{{ video.vod_name }}</span>
      </div>
      <button
        v-if="video"
        class="delete-btn-nav"
        :disabled="deleting"
        title="删除此视频"
        @click="deleteThisVideo"
      >
        <Icon name="trash" :size="14" />
      </button>
    </div>

    <div v-if="loading" class="center-pad">
      <LoadingSpinner label="正在加载详情..." />
    </div>

    <div v-else-if="error" class="center-pad">
      <div class="error-box">
        <div class="error-emoji">⚠️</div>
        <div class="error-title">加载失败</div>
        <div class="error-msg">{{ error }}</div>
        <button class="btn-primary" @click="loadDetail">重试</button>
      </div>
    </div>

    <template v-else-if="video">
      <!-- 顶部信息区 -->
      <section class="detail-header">
        <div class="poster-column">
          <div class="poster-frame">
            <img v-if="video.vod_pic" :src="video.vod_pic" :alt="video.vod_name" class="poster" loading="lazy" />
            <div v-else class="poster poster-placeholder">
              <Icon name="film" :size="64" />
            </div>
            <span v-if="video.vod_remarks" class="badge-float">{{ video.vod_remarks }}</span>
          </div>
          <div class="poster-actions">
            <div v-if="videoStore.episodes.length > 0" class="poster-action-group">
              <!-- 继续上次观看 → 有历史时显示 -->
              <button v-if="lastWatched" class="continue-btn" @click="playEpisode(lastWatched.epIdx)">
                <Icon name="play" :size="12" />
                <span class="continue-main">继续观看 · {{ lastWatched.epName }}</span>
                <span v-if="lastWatched.position > 0" class="continue-pct">{{ Math.min(100, Math.round(lastWatched.position)) }}%</span>
              </button>
            </div>
            <Button
              :variant="isFav ? 'primary' : 'secondary'"
              size="md"
              :disabled="favBusy"
              @click="toggleFavorite"
              :title="isFav ? '取消收藏' : '加入收藏'"
            >
              <span>{{ isFav ? '★' : '☆' }}</span>
              <span>{{ isFav ? '已收藏' : '收藏' }}</span>
            </Button>
          </div>
        </div>

        <div class="info-column">
          <h1 class="title">{{ video.vod_name }}</h1>

          <div class="meta-tags">
            <button v-if="video.type_name" class="tag clickable" @click="searchByKeyword(video.type_name)">
              <Icon name="tag" :size="10" />
              <span>{{ video.type_name }}</span>
            </button>
            <button v-if="video.vod_year" class="tag clickable" @click="searchByKeyword(video.vod_year)">
              <Icon name="calendar" :size="10" />
              <span>{{ video.vod_year }}</span>
            </button>
            <button v-if="video.vod_area" class="tag clickable" @click="searchByKeyword(video.vod_area)">
              <Icon name="map-pin" :size="10" />
              <span>{{ video.vod_area }}</span>
            </button>
          </div>

          <div v-if="directorList.length > 0" class="meta-row">
            <span class="meta-label">导演</span>
            <div class="meta-values">
              <button
                v-for="(d, i) in directorList"
                :key="'d-' + i"
                class="meta-chip clickable"
                @click="searchByKeyword(d)"
              >{{ d }}</button>
            </div>
          </div>

          <div v-if="actorList.length > 0" class="meta-row">
            <span class="meta-label">演员</span>
            <div class="meta-values">
              <button
                v-for="(a, i) in actorList"
                :key="'a-' + i"
                class="meta-chip clickable"
                @click="searchByKeyword(a)"
              >{{ a }}</button>
            </div>
          </div>

          <div v-if="hasMetaRow" class="meta-row">
            <span class="meta-label">评分</span>
            <div class="meta-values">
              <Tag v-if="video.vod_douban_score" variant="success" size="sm">
                <Icon name="star" :size="10" />
                <span>豆瓣 {{ video.vod_douban_score }}</span>
              </Tag>
              <Tag v-if="video.vod_score" variant="primary" size="sm">
                <Icon name="star" :size="10" />
                <span>评分 {{ video.vod_score }}</span>
              </Tag>
              <Tag v-if="video.vod_hits" size="sm">
                <Icon name="flame" :size="10" />
                <span>{{ formatHits(video.vod_hits) }} 热度</span>
              </Tag>
            </div>
          </div>

          <div v-if="hasInfoRow" class="meta-row">
            <span class="meta-label">信息</span>
            <div class="meta-values">
              <Tag v-if="video.vod_version" size="sm">{{ video.vod_version }}</Tag>
              <Tag v-if="video.vod_state" size="sm">{{ video.vod_state }}</Tag>
              <Tag v-if="video.vod_isend === '1'" variant="success" size="sm">已完结</Tag>
              <Tag v-else-if="video.vod_isend" size="sm">连载中</Tag>
              <Tag v-if="video.vod_pubdate" size="sm">上映: {{ video.vod_pubdate }}</Tag>
              <Tag v-if="video.vod_play_from" size="sm">来源: {{ video.vod_play_from }}</Tag>
            </div>
          </div>

          <div class="overview-block">
            <div class="overview-label-row">
              <span class="overview-label">简介</span>
              <Button v-if="overviewText && overviewText.length > 120" variant="text" size="sm" class="expand-btn" @click="expandOverview = !expandOverview">
                {{ expandOverview ? '收起' : '展开' }}
              </Button>
            </div>
            <div class="overview-text" :class="{ expanded: expandOverview }">
              <template v-if="overviewText">{{ overviewText }}</template>
              <span v-else class="text-muted">暂无简介</span>
            </div>
          </div>
        </div>
      </section>

      <!-- 剧集列表 -->
      <section v-if="videoStore.episodes.length > 0" class="episodes-section">
        <div class="section-head">
          <div class="section-head-left">
            <h3>{{ episodeMode === 'download' ? '选集 · 点击下载' : '选集 · 点击播放' }}</h3>
              <!-- 共 X 集信息（始终显示为次要信息） -->
            <span>共 {{ videoStore.episodes.length }} 集</span>
          </div>
          <div class="section-head-right">
            <Button
              variant="secondary"
              size="sm"
              @click="toggleEpisodeSort"
              :title="episodeSortAsc ? '当前正序，点击切换倒序' : '当前倒序，点击切换正序'"
            >
              <Icon :name="episodeSortAsc ? 'chevron-down' : 'chevron-up'" :size="14" />
              <span>{{ episodeSortAsc ? '正序' : '倒序' }}</span>
            </Button>
            <Button
              :variant="episodeMode === 'download' ? 'primary' : 'secondary'"
              size="sm"
              @click="toggleEpisodeMode"
            >
              <Icon name="download" :size="14" />
              <span>{{ episodeMode === 'download' ? '返回观看模式' : '下载模式' }}</span>
            </Button>
            <Button v-if="episodeMode === 'download'" variant="primary" size="sm" @click="downloadAllEpisodes">
              <Icon name="layers" :size="14" />
              <span>批量下载</span>
            </Button>
          </div>
        </div>
        <div class="episodes-grid">
          <button
            v-for="(ep, i) in sortedEpisodes"
            :key="'ep-' + (ep.ep_num || i)"
            class="episode-btn"
            :class="{
              'download-mode': episodeMode === 'download',
              'in-download': downloadingEpKeys.has(epKey(ep)),
              'watched': episodeMode !== 'download' && isWatched(ep, origIdx(i)),
            }"
            @click="onEpisodeClick(i, ep)"
            :title="formatEpisodeName(ep, origIdx(i)) + (isWatched(ep, origIdx(i)) ? ' · 已观看 ' + Math.round(getEpPct(ep, origIdx(i))) + '%' : '')"
          >
            <Icon v-if="episodeMode === 'download'" name="download" :size="11" />
            <span class="ep-num">{{ formatEpisodeName(ep, origIdx(i)) }}</span>
            <div v-if="episodeMode !== 'download' && getEpPct(ep, origIdx(i)) > 0" class="ep-progress-fill" :style="{ width: getEpPct(ep, origIdx(i)) + '%' }"></div>
            <span v-if="episodeMode !== 'download' && getEpPct(ep, origIdx(i)) > 0" class="ep-progress-pct">{{ Math.round(getEpPct(ep, origIdx(i))) }}%</span>
          </button>
        </div>
      </section>

      <!-- 相似推荐 -->
      <section class="similar-section">
        <div class="section-head">
          <h3>相似推荐</h3>
          <div class="section-head-right">
            <span v-if="similarVideos.length > 0" class="section-sub">{{ similarVideos.length }} 部</span>
            <button
              v-if="hasMoreSimilar"
              class="show-more-btn"
              @click="goToRecommendations"
            >
              查看更多
              <span class="show-more-arrow">→</span>
            </button>
          </div>
        </div>

        <div v-if="similarLoading" class="similar-loading">
          <LoadingSpinner size="sm" label="加载中..." />
        </div>

        <div v-else-if="similarVideos.length === 0" class="similar-empty">
          <span>暂无相似推荐</span>
        </div>

        <div v-else class="similar-grid">
          <div
            v-for="(item, i) in displayedSimilar"
            :key="'sim-' + item.vod_id + '-' + i"
            class="similar-card"
            @click="openSimilarVideo(item)"
          >
            <div class="similar-cover">
              <img v-if="item.vod_pic" :src="item.vod_pic" :alt="item.vod_name" loading="lazy" />
              <div v-else class="similar-cover-empty">
                <Icon name="film" :size="24" />
              </div>
              <span v-if="item.vod_remarks" class="similar-remarks">{{ item.vod_remarks }}</span>
              <div class="similar-overlay">
                <Icon name="play" :size="18" />
              </div>
            </div>
            <div class="similar-name" :title="item.vod_name">{{ item.vod_name }}</div>
            <div class="similar-match">{{ item.matchKey }}</div>
          </div>
        </div>
      </section>
    </template>

    <!-- ==================== 下载弹窗 ==================== -->
    <Modal
      :model-value="showDownloadModal"
      title="选择剧集下载"
      width="640px"
      :show-footer="true"
      @update:model-value="(v: boolean) => !v && closeDownload()"
    >
      <div v-if="downloadError" class="modal-error">
        <Icon name="alert-triangle" :size="14" />
        <span>{{ downloadError }}</span>
      </div>

      <div v-if="videoStore.episodes.length === 0" class="modal-empty">
        暂无可下载的剧集
      </div>

      <template v-else>
        <div class="modal-toolbar">
          <span class="modal-toolbar-tip">点击单集下载 · 每集独立任务</span>
          <Button variant="primary" size="sm" @click="downloadAllEpisodes">
            <Icon name="layers" :size="14" />
            <span>全部下载 ({{ videoStore.episodes.length }})</span>
          </Button>
        </div>

        <div class="modal-episodes">
          <div
            v-for="(ep, i) in videoStore.episodes"
            :key="'dl-' + (ep.ep_num || i)"
            class="modal-episode"
            :class="{ downloading: downloadingEpKeys.has(epKey(ep)) }"
            @click="downloadEpisode(ep)"
          >
            <span class="modal-ep-index">{{ ep.ep_num ?? (i + 1) }}</span>
            <span class="modal-ep-name">{{ formatEpisodeName(ep, i) }}</span>
            <span class="modal-ep-action">
              <Icon name="download" :size="14" />
            </span>
          </div>
        </div>
      </template>

      <!-- 下载任务状态 -->
      <div v-if="downloadStore.tasks.length > 0" class="modal-tasks">
        <div class="modal-tasks-title">
          <span>下载任务</span>
          <span class="modal-tasks-count">{{ downloadStore.tasks.length }} 个</span>
        </div>
        <div class="modal-tasks-list">
          <div
            v-for="task in downloadStore.tasks.slice(0, 6)"
            :key="task.task_id"
            class="task-row"
          >
            <div class="task-name">{{ sanitizeFilename(task.filename) }}</div>
            <div class="task-progress">
              <div class="task-bar">
                <div
                  class="task-bar-fill"
                  :style="{ width: (task.total > 0 ? Math.round((task.downloaded / task.total) * 100) : 0) + '%' }"
                ></div>
              </div>
              <div class="task-meta">
                <template v-if="task.status === 'downloading'">
                  {{ humanizeBytes(task.speed_bps || 0) }}/s ·
                  {{ task.total > 0 ? Math.round((task.downloaded / task.total) * 100) : 0 }}%
                </template>
                <template v-else-if="task.status === 'done'">已完成 ✓</template>
                <template v-else-if="task.status === 'error'">失败: {{ task.error || '未知错误' }}</template>
                <template v-else-if="task.status === 'paused'">已暂停</template>
                <template v-else-if="task.status === 'queued'">等待中...</template>
                <template v-else>{{ task.status }}</template>
              </div>
            </div>
          </div>
        </div>
      </div>

      <template #footer>
        <Button variant="secondary" size="md" @click="closeDownload">关闭</Button>
      </template>
    </Modal>

    <!-- ==================== 选择收藏夹弹窗 ==================== -->
    <Modal
      :model-value="showFavFolderModal"
      title="收藏到文件夹"
      width="420px"
      :show-footer="true"
      @update:model-value="(v: boolean) => !v && (showFavFolderModal = false)"
    >
      <div class="folder-select-list">
        <label
          v-for="folder in favFolders"
          :key="folder.id"
          class="folder-select-item"
          :class="{ active: favTargetFolderId === folder.id }"
        >
          <input type="radio" v-model="favTargetFolderId" :value="folder.id" />
          <span class="folder-radio" />
          <Icon :name="folder.default ? 'star' : 'list'" :size="14" />
          <span class="folder-name">{{ folder.name }}</span>
        </label>
      </div>
      <template #footer>
        <Button variant="secondary" size="md" @click="showFavFolderModal = false">取消</Button>
        <Button variant="primary" size="md" @click="confirmAddToFolder">确认收藏</Button>
      </template>
    </Modal>
  </div>
</template>

<style scoped>
.detail-page {
  max-width: 100%;
  color: var(--text-primary);
  padding: 0;
  animation: fadeInUp 0.3s ease;
}

@keyframes fadeInUp {
  from { opacity: 0; transform: translateY(8px); }
  to { opacity: 1; transform: translateY(0); }
}

.center-pad {
  padding: 60px 20px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
}

/* 顶部导航 */
.detail-nav {
  display: flex;
  align-items: center;
  gap: 16px;
  margin-bottom: 14px;
}
.delete-btn-nav {
  margin-left: auto;
  flex-shrink: 0;
  width: 32px;
  height: 32px;
  display: flex;
  align-items: center;
  justify-content: center;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--bg-card);
  color: var(--text-muted);
  cursor: pointer;
  transition: all 0.15s ease;
}
.delete-btn-nav:hover:not(:disabled) {
  background: rgba(239, 68, 68, 0.12);
  border-color: #ef4444;
  color: #ef4444;
}
.delete-btn-nav:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.back-btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 6px 14px;
  border-radius: 20px;
  border: 1px solid var(--border);
  background: var(--bg-card);
  color: var(--text-secondary);
  cursor: pointer;
  font-size: 12px;
  font-family: inherit;
  transition: all 0.15s ease;
}
.back-btn:hover {
  border-color: var(--accent);
  color: var(--accent);
  background: var(--accent-alpha-10);
}

.nav-title {
  font-size: 13px;
  color: var(--text-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  flex: 1;
}

/* 错误框 */
.error-box {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 14px;
  padding: 32px 40px;
  text-align: center;
  max-width: 480px;
}
.error-emoji { font-size: 48px; margin-bottom: 12px; }
.error-title { font-size: 18px; font-weight: 600; margin-bottom: 8px; }
.error-msg { font-size: 13px; color: var(--text-muted); margin-bottom: 20px; }

/* ============ 顶部信息区 ============ */
.detail-header {
  display: flex;
  gap: 24px;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 14px;
  padding: 20px;
  margin-bottom: 20px;
}

@media (max-width: 720px) {
  .detail-header { flex-direction: column; }
}

.poster-column {
  flex-shrink: 0;
  width: 220px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}
@media (max-width: 720px) { .poster-column { width: 100%; } }

.poster-frame {
  position: relative;
  aspect-ratio: 2/3;
  width: 100%;
  border-radius: 12px;
  overflow: hidden;
  background: var(--bg-secondary);
  box-shadow: 0 4px 20px rgba(0, 0, 0, 0.15);
}

.poster {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

.poster-placeholder {
  width: 100%;
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--text-muted);
  opacity: 0.4;
}

.badge-float {
  position: absolute;
  top: 10px;
  right: 10px;
  padding: 4px 10px;
  border-radius: 12px;
  background: var(--accent);
  color: var(--accent-contrast);
  font-size: 11px;
  font-weight: 600;
  box-shadow: 0 2px 10px var(--accent-alpha-35);
}

.poster-actions {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.btn-download {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 12px 18px;
  border-radius: 10px;
  border: 1px solid var(--border);
  background: var(--bg-secondary);
  color: var(--text-primary);
  cursor: pointer;
  font-size: 13px;
  font-weight: 500;
  font-family: inherit;
  transition: all 0.15s ease;
}
.btn-download:hover {
  border-color: var(--accent);
  background: var(--accent-alpha-10);
  color: var(--accent);
}

.ep-count-info {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  padding: 8px 12px;
  border-radius: 8px;
  background: var(--tag-bg);
  font-size: 12px;
  color: var(--tag-text);
  border: 1px solid var(--border);
}

.poster-action-group {
  display: flex;
  flex-direction: column;
  gap: 8px;
  width: 100%;
}

/* ⭐ 继续上次观看 —— 实心主题色按钮 */
.continue-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 10px 16px;
  border-radius: 10px;
  border: 1px solid var(--accent);
  background: var(--btn-solid);
  color: var(--btn-solid-text);
  cursor: pointer;
  font-size: 13px;
  font-weight: 600;
  font-family: inherit;
  transition: all 0.2s ease;
  width: 100%;
}
.continue-btn:hover {
  background: var(--accent);
  transform: translateY(-1px);
  box-shadow: 0 4px 14px var(--accent-alpha-35);
}
.continue-btn .continue-main {
  flex: 1;
  text-align: center;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.continue-btn .continue-pct {
  font-size: 11px;
  padding: 2px 8px;
  border-radius: 8px;
  background: var(--accent-contrast);
  color: var(--accent);
  font-weight: 700;
  opacity: 0.9;
}

/* ⭐ 收藏按钮：主题化 */
.fav-btn-detail {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  padding: 10px 16px;
  border-radius: 10px;
  border: 1px solid var(--border);
  background: var(--btn-soft);
  color: var(--btn-soft-text);
  cursor: pointer;
  font-size: 13px;
  font-weight: 500;
  font-family: inherit;
  transition: all 0.2s ease;
  width: 100%;
}
.fav-btn-detail:hover {
  border-color: var(--accent);
  background: var(--accent);
  color: var(--accent-contrast);
  transform: translateY(-1px);
}
.fav-btn-detail.active {
  border-color: var(--accent);
  background: var(--btn-solid);
  color: var(--btn-solid-text);
}
.fav-btn-detail.active:hover {
  background: var(--accent);
}
.fav-btn-detail.busy { opacity: 0.6; pointer-events: none; }
.fav-btn-detail .fav-icon { font-size: 14px; }

.info-column {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.title {
  font-size: 24px;
  font-weight: 700;
  margin: 0;
  line-height: 1.3;
}

.meta-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.tag {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 5px 12px;
  border-radius: 20px;
  background: var(--bg-secondary);
  border: 1px solid transparent;
  color: var(--text-secondary);
  font-size: 12px;
  font-weight: 500;
  font-family: inherit;
  cursor: default;
  transition: all 0.15s ease;
}

.tag.clickable {
  cursor: pointer;
}

.tag.clickable:hover {
  background: var(--accent);
  color: var(--accent-contrast);
  transform: translateY(-1px);
  box-shadow: 0 2px 8px var(--accent-alpha-35);
}

.meta-row {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  font-size: 13px;
}

.meta-label {
  flex-shrink: 0;
  color: var(--text-muted);
  width: 48px;
  padding-top: 5px;
  font-weight: 500;
}

.meta-values {
  flex: 1;
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.meta-chip {
  padding: 5px 12px;
  background: var(--bg-secondary);
  border-radius: 6px;
  font-size: 12px;
  color: var(--text-secondary);
  border: 1px solid var(--border);
  font-family: inherit;
  cursor: default;
  transition: all 0.15s ease;
}

.meta-chip.clickable { cursor: pointer; }

.meta-chip.clickable:hover {
  background: var(--accent);
  color: var(--accent-contrast);
  border-color: var(--accent);
  transform: translateY(-1px);
}

.meta-chip.highlight {
  background: var(--tag-highlight-bg);
  color: var(--tag-highlight-text);
  border-color: var(--accent);
  font-weight: 600;
}

.meta-chip.success {
  background: var(--btn-soft);
  color: var(--btn-soft-text);
  border-color: var(--accent);
}

.meta-chip.muted {
  background: transparent;
  color: var(--text-muted);
  font-size: 11px;
}

.overview-block {
  margin-top: 6px;
  padding-top: 14px;
  border-top: 1px dashed var(--border);
}

.overview-label-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 8px;
}

.overview-label {
  font-size: 12px;
  color: var(--text-muted);
  font-weight: 500;
}

.expand-btn {
  background: transparent;
  border: none;
  color: var(--accent);
  font-size: 12px;
  cursor: pointer;
  padding: 4px 8px;
  border-radius: 6px;
  font-family: inherit;
  transition: background 0.15s;
}
.expand-btn:hover { background: var(--accent-alpha-10); }

.overview-text {
  font-size: 13px;
  line-height: 1.8;
  color: var(--text-secondary);
  display: -webkit-box;
  -webkit-line-clamp: 3;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
.overview-text.expanded {
  -webkit-line-clamp: unset;
  display: block;
}

.text-muted { color: var(--text-muted); }

/* ============ 剧集列表 ============ */
.episodes-section {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 14px;
  padding: 18px;
  margin-bottom: 20px;
}

.section-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 14px;
}
.section-head-left {
  display: flex;
  align-items: baseline;
  gap: 10px;
}
.section-head-right {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}
.section-head h3 {
  margin: 0;
  font-size: 16px;
  font-weight: 600;
}
.section-sub { font-size: 12px; color: var(--text-muted); }

/* 查看更多按钮 */
.show-more-btn {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 5px 12px;
  font-size: 12px;
  border: 1px solid var(--accent);
  background: var(--accent-alpha-10);
  color: var(--accent);
  border-radius: 14px;
  cursor: pointer;
  transition: all 0.2s ease;
  font-weight: 500;
}
.show-more-btn:hover {
  background: var(--bg-card);
  color: var(--text-secondary);
  border-color: var(--border);
  transform: none;
}
.show-more-arrow {
  font-size: 9px;
  opacity: 0.7;
}

.mode-toggle-btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 8px 14px;
  border-radius: 8px;
  border: 1px solid var(--accent);
  background: var(--accent-alpha-10);
  color: var(--accent);
  cursor: pointer;
  font-size: 12px;
  font-family: inherit;
  font-weight: 500;
  transition: all 0.15s ease;
}
.mode-toggle-btn:hover {
  border-color: var(--border);
  color: var(--text-secondary);
  background: var(--bg-secondary);
  transform: none;
}
.mode-toggle-btn.active {
  border-color: var(--accent);
  background: var(--accent);
  color: var(--accent-contrast);
}
.mode-toggle-btn.active:hover {
  background: var(--accent-alpha-10);
  color: var(--accent);
}
.mode-toggle-btn.secondary {
  background: var(--accent);
  border-color: var(--accent);
  color: var(--accent-contrast);
}
.mode-toggle-btn.secondary:hover {
  background: var(--accent-alpha-10);
  color: var(--accent);
}

.episodes-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(96px, 1fr));
  gap: 8px;
  align-content: start;
  max-height: 320px;
  overflow-y: auto;
  /* 隐藏滚动条但保留滚动功能 */
  scrollbar-width: none; /* Firefox */
  -ms-overflow-style: none; /* IE/Edge */
}
.episodes-grid::-webkit-scrollbar {
  display: none; /* Chrome/Safari */
}

.episode-btn {
  position: relative;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 4px;
  padding: 0 8px;
  height: 40px;
  border-radius: 8px;
  border: 1px solid var(--accent);
  background: var(--accent-alpha-10);
  color: var(--accent);
  cursor: pointer;
  font-size: 12px;
  font-family: inherit;
  font-weight: 500;
  transition: all 0.15s ease;
  text-align: center;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}

.episode-btn:hover {
  border-color: var(--border);
  color: var(--text-primary);
  background: var(--bg-secondary);
  transform: none;
}

.episode-btn.download-mode {
  color: var(--accent);
  border-color: var(--accent);
  background: var(--accent-alpha-10);
}
.episode-btn.download-mode:hover {
  background: var(--accent);
  color: var(--accent-contrast);
  border-color: var(--accent);
}

.episode-btn.in-download {
  background: var(--accent-alpha-10);
  border-color: var(--accent);
  color: var(--accent);
  opacity: 0.8;
  pointer-events: none;
}

/* 已观看态：按播放进度渲染底部进度条 + 百分比角标 */
.episode-btn.watched {
  background: var(--accent);
  border-color: var(--accent);
  color: var(--accent-contrast);
  font-weight: 500;
}
.episode-btn.watched:hover {
  background: var(--btn-soft);
  border-color: var(--accent);
  color: var(--btn-soft-text);
  box-shadow: 0 2px 8px var(--accent-alpha-35);
}

/* 已观看的底部进度条 */
.ep-progress-fill {
  position: absolute;
  left: 0;
  bottom: 0;
  height: 3px;
  background: rgba(255, 255, 255, 0.6);
  border-radius: 0 2px 2px 0;
  pointer-events: none;
  transition: width 0.25s ease;
}
.episode-btn:hover .ep-progress-fill {
  background: var(--accent);
}

/* 已观看的百分比角标 */
.ep-progress-pct {
  position: absolute;
  right: 6px;
  top: 4px;
  font-size: 10px;
  font-weight: 600;
  color: rgba(255, 255, 255, 0.9);
  background: rgba(0, 0, 0, 0.25);
  padding: 1px 5px;
  border-radius: 10px;
  line-height: 1.4;
  pointer-events: none;
}
.episode-btn:hover .ep-progress-pct {
  color: var(--accent);
  background: var(--accent-alpha-10);
}

.ep-num {
  pointer-events: none;
  font-weight: 500;
}

/* ============ 相似推荐 ============ */
.similar-section {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 14px;
  padding: 18px;
}

.similar-loading {
  padding: 30px;
  text-align: center;
}

.similar-empty {
  padding: 30px;
  color: var(--text-muted);
  font-size: 13px;
  text-align: center;
}

.similar-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(130px, 1fr));
  gap: 12px;
}

.similar-card {
  cursor: pointer;
  transition: transform 0.2s ease;
}

.similar-card:hover { transform: translateY(-3px); }
.similar-card:hover .similar-cover { border-color: var(--accent); }
.similar-card:hover .similar-overlay { opacity: 1; }
.similar-card:hover .similar-name { color: var(--accent); }

.similar-cover {
  position: relative;
  aspect-ratio: 2/3;
  border-radius: 10px;
  overflow: hidden;
  background: var(--bg-secondary);
  border: 1px solid var(--border);
  transition: border-color 0.15s ease;
}

.similar-cover img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

.similar-cover-empty {
  width: 100%;
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--text-muted);
  opacity: 0.45;
}

.similar-remarks {
  position: absolute;
  top: 6px;
  right: 6px;
  padding: 3px 8px;
  border-radius: 10px;
  background: var(--accent);
  color: var(--accent-contrast);
  font-size: 10px;
  font-weight: 600;
}

.similar-overlay {
  position: absolute;
  inset: 0;
  background: linear-gradient(to top, rgba(0, 0, 0, 0.5), transparent 60%);
  display: flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  opacity: 0;
  transition: opacity 0.15s ease;
}

.similar-name {
  margin-top: 8px;
  font-size: 12px;
  color: var(--text-primary);
  line-height: 1.35;
  text-align: center;
  overflow: hidden;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  transition: color 0.15s ease;
  font-weight: 500;
}

.similar-match {
  margin-top: 4px;
  text-align: center;
  font-size: 11px;
  color: var(--text-muted);
}

/* ============ 下载弹窗 ============ */
.modal-backdrop {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.55);
  backdrop-filter: blur(6px);
  z-index: 100;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  animation: fadeIn 0.18s ease;
}

@keyframes fadeIn {
  from { opacity: 0; }
  to { opacity: 1; }
}

.modal-box {
  width: 100%;
  max-width: 720px;
  max-height: 80vh;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 14px;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  box-shadow: 0 10px 40px rgba(0, 0, 0, 0.3);
  animation: scaleIn 0.2s cubic-bezier(0.4, 0, 0.2, 1);
}

@keyframes scaleIn {
  from { opacity: 0; transform: scale(0.96); }
  to { opacity: 1; transform: scale(1); }
}

.modal-head {
  padding: 16px 20px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  border-bottom: 1px solid var(--border);
}

.modal-title {
  margin: 0;
  font-size: 15px;
  font-weight: 600;
  color: var(--text-primary);
  display: inline-flex;
  align-items: center;
  gap: 8px;
}

.modal-close {
  background: transparent;
  border: none;
  color: var(--text-muted);
  cursor: pointer;
  padding: 4px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border-radius: 6px;
  transition: all 0.15s ease;
}
.modal-close:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

.modal-body {
  flex: 1;
  overflow-y: auto;
  padding: 16px 20px;
}

.modal-empty {
  padding: 30px;
  text-align: center;
  color: var(--text-muted);
  font-size: 13px;
}

.modal-error {
  display: flex;
  align-items: center;
  gap: 8px;
  background: rgba(255, 68, 68, 0.1);
  border: 1px solid rgba(255, 68, 68, 0.3);
  color: #ff6b6b;
  padding: 10px 14px;
  border-radius: 8px;
  font-size: 12px;
  margin-bottom: 14px;
}

.modal-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 12px;
  padding: 8px 12px;
  background: var(--bg-secondary);
  border-radius: 8px;
}

.modal-toolbar-tip {
  font-size: 12px;
  color: var(--text-muted);
}

.btn-download-all {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 6px 14px;
  border-radius: 6px;
  border: 1px solid var(--accent);
  background: var(--accent);
  color: var(--accent-contrast);
  cursor: pointer;
  font-size: 12px;
  font-weight: 500;
  font-family: inherit;
  transition: all 0.15s ease;
}
.btn-download-all:hover {
  background: var(--accent-alpha-10);
  border-color: var(--accent);
  color: var(--accent);
  transform: none;
}

.modal-episodes {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
  gap: 8px;
}

.modal-episode {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 12px;
  border-radius: 8px;
  border: 1px solid var(--accent);
  background: var(--accent-alpha-10);
  color: var(--accent);
  cursor: pointer;
  font-size: 12px;
  font-family: inherit;
  text-align: left;
  font-weight: 500;
  transition: all 0.15s ease;
}

.modal-episode:hover {
  border-color: var(--border);
  background: var(--bg-secondary);
  color: var(--text-primary);
  transform: none;
}

.modal-episode.downloading {
  background: var(--accent);
  border-color: var(--accent);
  color: var(--accent-contrast);
  pointer-events: none;
  opacity: 0.9;
}

.modal-ep-index {
  flex-shrink: 0;
  min-width: 28px;
  height: 28px;
  padding: 0 8px;
  border-radius: 6px;
  background: var(--accent);
  border: 1px solid var(--accent);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 11px;
  font-weight: 600;
  color: var(--accent-contrast);
}

.modal-episode:hover .modal-ep-index {
  background: var(--bg-card);
  border-color: var(--border);
  color: var(--text-muted);
}

.modal-ep-name {
  flex: 1;
  font-weight: 500;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.modal-ep-action {
  color: inherit;
  flex-shrink: 0;
}

.modal-episode:hover .modal-ep-action { color: var(--text-muted); }

.modal-tasks {
  margin-top: 20px;
  padding-top: 16px;
  border-top: 1px dashed var(--border);
}

.modal-tasks-title {
  display: flex;
  justify-content: space-between;
  align-items: center;
  font-size: 12px;
  color: var(--text-muted);
  font-weight: 500;
  margin-bottom: 10px;
}

.modal-tasks-count {
  font-weight: 600;
  color: var(--accent);
}

.modal-tasks-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.task-row {
  background: var(--bg-secondary);
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 10px 12px;
}

.task-name {
  font-size: 12px;
  font-weight: 500;
  margin-bottom: 6px;
  color: var(--text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.task-bar {
  height: 4px;
  background: var(--bg-card);
  border-radius: 2px;
  overflow: hidden;
  margin-bottom: 6px;
}

.task-bar-fill {
  height: 100%;
  background: var(--accent);
  border-radius: 2px;
  transition: width 0.3s ease;
}

.task-meta {
  font-size: 11px;
  color: var(--text-muted);
}

.modal-foot {
  padding: 14px 20px;
  border-top: 1px solid var(--border);
  display: flex;
  justify-content: flex-end;
  align-items: center;
  gap: 10px;
}
.modal-foot .btn-primary {
  margin-top: 0;
}

.btn-ghost {
  padding: 8px 20px;
  border-radius: 8px;
  border: 1px solid var(--border);
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
  font-size: 13px;
  font-family: inherit;
  transition: all 0.15s ease;
}
.btn-ghost:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
  border-color: var(--accent);
}

.btn-primary {
  margin-top: 16px;
  padding: 12px 28px;
  border-radius: 10px;
  border: none;
  background: var(--accent);
  color: var(--accent-contrast);
  cursor: pointer;
  font-size: 13px;
  font-weight: 600;
  font-family: inherit;
  transition: all 0.15s ease;
}

.btn-primary:hover {
  background: var(--accent-dim);
  transform: translateY(-1px);
  box-shadow: 0 4px 12px var(--accent-alpha-35);
}

/* 文件夹选择列表 */
.folder-select-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.folder-select-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 14px;
  border-radius: 8px;
  border: 1px solid var(--border);
  background: var(--bg-secondary);
  cursor: pointer;
  font-size: 13px;
  color: var(--text-primary);
  transition: all 0.15s ease;
  position: relative;
}

.folder-select-item:hover {
  border-color: var(--accent);
  background: var(--accent-alpha-10);
}

.folder-select-item.active {
  border-color: var(--accent);
  background: var(--accent);
  color: var(--accent-contrast);
  font-weight: 600;
}

.folder-select-item input {
  position: absolute;
  opacity: 0;
  width: 0;
  height: 0;
  pointer-events: none;
}
.folder-radio {
  flex-shrink: 0;
  width: 16px;
  height: 16px;
  border-radius: 50%;
  border: 2px solid var(--border-strong);
  background: var(--bg-card);
  transition: all 0.15s ease;
  position: relative;
}
.folder-select-item.active .folder-radio {
  border-color: var(--accent-contrast);
  background: var(--accent-contrast);
}
.folder-select-item.active .folder-radio::after {
  content: '';
  position: absolute;
  inset: 3px;
  border-radius: 50%;
  background: var(--accent);
}
.folder-name {
  flex: 1;
  min-width: 0;
}
</style>
