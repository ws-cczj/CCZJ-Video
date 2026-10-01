<script setup lang="ts">
defineOptions({ name: 'Player' })
import { ref, computed, onMounted, onBeforeUnmount, watch } from 'vue'
import { useI18n } from 'vue-i18n'

import { useRoute, useRouter } from 'vue-router'
import { GetRecentHistory, GetHistoryPosition, SaveWatchHistory, AddFavorite, RemoveFavorite, IsFavorite, normalizeApiError } from '../api/app'
import * as AppMod from '../api/app'
import { useSourceStore } from '../stores/source'
import { useVideoStore } from '../stores/video'
import { useErrorStore } from '../stores/error'
import VideoPlayer from '../components/VideoPlayer.vue'
import Icon from '../components/Icon.vue'
import PlayLinePicker from '../components/PlayLinePicker.vue'
import DoubanComments from '../components/DoubanComments.vue'
import { Button, Modal } from '../components/ui'
import { resolveEpisodeUrl, stripHtmlTags } from '../utils'
import { TsCache } from '../utils/tsCache'
import { epProgressKey, loadEpProgress, saveEpProgress, getEpProgressPct, flushEpProgress } from '../utils/episodeProgress'
import { bumpFavoritesRefresh } from '../stores/favoritesSync'
import { Window } from '../api/runtime'
import type { HistoryItem } from '../types'
import { readStorage, writeStorage } from '../platform/storage'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const sourceStore = useSourceStore()
const videoStore = useVideoStore()
const errorStore = useErrorStore()

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
  return t('detail.episode', { num })
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
// SearchSource 的第 3 个参数是页码、第 4 个才是条数，这里只要第 1 页 10 条。
const PROBE_PAGE_SIZE = 10

// 源站标题常带清晰度/压制尾巴（"雷神4：爱与雷霆_1080P_"），切到那种源之后再用它去问
// 别的源必然搜不到，于是探测会把明明有的源标成"未收录"。探测只认清洗过的名字。
const PROBE_JUNK_TAIL_RE = /(?:[\s_\-]+(?:\d{3,4}[pPiI]|hd|bd|uhd|web[\s_-]?rip|x26[45]|bluray)[\s_\-]*)+$/i

function probeNameOf(raw: string): string {
  const cleaned = raw.replace(PROBE_JUNK_TAIL_RE, '').trim()
  return cleaned || raw
}

async function searchSourceWithFallback(sk: string, vodName: string): Promise<any[]> {
  // 第一次：原名搜索
  try {
    const resp = await (AppMod as any).SearchSource(sk, vodName, 1, PROBE_PAGE_SIZE) as any
    const list = Array.isArray(resp?.videos) ? resp.videos : []
    if (list.length > 0) return list
  } catch { }

  // 第二次：去空格版搜索（解决“权力的游戏 第一季”vs“权力的游戏第一季”问题）
  const noSpaceName = vodName.replace(/\s+/g, '')
  if (noSpaceName !== vodName) {
    try {
      const resp2 = await (AppMod as any).SearchSource(sk, noSpaceName, 1, PROBE_PAGE_SIZE) as any
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
      const resp3 = await (AppMod as any).SearchSource(sk, baseName, 1, PROBE_PAGE_SIZE) as any
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
/**
 * 一个源对"这部片在哪儿"的回答。
 * - local：本库里就有这一条（当前源，或和本条同 global_id 的源）
 * - remote：本库没有，去源站搜到了
 * - missing / failed：源站回答"没有" / 源站根本没答上来（失败的那只允许再点一次重试）
 */
type SourceProbe = 'probing' | 'local' | 'remote' | 'missing' | 'failed'
interface SourceOption {
  source_key: string
  name: string
  vod_id?: string  // 该源中对应视频的 vod_id
  probe: SourceProbe
  hit?: any        // probe 为 remote 时的源站搜索原始条目，切换前要先入库
}
const sourceOptions = ref<SourceOption[]>([])
const activeSourceKey = ref('')  // 当前激活的源 key
const sourceSearchLoading = ref(false)
const showEpisodes = ref(true)   // 源列表 vs 选集列表切换
const readySourceCount = computed(() => sourceOptions.value.filter((s) => s.probe === 'local' || s.probe === 'remote').length)
const probingCount = computed(() => sourceOptions.value.filter((s) => s.probe === 'probing').length)

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
  // 刚起播的 0~5 秒不写库：否则每次重看都会把上次记住的位置冲掉。
  if (position <= 5) return
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

/**
 * 播放器恢复进度兜底：localStorage 按 origin 分区，独立 exe 与 dev 端口互不可见，
 * 本地没有记录时以数据库 watch_history 为准。
 */
async function resolveDbResumePosition(): Promise<number> {
  const idx = currentEpIndex.value
  const ep = episodes.value[idx]
  if (!sourceKey.value || !vodId.value || !ep) return 0
  try {
    return await GetHistoryPosition({
      source_key: sourceKey.value,
      vod_id: String(vodId.value),
      ep_num: ep.ep_num ?? (idx + 1),
    } as any) || 0
  } catch { return 0 }
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
    const raw = JSON.stringify(readStorage<Record<string, string[]>>('cczj_shortcuts', {}))
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
  try { (v as any).__progressAbortController?.abort() } catch { /* ignore */ }
  const progressAbort = new AbortController()
  ;(v as any).__progressAbortController = progressAbort
  ;(v as any).__epProgressBound = true
  console.log('[Player] ✔ 进度追踪已绑定到 video 元素')
  v.addEventListener('timeupdate', () => {
    _epUpdateCount++
    updateCurrentEpProgress(v.currentTime, v.duration)
    if (_epUpdateCount % 20 === 1) {
      const k = epKeyOf(currentEpIndex.value)
      console.log(`[Player] ✔ timeupdate #${_epUpdateCount}: key="${k}", time=${v.currentTime.toFixed(1)}s, dur=${v.duration?.toFixed(1) || '?'}`)
    }
  }, { signal: progressAbort.signal })
  v.addEventListener('loadedmetadata', () => {
    console.log(`[Player] ✔ loadedmetadata: dur=${v.duration?.toFixed(1) || '?'}`)
    updateCurrentEpProgress(v.currentTime, v.duration)
  }, { signal: progressAbort.signal })
}

/* ==================== 收藏状态 ==================== */
const isFav = ref(false)
const favBusy = ref(false)
const showFavFolderModal = ref(false)
const showCommentsModal = ref(false)

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
  if (isFav.value) {
    favBusy.value = true
    try {
      await RemoveFavorite({ source_key: sourceKey.value, vod_id: String(vodId.value), global_id: video.value?.global_id || 0 })
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
      const raw = JSON.stringify(readStorage<Record<string, string>>('cczj_fav_mapping', {}))
      const obj: Record<string, string> = raw ? JSON.parse(raw) : {}
      obj[key] = favTargetFolderId.value
      writeStorage('cczj_fav_mapping', obj)
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
  // 本地没有进度时不写库，否则会把数据库里记住的位置清零。
  if (position <= 0) return
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
  try {
    const v = document.querySelector('.native-video') as HTMLVideoElement | null
    v && (v as any).__progressAbortController?.abort()
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
  setTimeout(() => bindVideoTimeTracking(), 300)
  const canonicalID = video.value?.global_id || globalId.value || vodId.value
  router.replace(`/player/${sourceKey.value}/${canonicalID}/${targetIdx}?vod=${encodeURIComponent(String(vodId.value))}`).catch(() => { })
}

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
  if (!sourceKey.value || (!vodId.value && !globalId.value)) return
  loading.value = true
  try {
    const currentVodId = video.value?.vod_id
    if (String(currentVodId || '') !== String(vodId.value)) {
      await videoStore.loadDetail(sourceKey.value, vodId.value, false, globalId.value)
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

// ==================== 源探测：先问本库，再问源站 ====================
/**
 * 进播放页时对"这部片还能在哪儿看"做一次探测，分两步：
 *  1) 本库：用 global_id 查哪些源已经有同一部片（纯本地查询，不花钱）。
 *  2) 源站：本库没有副本的那些源，各发一次源站搜索。
 * 第 2 步以前只在 global_id 查不到时才跑，于是绝大多数情况下探测直接早退，选源列表里
 * 永远只剩当前那一个源——本库里的片子彼此不同 global_id，跨源同片本来就少。
 * 现在两步都跑，结果逐条回写到对应的源上。
 */
const PROBE_CONCURRENCY = 2
const REMOTE_PROBE_TTL = 10 * 60 * 1000
type ProbeOutcome = { probe: SourceProbe; vodId: string | null; hit?: any }
// 同一条片在同一个源上的结论短时间内不会变，来回切页面时不必再打一遍源站。
const remoteProbeCache = new Map<string, { outcome: ProbeOutcome; at: number }>()
let probeGeneration = 0

async function buildSourceOptions(): Promise<void> {
  if (!sourceStore.sources.length) return
  const generation = ++probeGeneration
  const vodName = probeNameOf(video.value?.vod_name || '')
  sourceOptions.value = sourceStore.sources.map((s: any) => {
    const isCurrent = s.source_key === sourceKey.value
    return {
      source_key: s.source_key || '',
      name: s.name || s.source_key || '',
      // 能进到这个页面就说明这一条在本库里，当前源直接算 local。
      vod_id: isCurrent ? String(vodId.value) : undefined,
      probe: isCurrent ? 'local' as SourceProbe : 'probing' as SourceProbe,
    }
  })
  activeSourceKey.value = sourceKey.value

  await probeLocalLibrary(generation)
  await probeRemoteSources(vodName, generation)
}

/** 本库探测：把已经有同一 global_id 副本的源标成 local。 */
async function probeLocalLibrary(generation: number): Promise<void> {
  let globalId = 0
  try {
    globalId = await (AppMod as any).GetGlobalIdForVideo(sourceKey.value, String(vodId.value)) as number
  } catch (e) {
    console.log('[Player] global_id 查找失败，改由源站探测补全:', e)
  }
  if (generation !== probeGeneration || globalId <= 0) return
  try {
    const refs = await (AppMod as any).FindSourcesByGlobalId(globalId) as any[]
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
  const cacheKey = `${sourceKeyOf}|${vodName}`
  const cached = remoteProbeCache.get(cacheKey)
  if (cached && Date.now() - cached.at < REMOTE_PROBE_TTL) return cached.outcome
  let outcome: ProbeOutcome = { probe: 'failed', vodId: null }
  try {
    const list = await searchSourceWithFallback(sourceKeyOf, vodName)
    const match = findBestMatch(list, vodName)
    outcome = match?.vod_id
      ? { probe: 'remote', vodId: String(match.vod_id), hit: match }
      : { probe: 'missing', vodId: null }
  } catch {
    outcome = { probe: 'failed', vodId: null }
  }
  remoteProbeCache.set(cacheKey, { outcome, at: Date.now() })
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

/**
 * 源站探测命中的那条只存在于源站，本库目录里没有它；而 GetVideoDetail 必须先解析目录身份，
 * 于是直接切过去只会落到「暂无播放资源」。所以切换前把这一条按「入库」写进目录——
 * 与源搜索的入库按钮走同一个接口，只写目录字段、不带任何播放地址。
 */
async function ensureCatalogEntry(opt: SourceOption): Promise<void> {
  if (opt.probe !== 'remote' || !opt.hit) return
  try {
    await (AppMod as any).ImportSourceVideos(opt.source_key, [opt.hit])
    opt.probe = 'local'
  } catch (e) {
    console.log('[Player] 源站命中条目入库失败:', e)
  }
}

async function switchToSource(sk: string): Promise<void> {
  if (!sk || sk === activeSourceKey.value) return
  sourceSearchLoading.value = true
  try {
    // 探测已经拿到 vod_id 的源，直接切（来自本库命中或源站搜索）
    const existing = sourceOptions.value.find((s) => s.source_key === sk)
    if (existing?.vod_id) {
      await ensureCatalogEntry(existing)
      await loadFromSource(sk, existing.vod_id)
      return
    }
    // 回退：远端搜索该源中的同名视频
    const vodName = probeNameOf(video.value?.vod_name || '')
    if (!vodName) return
    const list = await searchSourceWithFallback(sk, vodName)
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
    await loadFromSource(sk, String(match.vod_id))
  } catch (e) {
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

async function loadFromSource(sk: string, vid: string): Promise<void> {
  activeSourceKey.value = sk
  const prevEpNum = episodes.value[currentEpIndex.value]?.ep_num ?? undefined
  loading.value = true
  try {
    await videoStore.loadDetail(sk, vid)
    if (!video.value || !episodes.value.length) {
      loading.value = false
      errorStore.warn(t('player.sourceNoEpisodes'), t('player.sourceNoEpisodesDetail', { source: sk }), '', 'Player')
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
    if (targetIdx >= 0) recordHistory(targetIdx)
  } catch (e) {
    console.error('[Player] loadFromSource 失败:', e)
    errorStore.error(t('player.sourceLoadFailed'), normalizeApiError(e).message, '', 'Player')
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

// 同一视频内直接修改播放地址（例如浏览器前进/后退）时，页面实例会被保留，
// 因此需要将路由集数同步回播放器状态。通过播放器控制条切集时，当前集已先更新，
// 此处会自然跳过，避免重复加载。
watch(epIndexParam, (idx) => {
  if (!Number.isInteger(idx) || idx < 0 || idx >= episodes.value.length || idx === currentEpIndex.value) return
  currentEpIndex.value = idx
  _playToken.value++
  try { TsCache.setCurrentEpisode(idx) } catch { }
  setTimeout(() => bindVideoTimeTracking(), 300)
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
  // 探测要打源站，慢的时候几秒才回一条，不能挡住播放器起播：结果自己逐条回填。
  void buildSourceOptions()
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
  // 让在飞的源站探测结果作废：离开页面之后再回写没有意义，也会拖住退出。
  probeGeneration++
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
  return t('detail.episode', { num })
}
</script>

<template>
  <div class="player-page" @mouseenter="mouseInside = true" @mouseleave="mouseInside = false">
    <div v-if="loading" class="player-loading cczj-flex cczj-items-center cczj-justify-center cczj-gap-3">
      <div class="spinner"></div>
      <span>{{ t('common.loading') }}</span>
    </div>

    <template v-else-if="currentUrl">
      <div class="player-layout cczj-flex">
        <!-- ============= 左侧视频区 ============= -->
        <div class="player-col-main cczj-flex-1 cczj-relative" @mouseenter="mouseInside = true" @dblclick.stop>
          <VideoPlayer :url="currentUrl" :autoplay="true" :has-prev="hasPrev" :has-next="hasNext"
            :video-key="currentVideoKey" :show-title-bar="true" :title="currentEpName" :is-fav="isFav"
            :fav-busy="favBusy" :douban-id="video?.vod_douban_id || ''"
            :resolve-resume="resolveDbResumePosition"
            @toggle-favorite="toggleFavorite" @back="goBack" @prev="prevEpisode" @next="nextEpisode"
            @show-comments="showCommentsModal = true"
            :force-play-token="_playToken" />


          <!-- 侧面板折叠/展开按钮（视频区右侧中间） -->
          <button class="panel-toggle-btn cczj-absolute cczj-right-0 cczj-z-10 cczj-rounded-l cczj-p-2 cczj-cursor-pointer cczj-transition" :title="sidePanelCollapsed ? t('player.expandPanel') : t('player.collapsePanel')"
            :aria-label="sidePanelCollapsed ? t('player.expandPanel') : t('player.collapsePanel')"
            @click="sidePanelCollapsed = !sidePanelCollapsed">
            <Icon :name="sidePanelCollapsed ? 'chevron-left' : 'chevron-right'" :size="14" />
          </button>
        </div>

        <!-- ============= 右侧选集面板 ============= -->
        <aside class="player-col-side cczj-flex cczj-flex-col cczj-overflow-hidden" :class="{ collapsed: sidePanelCollapsed }">
          <!-- 顶部卡：标题 + 年份/地区/分类 + 简介 + 收藏按钮 -->
          <div class="side-header cczj-flex cczj-flex-col cczj-gap-3 cczj-p-4 cczj-mt-0 cczj-mb-0">
            <div class="side-top-bar cczj-flex cczj-items-center cczj-justify-between cczj-gap-3">
              <h1 class="side-title cczj-text-lg cczj-font-semibold cczj-truncate cczj-flex-1">{{ video?.vod_name || t('player.videoPlayback') }}</h1>
              <div class="side-top-actions cczj-flex cczj-items-center cczj-gap-2 cczj-flex-shrink-0">
                <button class="close-btn-panel cczj-p-2 cczj-rounded cczj-transition cczj-cursor-pointer" :title="t('common.minimize')" :aria-label="t('common.minimize')" @click="onMinimizeApp">
                  <Icon name="minimize" :size="14" />
                </button>
                <Button variant="text" size="sm" class="close-btn-panel cczj-p-2 cczj-rounded cczj-transition cczj-cursor-pointer" :title="t('common.close')" @click="flushEpProgress(); router.back()">
                  <Icon name="x" :size="18" />
                </Button>
              </div>
            </div>
            <div class="side-meta cczj-flex cczj-flex-wrap cczj-gap-2">
              <span v-if="video?.vod_year" class="side-meta-chip cczj-px-2 cczj-py-1 cczj-text-xs cczj-rounded cczj-bg-secondary">{{ video.vod_year }}</span>
              <span v-if="video?.vod_area" class="side-meta-chip cczj-px-2 cczj-py-1 cczj-text-xs cczj-rounded cczj-bg-secondary">{{ video.vod_area }}</span>
              <span v-if="video?.type_name" class="side-meta-chip cczj-px-2 cczj-py-1 cczj-text-xs cczj-rounded cczj-bg-secondary">{{ video.type_name }}</span>
            </div>
            <p v-if="overviewText" class="side-blurb cczj-text-sm cczj-text-muted cczj-line-clamp-3 cczj-mt-0 cczj-mb-0">{{ overviewText }}</p>
          </div>

          <!-- 选源区 + 选集区 -->
          <section class="side-section cczj-flex-1 cczj-overflow-y-auto cczj-p-4 cczj-flex cczj-flex-col">
            <!-- 源列表 -->
            <div class="side-section-title cczj-flex cczj-items-center cczj-justify-between cczj-gap-2 cczj-mb-3">
              <div class="side-section-title-left cczj-flex cczj-items-center cczj-gap-2">
                <span class="bullet cczj-w-2 cczj-h-2 cczj-rounded-full cczj-bg-accent"></span>
                <span class="cczj-text-sm cczj-font-medium">{{ t('player.playSources') }}</span>
              </div>
              <div class="side-section-right cczj-flex cczj-items-center cczj-gap-2">
                <span class="side-count cczj-text-xs cczj-text-muted">{{ t('player.availableSources', { count: readySourceCount }) }}</span>
                <span v-if="probingCount > 0" class="side-count cczj-text-xs cczj-text-muted">{{ t('player.probePendingCount', { count: probingCount }) }}</span>
              </div>
            </div>

            <div class="source-list cczj-flex cczj-gap-2">
              <button v-for="src in sourceOptions" :key="src.source_key" class="source-item cczj-flex cczj-items-center cczj-gap-2 cczj-px-3 cczj-py-2 cczj-rounded cczj-transition cczj-cursor-pointer"
                :class="[`probe-${src.probe}`, { active: src.source_key === activeSourceKey }]"
                @click="switchToSource(src.source_key)"
                :disabled="!sourceSelectable(src) || sourceSearchLoading"
                :title="probeLabel(src.probe)">
                <span class="source-name cczj-truncate">{{ src.name }}</span>
                <span class="source-probe cczj-text-xs">{{ probeLabel(src.probe) }}</span>
                <span v-if="src.source_key === activeSourceKey" class="source-active-dot cczj-w-2 cczj-h-2 cczj-rounded-full cczj-bg-accent"></span>
              </button>
            </div>

            <PlayLinePicker v-if="videoStore.lines.length > 1" class="cczj-mt-4" :lines="videoStore.lines"
              :model-value="videoStore.activeLineIndex" :source-key="sourceKey" :vod-id="String(vodId)"
              :global-id="Number(video?.global_id || globalId || 0)"
              :ep-num="Number(episodes[currentEpIndex]?.ep_num || 0)"
              @update:model-value="onLineChange(Number($event))" />

            <!-- 选集区（仅当有剧集时显示） -->
            <template v-if="episodes.length > 0">
              <div class="side-section-title cczj-flex cczj-items-center cczj-justify-between cczj-gap-2 cczj-mt-4 cczj-mb-3">
                <div class="side-section-title-left cczj-flex cczj-items-center cczj-gap-2">
                  <span class="bullet cczj-w-2 cczj-h-2 cczj-rounded-full cczj-bg-accent"></span>
                  <span class="cczj-text-sm cczj-font-medium">{{ t('detail.episodes') }}</span>
                  <span class="side-count cczj-text-xs cczj-text-muted">{{ t('detail.totalEpisodes', { count: episodes.length }) }}</span>
                </div>
                <div class="side-section-right">
                  <button class="sort-toggle-btn cczj-flex cczj-items-center cczj-gap-1 cczj-px-2 cczj-py-1 cczj-rounded cczj-transition cczj-cursor-pointer" :title="episodeSortAsc ? t('detail.sortAscTip') : t('detail.sortDescTip')"
                    @click="toggleEpisodeSort">
                    <Icon :name="episodeSortAsc ? 'chevron-down' : 'chevron-up'" :size="12" />
                    <span class="sort-label cczj-text-xs">{{ episodeSortAsc ? t('detail.ascOrder') : t('detail.descOrder') }}</span>
                  </button>
                </div>
              </div>

              <div class="ep-grid cczj-grid cczj-gap-2">
                <button v-for="(ep, i) in sortedEpisodes" :key="String(i)" class="ep-item cczj-relative cczj-px-3 cczj-py-2 cczj-rounded cczj-transition cczj-cursor-pointer cczj-text-sm" :class="{
                  active: origIdx(i) === currentEpIndex,
                  watched: isWatchedEp(origIdx(i)),
                  future: origIdx(i) > currentEpIndex && !isWatchedEp(origIdx(i)),
                }" @click="goToEpisode(origIdx(i))"
                  :title="epLabel(origIdx(i), ep) + (getEpWatchPct(origIdx(i)) > 0 ? t('detail.watchedProgress', { pct: Math.round(getEpWatchPct(origIdx(i))) }) : '')">
                  <span class="ep-item-num cczj-truncate">{{ epLabel(origIdx(i), ep) }}</span>
                  <span v-show="origIdx(i) === currentEpIndex" class="ep-playing-badge cczj-absolute cczj-top-1 cczj-right-1 cczj-flex">
                    <span class="bar b1 cczj-bg-accent cczj-rounded"></span>
                    <span class="bar b2 cczj-bg-accent cczj-rounded"></span>
                    <span class="bar b3 cczj-bg-accent cczj-rounded"></span>
                  </span>
                  <span v-show="isWatchedEp(origIdx(i)) && getEpWatchPct(origIdx(i)) > 0" class="ep-watched-progress cczj-absolute cczj-bottom-0 cczj-left-0 cczj-bg-accent cczj-rounded"
                    :style="{ width: getEpWatchPct(origIdx(i)) + '%' }"></span>
                  <span v-show="isWatchedEp(origIdx(i)) && getEpWatchPct(origIdx(i)) > 0" class="ep-watched-pct cczj-absolute cczj-right-1 cczj-text-xs cczj-text-muted">{{
                    Math.round(getEpWatchPct(origIdx(i))) }}%</span>
                </button>
              </div>
            </template>
            <div v-else-if="!sourceSearchLoading" class="side-empty cczj-text-center cczj-py-8 cczj-text-muted cczj-text-sm">{{ t('player.noPlayableEpisodes') }}</div>
          </section>
        </aside>
      </div>
    </template>

    <div v-else class="player-error-page cczj-flex cczj-flex-col cczj-items-center cczj-justify-center cczj-gap-4">
      <div class="error-msg cczj-text-lg cczj-text-muted">{{ t('player.noPlayableMedia') }}</div>
      <Button variant="text" size="md" @click="goBack"><span>{{ t('common.back') }}</span></Button>
    </div>

    <!-- 豆瓣评论弹窗 -->
    <Modal :model-value="showCommentsModal" title="" width="680px" :show-footer="false"
      @update:model-value="(v: boolean) => !v && (showCommentsModal = false)">
      <DoubanComments v-if="video?.vod_douban_id && showCommentsModal" :douban-id="String(video.vod_douban_id)" />
    </Modal>

    <!-- 收藏夹选择弹窗 -->
    <Modal :model-value="showFavFolderModal" :title="t('detail.favToFolder')" width="420px" :show-footer="true"
      @update:model-value="(v: boolean) => !v && (showFavFolderModal = false)">
      <div class="folder-select-list cczj-flex cczj-flex-col cczj-gap-2">
        <label v-for="folder in favFolders" :key="folder.id" class="folder-select-item cczj-flex cczj-items-center cczj-gap-2 cczj-p-3 cczj-rounded cczj-transition cczj-cursor-pointer"
          :class="{ active: favTargetFolderId === folder.id }">
          <input type="radio" v-model="favTargetFolderId" :value="folder.id" class="cczj-hidden" />
          <span class="folder-radio cczj-rounded-full cczj-border cczj-flex cczj-items-center cczj-justify-center" />
          <Icon :name="folder.default ? 'star' : 'list'" :size="14" />
          <span class="folder-name cczj-flex-1 cczj-truncate">{{ folder.name }}</span>
        </label>
      </div>
      <template #footer>
        <Button variant="secondary" size="md" @click="showFavFolderModal = false">{{ t('common.cancel') }}</Button>
        <Button variant="primary" size="md" @click="confirmAddToFolder">{{ t('detail.confirmFav') }}</Button>
      </template>
    </Modal>
  </div>
</template>

<style scoped src="../styles/views/player.css"></style>
