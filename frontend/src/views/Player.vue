<script setup lang="ts">
defineOptions({ name: 'Player' })
import { ref, computed, onMounted, onBeforeUnmount, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { GetRecentHistory, SaveWatchHistory, AddFavorite, RemoveFavorite, IsFavorite } from '../../bindings/cczjVideo/app'
import * as AppMod from '../../bindings/cczjVideo/app'
import { useSourceStore } from '../stores/source'
import { useVideoStore } from '../stores/video'
import VideoPlayer from '../components/VideoPlayer.vue'
import Icon from '../components/Icon.vue'
import { Button, Modal } from '../components/ui'
import { resolveEpisodeUrl, stripHtmlTags } from '../utils'
import { TsCache } from '../utils/tsCache'
import { epProgressKey, loadEpProgress, saveEpProgress, getEpProgressPct, flushEpProgress } from '../utils/episodeProgress'
import { bumpFavoritesRefresh } from '../stores/favoritesSync'
import { Window } from '@wailsio/runtime'
import type { HistoryItem } from '../types'

const route = useRoute()
const router = useRouter()
const sourceStore = useSourceStore()
const videoStore = useVideoStore()

// ==================== 路由参数解析 ====================
const vodId = computed(() => {
  const p = route.params.vodId
  if (Array.isArray(p)) return p[0] || ''
  return String(p || route.query.id || '')
})

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
const loading = ref(false)
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

const currentEpName = computed(() => {
  const ep = episodes.value[currentEpIndex.value]
  if (!ep) return ''
  if (ep.ep_name) return ep.ep_name
  const num = ep.ep_num ?? (currentEpIndex.value + 1)
  return '第' + String(num) + '集'
})

const hasPrev = computed(() => currentEpIndex.value > 0)
const hasNext = computed(() => currentEpIndex.value < episodes.value.length - 1)

/** 模糊匹配视频名称（去除空格、大小写统一、允许包含关系） */
function fuzzyMatchVod(name1: string, name2: string): boolean {
  const n1 = name1.replace(/\s+/g, '').toLowerCase()
  const n2 = name2.replace(/\s+/g, '').toLowerCase()
  if (n1 === n2) return true
  if (n1.includes(n2) || n2.includes(n1)) return true
  // 允许去掉常见后缀（第一季/第二季等）后匹配
  const seasonRe = /第[一二三四五六七八九十\d]+季$/
  const s1 = n1.replace(seasonRe, '')
  const s2 = n2.replace(seasonRe, '')
  if (s1 && s2 && (s1 === s2 || s1.includes(s2) || s2.includes(s1))) return true
  return false
}

function findBestMatch(list: any[], vodName: string): any | null {
  if (!list.length) return null
  // 精确匹配优先
  const exact = list.find((v: any) => v.vod_name === vodName)
  if (exact) return exact
  // 模糊匹配
  const fuzzy = list.find((v: any) => fuzzyMatchVod(v.vod_name || '', vodName))
  if (fuzzy) return fuzzy
  // 兆底第一条
  return list[0]
}

/** 搜索源并自动回退：原名 → 去空格版 → 去季后缀版 */
async function searchSourceWithFallback(sk: string, vodName: string): Promise<any[]> {
  // 第一次：原名搜索
  try {
    const resp = await (AppMod as any).SearchSource(sk, vodName, 10) as any
    const list = Array.isArray(resp?.videos) ? resp.videos : []
    if (list.length > 0) return list
  } catch { }

  // 第二次：去空格版搜索（解决“权力的游戏 第一季”vs“权力的游戏第一季”问题）
  const noSpaceName = vodName.replace(/\s+/g, '')
  if (noSpaceName !== vodName) {
    try {
      const resp2 = await (AppMod as any).SearchSource(sk, noSpaceName, 10) as any
      const list2 = Array.isArray(resp2?.videos) ? resp2.videos : []
      if (list2.length > 0) {
        console.log(`[Player] ✔ 去空格搜索 "${noSpaceName}" 找到 ${list2.length} 条结果`)
        return list2
      }
    } catch { }
  }

  // 第三次：去掉季后缀搜索（如“权力的游戏 第一季”→“权力的游戏”）
  const seasonRe = /第[一二三四五六七八九十\d]+季\s*$/
  const baseName = vodName.replace(seasonRe, '').trim()
  if (baseName && baseName !== vodName && baseName !== noSpaceName) {
    try {
      const resp3 = await (AppMod as any).SearchSource(sk, baseName, 10) as any
      const list3 = Array.isArray(resp3?.videos) ? resp3.videos : []
      if (list3.length > 0) {
        console.log(`[Player] ✔ 基名搜索 "${baseName}" 找到 ${list3.length} 条结果`)
        return list3
      }
    } catch { }
  }

  return []
}

/* ==================== 侧面板收起/展开 ==================== */
const sidePanelCollapsed = ref(false)

function onMinimizeApp(): void {
  try { Window.Minimise() } catch { /* 忽略 */ }
}

/* ==================== 源切换 ==================== */
interface SourceOption {
  source_key: string
  name: string
  vod_id?: string  // 该源中对应视频的 vod_id
  loaded: boolean   // 是否已加载过剧集
  hasData?: boolean // 是否确认该源有此视频（预搜索后设置）
}
const sourceOptions = ref<SourceOption[]>([])
const activeSourceKey = ref('')  // 当前激活的源 key
const sourceSearchLoading = ref(false)
const showEpisodes = ref(true)   // 源列表 vs 选集列表切换

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

/* ==================== TsCache 响应式（顶部栏片段进度） ====================
 * 每 250ms TsCache 可能有新片段缓存 → 触发此函数更新页面的 cached/total 显示
 */
const cacheReadTick = ref(0)
let _unsubTsCache: (() => void) | null = null

function refreshCacheUI(): void { cacheReadTick.value++ }

/* ==================== "已观看" 集数进度（从独立 localStorage 存储读取，1 个月自动淘汰） ====================
 * 键：`${sourceKey}-${vodId}-${epNum}`；值：{ position, duration?, updatedAt }
 * 每集独立一条记录，播放时持续写入，读取时自动淘汰过期记录。
 */
// 缓存每一集的进度百分比（0-100）
const epProgressMap = ref<Record<number, number>>({})

function epKeyOf(idx: number): string {
  return epProgressKey(video.value?.global_id, video.value?.vod_name, episodes.value[idx]?.ep_num ?? idx)
}

// 从独立存储刷新 UI 可见的进度百分比表
function refreshEpProgressUI(): void {
  try {
    const store = loadEpProgress()
    const out: Record<number, number> = {}
    for (let i = 0; i < episodes.value.length; i++) {
      const k = epKeyOf(i)
      if (store[k]) out[i] = getEpProgressPct(store[k])
    }
    epProgressMap.value = out
  } catch { /* ignore */ }
}

// 播放开始时先执行一次（此时 episodes 可能为空，后面 loadData 会再刷一次）
refreshEpProgressUI()

function getEpWatchPct(idx: number): number {
  return epProgressMap.value[idx] ?? 0
}
function isWatchedEp(idx: number): boolean { return getEpWatchPct(idx) > 0 }

let lastHistorySyncAt = 0
const HISTORY_SYNC_INTERVAL_MS = 10000

function syncHistoryToDb(position: number, force = false): void {
  if (!force) {
    const now = Date.now()
    if (now - lastHistorySyncAt < HISTORY_SYNC_INTERVAL_MS) return
    lastHistorySyncAt = now
  }
  if (!vodId.value || currentEpIndex.value < 0) return
  const ep = episodes.value[currentEpIndex.value]
  if (!ep) return
  SaveWatchHistory({
    source_key: sourceKey.value,
    vod_id: String(vodId.value),
    vod_name: video.value?.vod_name || '',
    ep_num: ep.ep_num ?? (currentEpIndex.value + 1),
    position,
  } as any).catch(() => { })
}

// 写入当前集播放时持续写入独立存储（position/duration 会在用户播放过程中不断被写入
function updateCurrentEpProgress(position: number, duration?: number): void {
  if (currentEpIndex.value < 0) return
  const k = epKeyOf(currentEpIndex.value)
  saveEpProgress(k, position, duration)
  syncHistoryToDb(position)
  // 同时同步 UI
  const store = loadEpProgress()
  const out: Record<number, number> = { ...epProgressMap.value }
  out[currentEpIndex.value] = getEpProgressPct(store[k])
  epProgressMap.value = out
  // 派发自定义事件，供详情页等其他页面监听以实时同步已观看状态
  try {
    window.dispatchEvent(new CustomEvent('cczj-ep-progress-updated', {
      detail: { key: k, position, duration },
    }))
  } catch { /* ignore */ }
}

// Debug: 跟踪进度更新频率
let _epUpdateCount = 0

/* ==================== 自定义快捷键 ==================== */
interface ShortcutMap {
  togglePlay: string[]
  seekBack: string[]
  seekForward: string[]
  seekBackBig: string[]
  seekForwardBig: string[]
  volumeUp: string[]
  volumeDown: string[]
  mute: string[]
  speedUp: string[]
  speedDown: string[]
  fullscreen: string[]
  prevEp: string[]
  nextEp: string[]
  pip: string[]
}

const DEFAULT_SHORTCUTS: ShortcutMap = {
  togglePlay: ['Space', 'K', 'k'],
  seekBack: ['ArrowLeft'],
  seekForward: ['ArrowRight'],
  seekBackBig: ['J', 'j'],
  seekForwardBig: ['L', 'l'],
  volumeUp: ['ArrowUp'],
  volumeDown: ['ArrowDown'],
  mute: ['M', 'm'],
  speedUp: [']'],
  speedDown: ['['],
  fullscreen: ['F', 'f'],
  prevEp: ['P', 'p'],
  nextEp: ['N', 'n'],
  pip: ['I', 'i'],
}

function loadShortcuts(): ShortcutMap {
  try {
    const raw = localStorage.getItem('cczj_shortcuts')
    if (raw) {
      const parsed = JSON.parse(raw)
      if (parsed && typeof parsed === 'object') {
        // 合并默认值，确保所有键都存在
        const merged = { ...DEFAULT_SHORTCUTS }
        for (const key of Object.keys(merged) as (keyof ShortcutMap)[]) {
          if (Array.isArray(parsed[key]) && parsed[key].length > 0) {
            // 将 Space 规范化，并且同时加入小写版本（对于字母键）
            const keys: string[] = []
            for (const k of parsed[key]) {
              keys.push(k)
              if (k.length === 1) keys.push(k.toLowerCase())
            }
            merged[key] = keys
          }
        }
        return merged
      }
    }
  } catch { }
  return DEFAULT_SHORTCUTS
}

function normalizeKey(key: string): string {
  if (key === ' ') return 'Space'
  return key
}

function matchShortcut(action: keyof ShortcutMap, key: string, shortcuts: ShortcutMap): boolean {
  const normalizedKey = normalizeKey(key)
  return shortcuts[action].some(k => k === normalizedKey || k.toLowerCase() === key.toLowerCase())
}

/* ==================== 鼠标位置跟踪（用于键盘快捷键作用域） ==================== */
const mouseInside = ref(false)
function onPageKeyDown(e: KeyboardEvent): void {
  // 键盘焦点在输入框时不响应
  const activeTag = (document.activeElement?.tagName || '').toLowerCase()
  if (activeTag === 'input' || activeTag === 'textarea') return

  // ESC 总是允许退出全屏（由 VideoPlayer 组件内部处理）
  // 其他键需要鼠标位于播放器区域内才响应
  const v = document.querySelector('.native-video') as HTMLVideoElement | null
  if (!v) return

  const sc = loadShortcuts()
  const key = e.key

  if (matchShortcut('togglePlay', key, sc)) {
    if (!mouseInside.value) return
    e.preventDefault()
    if (v.paused) v.play()
    else v.pause()
  } else if (matchShortcut('seekBack', key, sc)) {
    if (!mouseInside.value) return
    e.preventDefault()
    v.currentTime = Math.max(0, v.currentTime - 5)
    v.dispatchEvent(new CustomEvent('cczj-seek-osd', { detail: { delta: -5 } }))
  } else if (matchShortcut('seekForward', key, sc)) {
    if (!mouseInside.value) return
    e.preventDefault()
    v.currentTime = Math.min(v.duration || 0, v.currentTime + 5)
    v.dispatchEvent(new CustomEvent('cczj-seek-osd', { detail: { delta: 5 } }))
  } else if (matchShortcut('seekBackBig', key, sc)) {
    if (!mouseInside.value) return
    e.preventDefault()
    v.currentTime = Math.max(0, v.currentTime - 30)
    v.dispatchEvent(new CustomEvent('cczj-seek-osd', { detail: { delta: -30 } }))
  } else if (matchShortcut('seekForwardBig', key, sc)) {
    if (!mouseInside.value) return
    e.preventDefault()
    v.currentTime = Math.min(v.duration || 0, v.currentTime + 30)
    v.dispatchEvent(new CustomEvent('cczj-seek-osd', { detail: { delta: 30 } }))
  } else if (matchShortcut('volumeUp', key, sc)) {
    if (!mouseInside.value) return
    e.preventDefault()
    v.volume = Math.min(1, v.volume + 0.05)
  } else if (matchShortcut('volumeDown', key, sc)) {
    if (!mouseInside.value) return
    e.preventDefault()
    v.volume = Math.max(0, v.volume - 0.05)
  } else if (matchShortcut('mute', key, sc)) {
    if (!mouseInside.value) return
    e.preventDefault()
    v.muted = !v.muted
  } else if (matchShortcut('speedUp', key, sc)) {
    if (!mouseInside.value) return
    e.preventDefault()
    const newRate = Math.min(4, v.playbackRate + 0.25)
    v.playbackRate = Math.round(newRate * 100) / 100
  } else if (matchShortcut('speedDown', key, sc)) {
    if (!mouseInside.value) return
    e.preventDefault()
    const newRate = Math.max(0.25, v.playbackRate - 0.25)
    v.playbackRate = Math.round(newRate * 100) / 100
  } else if (matchShortcut('fullscreen', key, sc)) {
    if (!mouseInside.value) return
    e.preventDefault()
    if (!document.fullscreenElement) v.requestFullscreen?.()
    else document.exitFullscreen?.()
  } else if (matchShortcut('pip', key, sc)) {
    if (!mouseInside.value) return
    e.preventDefault()
    if (v.readyState < 1) return
    try {
      if (document.pictureInPictureElement) {
        document.exitPictureInPicture()
      } else {
        (v as any).requestPictureInPicture?.()
      }
    } catch { }
  } else if (matchShortcut('prevEp', key, sc)) {
    if (hasPrev.value) {
      e.preventDefault()
      prevEpisode()
    }
  } else if (matchShortcut('nextEp', key, sc)) {
    if (hasNext.value) {
      e.preventDefault()
      nextEpisode()
    }
  } else if (e.key === 'Escape') {
    // 保留给系统和 VideoPlayer 内部使用（退出全屏）
  }
}

// 监听视频元素的 timeupdate 和 durationchange（用于进度条）
// 使用重试机制：VideoPlayer 组件挂载可能超过 200ms，轮询直到找到 video 元素
let _videoTrackRetries = 0
const MAX_VIDEO_TRACK_RETRIES = 20
let _videoTrackTimer: ReturnType<typeof setTimeout> | null = null

function bindVideoTimeTracking(): void {
  if (_videoTrackTimer) { clearTimeout(_videoTrackTimer); _videoTrackTimer = null }
  _videoTrackRetries = 0
  const v = document.querySelector('.native-video') as HTMLVideoElement | null
  if (!v) {
    _videoTrackRetries++
    if (_videoTrackRetries <= MAX_VIDEO_TRACK_RETRIES) {
      _videoTrackTimer = setTimeout(bindVideoTimeTracking, 300)
    } else {
      console.warn('[Player] 视频元素未找到，进度追踪未启动（已重试 ' + MAX_VIDEO_TRACK_RETRIES + ' 次）')
    }
    return
  }
  try { delete (v as any).__epProgressBound } catch { /* ignore */ }
  ; (v as any).__epProgressBound = true
  console.log('[Player] ✔ 进度追踪已绑定到 video 元素')
  v.addEventListener('timeupdate', () => {
    _epUpdateCount++
    updateCurrentEpProgress(v.currentTime, v.duration)
    if (_epUpdateCount % 20 === 1) {
      const k = epKeyOf(currentEpIndex.value)
      console.log(`[Player] ✔ timeupdate #${_epUpdateCount}: key="${k}", time=${v.currentTime.toFixed(1)}s, dur=${v.duration?.toFixed(1) || '?'}`)
    }
  })
  v.addEventListener('loadedmetadata', () => {
    console.log(`[Player] ✔ loadedmetadata: dur=${v.duration?.toFixed(1) || '?'}`)
    updateCurrentEpProgress(v.currentTime, v.duration)
  })
}

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
  if (isFav.value) {
    favBusy.value = true
    try {
      await RemoveFavorite({ source_key: sourceKey.value, vod_id: String(vodId.value) })
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
    finally { favBusy.value = false }
    return
  }
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
  finally { favBusy.value = false }
}

/* ==================== 记录观看历史（点击就记录，无需等 30 秒 ==================== */
function recordHistory(idx: number): void {
  if (!vodId.value) return
  const ep = episodes.value[idx]
  if (!ep) return
  const epNum = ep.ep_num ?? (idx + 1)
  const progKey = epProgressKey(video.value?.global_id, video.value?.vod_name, epNum)
  const entry = loadEpProgress()[progKey]
  const position = entry?.position ?? 0
  lastHistorySyncAt = Date.now()
  try {
    SaveWatchHistory({
      source_key: sourceKey.value,
      vod_id: String(vodId.value),
      vod_name: video.value?.vod_name || '',
      ep_num: epNum,
      position,
    } as any).catch(() => { })
  } catch { /* ignore */ }
}

// ==================== 简介文本（去除 HTML 标签） ====================
const overviewText = computed(() => stripHtmlTags(video.value?.vod_content || ''))

// ==================== 集数切换 & 路由更新 ====================
function goToEpisode(idx: number): void {
  if (idx < 0 || idx >= episodes.value.length) return
  if (idx === currentEpIndex.value) return
  try {
    const v = document.querySelector('.native-video') as HTMLVideoElement | null
    if (v && !isNaN(v.currentTime)) {
      syncHistoryToDb(v.currentTime, true)
    }
  } catch { /* ignore */ }
  recordHistory(idx)
  currentEpIndex.value = idx
  _playToken.value++
  try { TsCache.setCurrentEpisode(idx) } catch { }
  try {
    const v = document.querySelector('.native-video') as HTMLVideoElement | null
    if (v) delete (v as any).__epProgressBound
  } catch { /* ignore */ }
  setTimeout(() => bindVideoTimeTracking(), 300)
  router.replace(`/player/${sourceKey.value}/${vodId.value}/${idx}`).catch(() => { })
}
function prevEpisode(): void { if (hasPrev.value) goToEpisode(currentEpIndex.value - 1) }
function nextEpisode(): void { if (hasNext.value) goToEpisode(currentEpIndex.value + 1) }

function goBack(): void {
  router.back()
}

// ==================== 顶部栏读取当前集预取进度 ====================
function getCurrentEpCached(): { cached: number; total: number } {
  cacheReadTick.value // 订阅：触发响应式重算
  try { return TsCache.episodeProgress(currentEpIndex.value) } catch { return { cached: 0, total: 0 } }
}
function getHitRate(): number {
  cacheReadTick.value
  try { return TsCache.stats().hitRate } catch { return 0 }
}

// ==================== 加载流程 ====================
async function loadData(): Promise<void> {
  if (!sourceKey.value || !vodId.value) return
  loading.value = true
  try {
    const currentVodId = video.value?.vod_id
    if (String(currentVodId || '') !== String(vodId.value)) {
      await videoStore.loadDetail(sourceKey.value, vodId.value)
    }
    if (!video.value) { loading.value = false; return }

    let targetIdx = 0
    if (!isNaN(epIndexParam.value) && epIndexParam.value >= 0 && epIndexParam.value < episodes.value.length) {
      targetIdx = epIndexParam.value
    } else {
      try {
        const history = (await GetRecentHistory(1)) as HistoryItem[] | null | undefined
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

// ==================== 源切换逻辑（基于 global_id 查找，不再依赖远端名称搜索） ====================
async function buildSourceOptions(): Promise<void> {
  if (!sourceStore.sources.length) return
  const vodName = video.value?.vod_name || ''
  const opts: SourceOption[] = sourceStore.sources
    .map((s: any) => ({
      source_key: s.source_key || '',
      name: s.name || s.source_key || '',
      vod_id: s.source_key === sourceKey.value ? String(vodId.value) : undefined,
      loaded: s.source_key === sourceKey.value,
      hasData: s.source_key === sourceKey.value, // 当前源默认有数据
    }))
  sourceOptions.value = opts
  activeSourceKey.value = sourceKey.value

  // 优先通过 global_id 查找所有拥有该视频的源（本地查询，无需网络请求）
  try {
    const globalId = await (AppMod as any).GetGlobalIdForVideo(sourceKey.value, String(vodId.value)) as number
    if (globalId > 0) {
      const refs = await (AppMod as any).FindSourcesByGlobalId(globalId) as any[]
      if (Array.isArray(refs) && refs.length > 0) {
        console.log(`[Player] ✔ global_id=${globalId} 找到 ${refs.length} 个源:`, refs.map((r: any) => r.source_key).join(', '))
        sourceOptions.value = opts.map((o) => {
          if (o.loaded) return o
          const ref = refs.find((r: any) => r.source_key === o.source_key)
          if (ref && ref.vod_id) {
            o.hasData = true
            o.vod_id = String(ref.vod_id)
          }
          return o
        })
        return // global_id 已找到所有源，无需远端搜索
      }
    }
  } catch (e) {
    console.log('[Player] global_id 查找失败，回退到远端搜索:', e)
  }

  // 回退：远端 API 搜索（当 global_id 不可用时）
  if (!vodName) return
  const otherOpts = opts.filter((o) => !o.loaded)
  if (otherOpts.length === 0) return

  const results = await Promise.allSettled(
    otherOpts.map(async (o) => {
      try {
        const list = await searchSourceWithFallback(o.source_key, vodName)
        const match = findBestMatch(list, vodName)
        return { opt: o, vodId: match?.vod_id ? String(match.vod_id) : null }
      } catch {
        return { opt: o, vodId: null }
      }
    })
  )

  sourceOptions.value = opts.map((o) => {
    if (o.loaded) return o
    const result = results.find((r) => r.status === 'fulfilled' && r.value?.opt?.source_key === o.source_key)
    if (result && result.status === 'fulfilled' && result.value.vodId != null) {
      o.hasData = true
      o.vod_id = result.value.vodId
    }
    return o
  })
}

async function switchToSource(sk: string): Promise<void> {
  if (!sk || sk === activeSourceKey.value) return
  sourceSearchLoading.value = true
  try {
    // 如果 pre-search 已找到 vod_id，直接切换（来自 global_id 查找或远端搜索）
    const existing = sourceOptions.value.find((s) => s.source_key === sk)
    if (existing?.vod_id) {
      await loadFromSource(sk, existing.vod_id)
      return
    }
    // 回退：远端搜索该源中的同名视频
    const vodName = video.value?.vod_name || ''
    if (!vodName) return
    const list = await searchSourceWithFallback(sk, vodName)
    const match = findBestMatch(list, vodName)
    if (!match?.vod_id) {
      console.log(`[Player] ❗ 源 "${sk}" 中未找到 "${vodName}"`)
      return
    }
    const idx = sourceOptions.value.findIndex((s) => s.source_key === sk)
    if (idx >= 0) {
      sourceOptions.value[idx].vod_id = String(match.vod_id)
      sourceOptions.value[idx].loaded = true
    }
    await loadFromSource(sk, String(match.vod_id))
  } catch (e) {
    console.error('[Player] 切换源失败:', e)
  } finally {
    sourceSearchLoading.value = false
  }
}

async function loadFromSource(sk: string, vid: string): Promise<void> {
  activeSourceKey.value = sk
  const prevEpNum = episodes.value[currentEpIndex.value]?.ep_num ?? undefined
  loading.value = true
  try {
    await videoStore.loadDetail(sk, vid)
    if (!video.value || !episodes.value.length) {
      loading.value = false
      return
    }

    // 尽量保持当前集数：按 ep_num 匹配，匹配不到则回退到第 0 集
    let targetIdx = 0
    if (prevEpNum != null) {
      const idx = episodes.value.findIndex((e) => Number(e.ep_num) === Number(prevEpNum))
      if (idx >= 0) targetIdx = idx
    }
    currentEpIndex.value = targetIdx
    _playToken.value++
    showEpisodes.value = true

    // 更新路由
    router.replace(`/player/${sk}/${vid}/${targetIdx}`).catch(() => { })

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
    if (targetIdx >= 0) recordHistory(targetIdx)
  } catch (e) {
    console.error('[Player] loadFromSource 失败:', e)
  } finally {
    loading.value = false
  }
}

watch(currentEpIndex, (idx) => {
  // 播放中 → 把该集加入进度（即使没有具体位置，也标记为"开始看过"）
  if (idx >= 0 && !isWatchedEp(idx)) {
    const k = epKeyOf(idx)
    saveEpProgress(k, 0, undefined)
    refreshEpProgressUI()
  }
  try { TsCache.setCurrentEpisode(idx) } catch { }
})

// 源切换/重新加载后，VideoPlayer 被销毁重建，需重新绑定 timeupdate 监听
watch(loading, (val) => {
  if (!val) {
    bindVideoTimeTracking()
  }
})

// 页面可见性变化时刷新进度（从其他页面返回时同步）
function onVisibilityChange(): void {
  if (document.visibilityState === 'visible') {
    refreshEpProgressUI()
  }
}

onMounted(async () => {
  await sourceStore.loadSources()
  await loadData()
  await buildSourceOptions()
  // 首集（或恢复的上一集）也记录历史
  if (currentEpIndex.value >= 0 && episodes.value.length > 0) {
    recordHistory(currentEpIndex.value)
  }
  // 从后端加载已观看集数（含 position）作为补充
  try {
    const history = await GetRecentHistory(50) as any[]
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
            const local = loadEpProgress()
            if (!local[k]) saveEpProgress(k, Number(h.position) || 0, undefined)
          }
        }
      }
      refreshEpProgressUI()
    }
    // 当前集至少也要"已观看过"
    if (currentEpIndex.value >= 0 && !isWatchedEp(currentEpIndex.value)) {
      saveEpProgress(epKeyOf(currentEpIndex.value), 0, undefined)
      refreshEpProgressUI()
    }
  } catch { /* ignore */ }
  // 刷新收藏状态
  refreshFav().catch(() => { })
  try {
    _unsubTsCache = TsCache.onStateChange(refreshCacheUI)
  } catch { }

  // 页面级键盘监听
  document.addEventListener('keydown', onPageKeyDown)

  // 视频时间跟踪（延迟到 VideoPlayer 挂载后）
  setTimeout(() => { bindVideoTimeTracking() }, 200)

  // 页面可见性变化时刷新进度（从其他页面返回时同步）
  document.addEventListener('visibilitychange', onVisibilityChange)
})

onBeforeUnmount(() => {
  flushEpProgress()
  console.log(`[Player] ✔ 组件卸载，进度已 flush 到 localStorage (共 ${_epUpdateCount} 次 timeupdate)`)
  try {
    const v = document.querySelector('.native-video') as HTMLVideoElement | null
    if (v && !isNaN(v.currentTime)) {
      syncHistoryToDb(v.currentTime, true)
      console.log(`[Player] ✔ 强制同步历史记录到后端: position=${v.currentTime.toFixed(1)}s`)
    }
  } catch { /* ignore */ }
  try { window.dispatchEvent(new CustomEvent('cczj-ep-progress-flushed')) } catch { /* ignore */ }
  document.removeEventListener('keydown', onPageKeyDown)
  document.removeEventListener('visibilitychange', onVisibilityChange)
  if (_unsubTsCache) {
    try { _unsubTsCache() } catch { }
    _unsubTsCache = null
  }
  if (_videoTrackTimer != null) {
    clearTimeout(_videoTrackTimer)
    _videoTrackTimer = null
  }
})

/* ==================== 小工具：生成集数显示 ==================== */
function epLabel(i: number, ep: { ep_num?: number; ep_name?: string }): string {
  if (ep.ep_name) return ep.ep_name
  const num = ep.ep_num ?? (i + 1)
  return '第' + String(num) + '集'
}
</script>

<template>
  <div class="player-page" @mouseenter="mouseInside = true" @mouseleave="mouseInside = false">
    <div v-if="loading" class="player-loading">
      <div class="spinner"></div>
      <span>加载中...</span>
    </div>

    <template v-else-if="currentUrl">
      <div class="player-layout">
        <!-- ============= 左侧视频区 ============= -->
        <div class="player-col-main" @mouseenter="mouseInside = true" @dblclick.stop>
          <VideoPlayer :url="currentUrl" :autoplay="true" :has-prev="hasPrev" :has-next="hasNext"
            :video-key="currentVideoKey" :show-title-bar="true" :title="currentEpName" :is-fav="isFav"
            :fav-busy="favBusy" @toggle-favorite="toggleFavorite" @back="goBack" @prev="prevEpisode" @next="nextEpisode"
            :force-play-token="_playToken" />


          <!-- 侧面板折叠/展开按钮（视频区右侧中间） -->
          <button class="panel-toggle-btn" :title="sidePanelCollapsed ? '展开面板' : '收起面板'"
            @click="sidePanelCollapsed = !sidePanelCollapsed">
            <Icon :name="sidePanelCollapsed ? 'chevron-left' : 'chevron-right'" :size="14" />
          </button>
        </div>

        <!-- ============= 右侧选集面板 ============= -->
        <aside class="player-col-side" :class="{ collapsed: sidePanelCollapsed }">
          <!-- 顶部卡：标题 + 年份/地区/分类 + 简介 + 收藏按钮 -->
          <div class="side-header">
            <div class="side-top-bar">
              <h1 class="side-title">{{ video?.vod_name || '视频播放' }}</h1>
              <div class="side-top-actions">
                <button class="close-btn-panel" title="最小化" @click="onMinimizeApp">
                  <Icon name="minimize" :size="14" />
                </button>
                <Button variant="text" size="sm" class="close-btn-panel" @click="flushEpProgress(); router.back()"
                  title="关闭">
                  <Icon name="x" :size="18" />
                </Button>
              </div>
            </div>
            <div class="side-meta">
              <span v-if="video?.vod_year" class="side-meta-chip">{{ video.vod_year }}</span>
              <span v-if="video?.vod_area" class="side-meta-chip">{{ video.vod_area }}</span>
              <span v-if="video?.type_name" class="side-meta-chip">{{ video.type_name }}</span>
            </div>
            <p v-if="overviewText" class="side-blurb">{{ overviewText }}</p>
          </div>

          <!-- 选源区 + 选集区 -->
          <section class="side-section">
            <!-- 源列表 -->
            <div class="side-section-title">
              <div class="side-section-title-left">
                <span class="bullet"></span>
                <span>播放源</span>
              </div>
              <div class="side-section-right">
                <span class="side-count">{{sourceOptions.filter(s => s.hasData).length}} 个可用源</span>
              </div>
            </div>

            <div class="source-list">
              <button v-for="src in sourceOptions.filter(s => s.hasData)" :key="src.source_key" class="source-item"
                :class="{ active: src.source_key === activeSourceKey }" @click="switchToSource(src.source_key)"
                :disabled="sourceSearchLoading">
                <span class="source-name">{{ src.name }}</span>
                <span v-if="src.source_key === activeSourceKey" class="source-active-dot"></span>
                <span v-if="sourceSearchLoading && src.source_key !== activeSourceKey"
                  class="source-loading-dot">…</span>
              </button>
            </div>

            <!-- 选集区（仅当有剧集时显示） -->
            <template v-if="episodes.length > 0">
              <div class="side-section-title" style="margin-top: 16px">
                <div class="side-section-title-left">
                  <span class="bullet"></span>
                  <span>选集</span>
                  <span class="side-count">共 {{ episodes.length }} 集</span>
                </div>
                <div class="side-section-right">
                  <button class="sort-toggle-btn" :title="episodeSortAsc ? '当前正序，点击切换倒序' : '当前倒序，点击切换正序'"
                    @click="toggleEpisodeSort">
                    <Icon :name="episodeSortAsc ? 'chevron-down' : 'chevron-up'" :size="12" />
                    <span class="sort-label">{{ episodeSortAsc ? '正序' : '倒序' }}</span>
                  </button>
                </div>
              </div>

              <div class="ep-grid">
                <button v-for="(ep, i) in sortedEpisodes" :key="String(i)" class="ep-item" :class="{
                  active: origIdx(i) === currentEpIndex,
                  watched: isWatchedEp(origIdx(i)),
                  future: origIdx(i) > currentEpIndex && !isWatchedEp(origIdx(i)),
                }" @click="goToEpisode(origIdx(i))"
                  :title="epLabel(origIdx(i), ep) + (getEpWatchPct(origIdx(i)) > 0 ? ' · 已观看 ' + Math.round(getEpWatchPct(origIdx(i))) + '%' : '')">
                  <span class="ep-item-num">{{ epLabel(origIdx(i), ep) }}</span>
                  <span v-show="origIdx(i) === currentEpIndex" class="ep-playing-badge">
                    <span class="bar b1"></span>
                    <span class="bar b2"></span>
                    <span class="bar b3"></span>
                  </span>
                  <span v-show="isWatchedEp(origIdx(i)) && getEpWatchPct(origIdx(i)) > 0" class="ep-watched-progress"
                    :style="{ width: getEpWatchPct(origIdx(i)) + '%' }"></span>
                  <span v-show="isWatchedEp(origIdx(i)) && getEpWatchPct(origIdx(i)) > 0" class="ep-watched-pct">{{
                    Math.round(getEpWatchPct(origIdx(i))) }}%</span>
                </button>
              </div>
            </template>
            <div v-else-if="!sourceSearchLoading" class="side-empty">暂无可播放剧集</div>
          </section>
        </aside>
      </div>
    </template>

    <div v-else class="player-error-page">
      <div class="error-msg">暂无播放资源</div>
      <Button variant="text" size="md" @click="goBack"><span>返回</span></Button>
    </div>

    <!-- 收藏夹选择弹窗 -->
    <Modal :model-value="showFavFolderModal" title="收藏到文件夹" width="420px" :show-footer="true"
      @update:model-value="(v: boolean) => !v && (showFavFolderModal = false)">
      <div class="folder-select-list">
        <label v-for="folder in favFolders" :key="folder.id" class="folder-select-item"
          :class="{ active: favTargetFolderId === folder.id }">
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
/* ============ 根层：铺满整屏 ============== */
.player-page {
  position: fixed;
  inset: 0;
  background: #0b0d10;
  color: #fff;
  overflow: hidden;
  z-index: 9999;
}

/* ============ 布局：左侧视频，右侧选集 ============== */
.player-layout {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: stretch;
  background: radial-gradient(ellipse at 30% 30%, #12161b 0%, #0b0d10 60%, #060709 100%);
}

.player-col-main {
  flex: 1 1 auto;
  min-width: 0;
  min-height: 0;
  position: relative;
  background: #000;
  display: flex;
  align-items: stretch;
  justify-content: stretch;
  padding: 0;
  margin: 0;
}

.player-col-main>* {
  width: 100%;
  height: 100%;
  flex: 1 1 auto;
}

.player-col-side {
  flex: 0 0 360px;
  max-width: 380px;
  background: linear-gradient(180deg, #111419 0%, #0b0d10 100%);
  display: flex;
  flex-direction: column;
  overflow: hidden;
  transition: flex-basis 0.3s ease, max-width 0.3s ease, opacity 0.3s ease;
}

.player-col-side.collapsed {
  flex: 0 0 0;
  max-width: 0;
  opacity: 0;
  pointer-events: none;
  overflow: hidden;
}

/* ============ 视频区右侧折叠按钮 ============== */
.panel-toggle-btn {
  position: absolute;
  top: 50%;
  right: 0;
  transform: translateY(-50%);
  z-index: 30;
  width: 22px;
  height: 56px;
  border-radius: 11px 0 0 11px;
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-right: none;
  background: rgba(10, 12, 16, 0.75);
  backdrop-filter: blur(6px);
  color: rgba(255, 255, 255, 0.5);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: all 0.25s ease;
  padding: 0;
  font-family: inherit;
}

.panel-toggle-btn:hover {
  width: 28px;
  color: #fff;
  background: rgba(24, 144, 255, 0.35);
  border-color: rgba(24, 144, 255, 0.5);
}

.side-top-actions {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-shrink: 0;
}

/* 响应式：窄屏收起右侧 */
@media (max-width: 960px) {
  .player-col-side {
    display: none;
  }
}

/* ============ 右侧：标题/简介区 ============== */
.side-header {
  padding: 24px 22px 18px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.05);
  background: linear-gradient(180deg, rgba(24, 144, 255, 0.04) 0%, transparent 100%);
}

.side-top-bar {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 12px;
  margin-bottom: 10px;
}

.side-title {
  margin: 0;
  flex: 1;
  font-size: 20px;
  font-weight: 700;
  letter-spacing: 0.3px;
  line-height: 1.35;
  color: #fff;
  text-shadow: 0 1px 8px rgba(0, 0, 0, 0.35);
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.close-btn-panel {
  flex-shrink: 0;
  width: 30px;
  height: 30px;
  border-radius: 8px;
  border: 1px solid rgba(255, 255, 255, 0.12);
  background: rgba(255, 255, 255, 0.04);
  color: rgba(255, 255, 255, 0.55);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: all 0.2s ease;
}

.close-btn-panel:hover {
  color: #ff4d4f;
  border-color: rgba(255, 77, 79, 0.5);
  background: rgba(255, 77, 79, 0.12);
}

/* ============ 源列表（横向排布） ============== */
.source-list {
  display: flex;
  flex-wrap: nowrap;
  gap: 6px;
  margin-bottom: 8px;
  overflow-x: auto;
  overflow-y: hidden;
  flex-shrink: 0;
  /* padding 给 active 按钮的 box-shadow 光晕 + translateY 动画留出空间（防止 overflow-y:hidden 裁剪） */
  padding: 16px 0 22px 0;
  /* 隐藏滚动条但保留滚动能力 */
  scrollbar-width: none;
  -ms-overflow-style: none;
}

.source-list::-webkit-scrollbar {
  display: none;
  width: 0;
  height: 0;
}

.source-item {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  padding: 8px 16px;
  border-radius: 6px;
  border: 1.5px solid rgba(255, 255, 255, 0.12);
  background: rgba(255, 255, 255, 0.03);
  color: rgba(255, 255, 255, 0.65);
  font-size: 12.5px;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.15s ease;
  font-family: inherit;
  white-space: nowrap;
  flex-shrink: 0;
  position: relative;
  overflow: hidden;
}

.source-item:hover {
  background: rgba(255, 255, 255, 0.05);
  border-color: rgba(255, 255, 255, 0.18);
  color: #fff;
}

.source-item.active {
  background: linear-gradient(135deg, rgba(24, 144, 255, 0.15) 0%, rgba(64, 169, 255, 0.08) 100%);
  border-color: rgba(64, 169, 255, 0.5);
  color: #69c0ff;
  font-weight: 600;
  box-shadow: 0 0 0 1px rgba(64, 169, 255, 0.3), 0 0 12px rgba(24, 144, 255, 0.25);
  transform: scale(1.03);
  animation: source-active-glow 1.6s ease-in-out infinite;
}

/* 激活源左侧 3px 蓝色竖条 */
.source-item.active::before {
  content: '';
  position: absolute;
  left: 0;
  top: 20%;
  bottom: 20%;
  width: 3px;
  background: linear-gradient(180deg, #40a9ff, #1890ff);
  border-radius: 0 2px 2px 0;
}

@keyframes source-active-glow {
  0%, 100% {
    box-shadow: 0 0 0 1px rgba(64, 169, 255, 0.3), 0 0 8px rgba(24, 144, 255, 0.2);
  }
  50% {
    box-shadow: 0 0 0 1px rgba(100, 181, 246, 0.5), 0 0 18px rgba(24, 144, 255, 0.4);
  }
}

.source-item:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.source-name {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.source-active-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: #40a9ff;
  box-shadow: 0 0 6px #40a9ff;
  flex-shrink: 0;
  animation: glow-pulse 1.4s ease-in-out infinite;
}

.source-loading-dot {
  font-size: 14px;
  color: rgba(255, 255, 255, 0.4);
  animation: glow-pulse 0.8s ease-in-out infinite;
  flex-shrink: 0;
}

.fav-btn-text {
  width: 100%;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  padding: 9px 14px;
  margin-bottom: 12px;
  border-radius: 10px;
  border: 1px solid rgba(255, 255, 255, 0.1);
  background: rgba(255, 255, 255, 0.05);
  color: rgba(255, 255, 255, 0.75);
  cursor: pointer;
  font-size: 13px;
  font-weight: 500;
  font-family: inherit;
  transition: all 0.2s ease;
}

.fav-btn-text .fav-icon {
  font-size: 14px;
}

.fav-btn-text:hover {
  background: rgba(255, 255, 255, 0.09);
  color: #fff;
}

.fav-btn-text.active {
  color: #ffc107;
  border-color: rgba(255, 193, 7, 0.55);
  background: rgba(255, 193, 7, 0.12);
}

.fav-btn-text.busy {
  opacity: 0.6;
  pointer-events: none;
}

.side-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-bottom: 12px;
}

.side-meta-chip {
  display: inline-block;
  padding: 3px 10px;
  font-size: 11px;
  color: #9aa4b2;
  background: rgba(255, 255, 255, 0.04);
  border: 1px solid rgba(255, 255, 255, 0.06);
  border-radius: 12px;
}

.side-blurb {
  margin: 0;
  font-size: 12.5px;
  line-height: 1.7;
  color: rgba(255, 255, 255, 0.6);
  max-height: 88px;
  overflow: hidden;
  display: -webkit-box;
  -webkit-line-clamp: 4;
  -webkit-box-orient: vertical;
}

/* ============ 右侧：选集区 ============== */
.side-section {
  flex: 1 1 auto;
  min-height: 0;
  display: flex;
  flex-direction: column;
  padding: 14px 22px 22px;
}

.side-section-title {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  margin-bottom: 12px;
}

.side-section-title-left {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  font-weight: 600;
  color: #fff;
  letter-spacing: 0.3px;
}

.side-section-title-left .bullet {
  width: 3px;
  height: 14px;
  background: linear-gradient(180deg, #40a9ff, #1890ff);
  border-radius: 2px;
}

.side-section-right {
  display: flex;
  align-items: center;
  gap: 8px;
}

.sort-toggle-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 4px;
  height: 26px;
  padding: 0 8px;
  border-radius: 6px;
  border: 1px solid rgba(255, 255, 255, 0.1);
  background: rgba(255, 255, 255, 0.04);
  color: rgba(255, 255, 255, 0.55);
  cursor: pointer;
  transition: all 0.15s ease;
  font-family: inherit;
  font-size: 11px;
}

.sort-toggle-btn .sort-label {
  font-size: 11px;
  white-space: nowrap;
}

.sort-toggle-btn:hover {
  background: rgba(255, 255, 255, 0.08);
  color: #fff;
  border-color: rgba(255, 255, 255, 0.2);
}

.side-count {
  font-size: 11.5px;
  color: rgba(255, 255, 255, 0.45);
}

.side-cache-chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 3px 8px;
  font-size: 10.5px;
  color: #7ee3b6;
  background: rgba(126, 227, 182, 0.1);
  border: 1px solid rgba(126, 227, 182, 0.25);
  border-radius: 10px;
  white-space: nowrap;
}

.side-cache-chip .side-cache-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: #7ee3b6;
  box-shadow: 0 0 6px #7ee3b6;
  animation: glow-pulse 1.4s ease-in-out infinite;
}

@keyframes glow-pulse {

  0%,
  100% {
    opacity: 0.6;
  }

  50% {
    opacity: 1;
  }
}

.side-empty {
  color: rgba(255, 255, 255, 0.45);
  font-size: 13px;
  padding: 20px 0;
}

/* ============ 集数网格：美化核心 ============ */
.ep-grid {
  flex: 1 1 auto;
  overflow-y: auto;
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 10px;
  padding-right: 4px;
  align-content: start;
  scrollbar-width: thin;
  scrollbar-color: rgba(255, 255, 255, 0.18) transparent;
}

.ep-grid::-webkit-scrollbar {
  width: 6px;
}

.ep-grid::-webkit-scrollbar-thumb {
  background: rgba(255, 255, 255, 0.14);
  border-radius: 3px;
}

.ep-grid::-webkit-scrollbar-thumb:hover {
  background: rgba(255, 255, 255, 0.28);
}

.ep-grid::-webkit-scrollbar-track {
  background: transparent;
}

/* 单个集数卡片：默认态（未观看 & 非当前集）——默认显示明显 */
.ep-item {
  position: relative;
  font-family: inherit;
  min-height: 48px;
  height: 48px;
  padding: 0 8px;
  border-radius: 10px;
  border: 1px solid rgba(64, 169, 255, 0.55);
  background: linear-gradient(180deg, rgba(24, 144, 255, 0.16) 0%, rgba(64, 169, 255, 0.08) 100%);
  color: #fff;
  font-size: 12.5px;
  font-weight: 500;
  cursor: pointer;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 2px;
  transition: transform 0.15s ease,
    background 0.15s ease,
    border-color 0.15s ease,
    box-shadow 0.15s ease,
    color 0.15s ease,
    opacity 0.15s ease;
}

.ep-item-num {
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* hover：收住（次要态） */
.ep-item:hover {
  background: linear-gradient(180deg, rgba(255, 255, 255, 0.035) 0%, rgba(255, 255, 255, 0.015) 100%);
  border-color: rgba(255, 255, 255, 0.12);
  color: #d9dde3;
  transform: none;
  box-shadow: none;
}

.ep-item:active {
  transform: translateY(0);
}

/* 未来集（在当前集之后，且未观看）：正常中性，不灰 */
.ep-item.future {
  opacity: 1;
}

/* 已观看：绿色高亮（明显态） */
.ep-item.watched {
  background: linear-gradient(180deg, rgba(76, 175, 80, 0.18) 0%, rgba(139, 195, 74, 0.10) 100%);
  border-color: rgba(76, 175, 80, 0.55);
  color: #e8f5e9;
  cursor: pointer;
  font-weight: 500;
}

.ep-item.watched:hover {
  background: linear-gradient(180deg, rgba(76, 175, 80, 0.08) 0%, rgba(76, 175, 80, 0.04) 100%);
  border-color: rgba(76, 175, 80, 0.25);
  color: rgba(255, 255, 255, 0.75);
  transform: none;
  box-shadow: none;
}

/* 已观看：底部进度条 + 右上角百分比 */
.ep-watched-progress {
  position: absolute;
  left: 0;
  bottom: 0;
  height: 4px;
  background: linear-gradient(90deg, #4caf50 0%, #8bc34a 100%);
  z-index: 1;
  pointer-events: none;
}

.ep-watched-pct {
  position: absolute;
  top: 3px;
  right: 4px;
  font-size: 9px;
  font-weight: 600;
  color: #4caf50;
  background: rgba(0, 0, 0, 0.45);
  padding: 1px 4px;
  border-radius: 3px;
  line-height: 1.3;
  z-index: 2;
  pointer-events: none;
}

/* 当前播放集：百分比移到左上角，避免与播放中徽章重叠 */
.ep-item.active .ep-watched-pct {
  top: 3px;
  right: auto;
  left: 4px;
  color: #69c0ff;
  background: rgba(0, 0, 0, 0.55);
}

/* 当前播放集：进度条颜色调整为适配蓝色主题 */
.ep-item.active .ep-watched-progress {
  background: linear-gradient(90deg, #40a9ff 0%, #69c0ff 100%);
}

/* 当前播放的集：发光边框 + 内部动画 + 彩色背景 */
.ep-item.active {
  background: linear-gradient(135deg, rgba(24, 144, 255, 0.22) 0%, rgba(64, 169, 255, 0.14) 60%, rgba(100, 181, 246, 0.1) 100%);
  border-color: rgba(64, 169, 255, 0.8);
  color: #fff;
  font-weight: 700;
  box-shadow:
    0 0 0 1px rgba(64, 169, 255, 0.4),
    0 0 14px rgba(24, 144, 255, 0.55),
    0 4px 14px rgba(24, 144, 255, 0.35);
  transform: translateY(-1px);
  animation: active-ep-pulse 1.6s ease-in-out infinite;
}

@keyframes active-ep-pulse {

  0%,
  100% {
    box-shadow:
      0 0 0 1px rgba(64, 169, 255, 0.35),
      0 0 10px rgba(24, 144, 255, 0.45),
      0 4px 12px rgba(24, 144, 255, 0.25);
  }

  50% {
    box-shadow:
      0 0 0 1px rgba(100, 181, 246, 0.6),
      0 0 18px rgba(24, 144, 255, 0.7),
      0 4px 18px rgba(24, 144, 255, 0.5);
  }
}

/* 当前集卡片右上角："播放中"三条竖线律动 */
.ep-playing-badge {
  position: absolute;
  top: 6px;
  right: 6px;
  display: inline-flex;
  align-items: flex-end;
  gap: 2px;
  height: 14px;
}

.ep-playing-badge .bar {
  display: inline-block;
  width: 2px;
  height: 6px;
  border-radius: 2px;
  background: #40a9ff;
  box-shadow: 0 0 4px #40a9ff;
  animation: bar-bounce 0.9s ease-in-out infinite;
}

.ep-playing-badge .b1 {
  animation-delay: -0.2s;
}

.ep-playing-badge .b2 {
  animation-delay: -0.5s;
}

.ep-playing-badge .b3 {
  animation-delay: -0.8s;
}

@keyframes bar-bounce {

  0%,
  100% {
    height: 4px;
  }

  50% {
    height: 12px;
    background: #69c0ff;
    box-shadow: 0 0 6px #69c0ff;
  }
}

/* 左下角：下一集预取提示 */
.prefetch-hint {
  position: absolute;
  left: 20px;
  bottom: 20px;
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 8px 14px;
  background: rgba(20, 22, 26, 0.75);
  backdrop-filter: blur(8px);
  border: 1px solid rgba(126, 227, 182, 0.35);
  border-radius: 10px;
  color: #c9f0dc;
  font-size: 12px;
  font-weight: 500;
  pointer-events: none;
  z-index: 10;
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.35);
}

.prefetch-icon {
  color: #7ee3b6;
  font-size: 14px;
  text-shadow: 0 0 6px #7ee3b6;
  animation: float-down 1.4s ease-in-out infinite;
}

@keyframes float-down {

  0%,
  100% {
    transform: translateY(0);
    opacity: 0.7;
  }

  50% {
    transform: translateY(2px);
    opacity: 1;
  }
}

/* loading / error */
.player-loading {
  position: absolute;
  inset: 0;
  z-index: 10;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 16px;
  color: #8892a0;
  font-size: 14px;
}

.player-loading .spinner {
  width: 44px;
  height: 44px;
  border: 3px solid rgba(255, 255, 255, 0.08);
  border-top-color: #1890ff;
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}

.player-error-page {
  position: absolute;
  inset: 0;
  z-index: 10;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 20px;
  color: #aaa;
}

.player-error-page .error-msg {
  font-size: 15px;
  color: #e8e8e8;
}

.player-error-page .back-btn {
  padding: 8px 20px;
  border-radius: 20px;
  background: rgba(24, 144, 255, 0.15);
  color: #40a9ff;
  border: 1px solid rgba(24, 144, 255, 0.35);
  font-size: 13px;
  cursor: pointer;
  font-family: inherit;
}

.player-error-page .back-btn:hover {
  background: rgba(24, 144, 255, 0.25);
}

/* ============ fade transition（vue built-in） ============ */
.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.35s ease, transform 0.35s ease;
}

.fade-enter-from,
.fade-leave-to {
  opacity: 0;
  transform: translateY(4px);
}

/* ============ 收藏夹弹窗 ============ */
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
  from {
    opacity: 0
  }

  to {
    opacity: 1
  }
}

@keyframes scaleIn {
  from {
    opacity: 0;
    transform: scale(0.96)
  }

  to {
    opacity: 1;
    transform: scale(1)
  }
}

.modal-box {
  width: 100%;
  max-width: 480px;
  background: #14181f;
  border: 1px solid rgba(255, 255, 255, 0.08);
  border-radius: 14px;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  box-shadow: 0 10px 40px rgba(0, 0, 0, 0.3);
  animation: scaleIn 0.2s cubic-bezier(0.4, 0, 0.2, 1);
}

.modal-box.small {
  max-width: 420px;
}

.modal-head {
  padding: 14px 18px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  border-bottom: 1px solid rgba(255, 255, 255, 0.06);
}

.modal-title {
  margin: 0;
  font-size: 15px;
  font-weight: 600;
  color: #fff;
  display: inline-flex;
  align-items: center;
  gap: 8px;
}

.modal-close {
  background: transparent;
  border: none;
  color: rgba(255, 255, 255, 0.5);
  cursor: pointer;
  padding: 4px;
  border-radius: 6px;
  line-height: 0;
  transition: all 0.15s ease;
}

.modal-close:hover {
  background: rgba(255, 255, 255, 0.06);
  color: #fff;
}

.modal-body {
  padding: 18px;
}

.modal-foot {
  padding: 12px 18px;
  border-top: 1px solid rgba(255, 255, 255, 0.06);
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}

.btn-ghost {
  padding: 8px 20px;
  border-radius: 8px;
  border: 1px solid rgba(255, 255, 255, 0.12);
  background: transparent;
  color: rgba(255, 255, 255, 0.65);
  cursor: pointer;
  font-size: 13px;
  font-family: inherit;
  transition: all 0.15s ease;
}

.btn-ghost:hover {
  background: rgba(255, 255, 255, 0.06);
  color: #fff;
  border-color: rgba(64, 169, 255, 0.45);
}

.btn-primary {
  padding: 8px 20px;
  border-radius: 8px;
  border: none;
  background: #1890ff;
  color: #fff;
  cursor: pointer;
  font-size: 13px;
  font-weight: 600;
  font-family: inherit;
  transition: all 0.15s ease;
}

.btn-primary:hover {
  background: #40a9ff;
  transform: translateY(-1px);
  box-shadow: 0 4px 12px rgba(24, 144, 255, 0.35);
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
