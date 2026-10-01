<script setup lang="ts">
import { ref, nextTick, onMounted, onBeforeUnmount, watch, computed } from 'vue'
import Icon from './Icon.vue'
import { MotionTransition, Select as SelectDropdown } from './ui'
import { TsCache } from '../utils/tsCache'
import { usePerfStore } from '../stores/perf'
import { readStorage, readStorageBoolean, removeStorage, writeStorage } from '../platform/storage'
import { createPlayerSettings } from '../player/settings'
import { usePlayerShortcuts } from '../player/usePlayerShortcuts'
import { usePlayerOsd } from '../player/usePlayerOsd'
import { useSubtitles } from '../player/useSubtitles'
import { useThumbnailScrubber } from '../player/useThumbnailScrubber'
import { useVideoQuality } from '../player/useVideoQuality'
import { clearPlaybackTime, readPlaybackTime, savePlaybackTime } from '../player/usePlaybackProgress'
import { useHlsEngine } from '../player/hls/useHlsEngine'
import { proxyHlsURL } from '../player/hls/proxy'
import loadingGif from '../assets/videos/loading.gif'
import pauseImg from '../assets/images/pause.png'
import {
  WindowIsFs, WindowIsMax, WindowSetFullscreen, WindowToggleMax
} from '../api/app'
import { useI18n } from 'vue-i18n'
import { tr } from '../locales'

const props = withDefaults(defineProps<{
  url: string
  autoplay?: boolean
  hasPrev?: boolean
  hasNext?: boolean
  videoKey?: string
  showTitleBar?: boolean
  title?: string
  isFav?: boolean
  favBusy?: boolean
  doubanId?: string
  /** 取当前集在后端 watch_history 里的位置（秒），本地缓存缺失时兜底 */
  resolveResume?: () => Promise<number>
}>(), {
  autoplay: true,
  hasPrev: false,
  hasNext: false,
  videoKey: '',
  showTitleBar: true,
  title: '',
  isFav: false,
  favBusy: false,
  doubanId: '',
  resolveResume: undefined,
})

const emit = defineEmits(['back', 'prev', 'next', 'toggleFavorite', 'toggleAutoplay', 'showComments'])
const hlsEngine = useHlsEngine()
const { t } = useI18n()
const perf = usePerfStore()

// ------ 播放质量取样 ------
// 解码器的帧计数是「这个元素从建起来到现在」的累计值，换集时元素会重建，所以每次装载
// 都重新对齐一次基准，否则上一集的帧数会被算进这一集。
let _perfFramesRaw = 0
let _perfDroppedRaw = 0
let _perfClockRaw = 0
let _stallSince = 0

function hostOf(u: string): string {
  try { return new URL(u).hostname } catch { return '' }
}

function resetPerfBaseline(video: HTMLVideoElement): void {
  const q = video.getVideoPlaybackQuality?.()
  _perfFramesRaw = q ? q.totalVideoFrames : 0
  _perfDroppedRaw = q ? q.droppedVideoFrames : 0
  _perfClockRaw = video.currentTime
  _stallSince = 0
}

// 每秒一次（跟着缓存监控定时器走）：累计解码帧与丢帧、真实观看时长和当前规格。
// 观看时长按 currentTime 增量算，超过 2 秒的增量一定是 seek 或卡顿，不算「看了」，
// 于是拖进度条不会虚增时长，卡顿也不会被记成观看。
function samplePerf(video: HTMLVideoElement): void {
  const q = video.getVideoPlaybackQuality?.()
  if (q) {
    perf.addFrames(q.totalVideoFrames - _perfFramesRaw, q.droppedVideoFrames - _perfDroppedRaw)
    _perfFramesRaw = q.totalVideoFrames
    _perfDroppedRaw = q.droppedVideoFrames
  }
  if (!video.paused && !video.ended) {
    const delta = video.currentTime - _perfClockRaw
    if (delta > 0 && delta <= 2) perf.addWatched(delta)
  }
  _perfClockRaw = video.currentTime

  let bitrate = 0
  try {
    const hls = (video as any).__hls
    const idx = hls && hls.currentLevel >= 0 ? hls.currentLevel : hls?.nextAutoLevel
    const lvl = hls?.levels?.[idx] || hls?.levels?.[hls.levels.length - 1]
    bitrate = lvl?.bitrate || 0
  } catch { bitrate = 0 }
  if (!bitrate) bitrate = extractBitrateFromUrl(props.url) || extractBitrateFromCachedTs()
  const w = video.videoWidth || 0
  const h = video.videoHeight || 0
  perf.setMedia(w && h ? `${w}x${h}` : '', bitrate)
}

// 卡顿结算：waiting 到恢复出声之间就是用户看到的「转圈」。seek 也算，
// 因为对播放器来说那同样是一段没有画面的等待。
function endStall(): void {
  if (!_stallSince) return
  perf.addStall(performance.now() - _stallSince)
  _stallSince = 0
}

const wrapperRef = ref<HTMLDivElement>()
const errorMsg = ref('')
let networkErrTimer: ReturnType<typeof setTimeout> | null = null
const showNetworkError = ref(false)
function clearNetworkErrTimer(): void {
  if (networkErrTimer) { clearTimeout(networkErrTimer); networkErrTimer = null }
  showNetworkError.value = false
}
function retryPlayback(): void {
  const video = getVideoEl()
  if (!video || !props.url) return
  errorMsg.value = ''
  showNetworkError.value = false
  loadHls(video, props.url)
}

const playing = ref(false)
const current = ref(0)
const duration = ref(0)
const volume = ref(1)
const muted = ref(false)
const speed = ref(1)
const isFullscreen = ref(false)
const showControls = ref(true)
const mouseInside = ref(false)  // ⭐ 鼠标是否在播放器区域内
let hideTimer: number | null = null
const loading = ref(true)
const videoReady = ref(false)

// ========= 预缓冲：等待加载完5个TS分片或超过10s才准备播放 =========
const preBuffering = ref(false)       // 是否处于初始预缓冲阶段
const loadingCached = ref(0)          // 当前已缓存片段数
const loadingTotal = ref(0)           // 总片段数
const loadingSpeed = ref('')          // 下载速度文本
const loadingElapsed = ref(0)         // 已等待秒数
let prebufferTimeout: number | null = null
let prebufferCheckTimer: number | null = null
let loadingStatsTimer: number | null = null
let loadingStatsStartTime = 0
let loadingStatsLastBytes = 0

/** 启动加载统计轮询（用于显示缓冲进度和速度） */
function startLoadingStats(): void {
  loadingStatsStartTime = Date.now()
  loadingStatsLastBytes = TsCache.stats().bytes
  loadingElapsed.value = 0
  loadingSpeed.value = ''

  if (loadingStatsTimer) clearInterval(loadingStatsTimer)
  loadingStatsTimer = window.setInterval(() => {
    const s = TsCache.stats()
    loadingCached.value = s.entries
    loadingTotal.value = s.totalSegments
    loadingElapsed.value = Math.round((Date.now() - loadingStatsStartTime) / 1000)

    // 计算下载速度
    const bytesDelta = s.bytes - loadingStatsLastBytes
    loadingStatsLastBytes = s.bytes
    const intervalSec = 0.3
    if (bytesDelta > 0) {
      const speedBps = bytesDelta / intervalSec
      if (speedBps > 1024 * 1024) {
        loadingSpeed.value = (speedBps / 1024 / 1024).toFixed(1) + ' MB/s'
      } else {
        loadingSpeed.value = Math.round(speedBps / 1024) + ' KB/s'
      }
    } else if (loadingSpeed.value === '') {
      loadingSpeed.value = t('player.connecting')
    }
  }, 300)
}

function stopLoadingStats(): void {
  if (loadingStatsTimer) {
    clearInterval(loadingStatsTimer)
    loadingStatsTimer = null
  }
  loadingSpeed.value = ''
}

/** 启动预缓冲检查：等待5个TS分片或10秒超时 */
function startPrebuffer(): void {
  if (preBuffering.value) return
  preBuffering.value = true
  startLoadingStats()

  // 每300ms检查一次条件
  prebufferCheckTimer = window.setInterval(() => {
    const s = TsCache.stats()
    if (s.entries >= 5) {
      console.log(`[Player] ✅ 预缓冲完成: ${s.entries} 片段已缓存 (${loadingElapsed.value}s)`)
      stopPrebuffer()
    }
  }, 300)

  // 10秒超时
  prebufferTimeout = window.setTimeout(() => {
    const s = TsCache.stats()
    console.log(`[Player] ⏰ 预缓冲超时: ${s.entries} 片段已缓存 (10s)，开始播放`)
    stopPrebuffer()
  }, 10000)

  console.log('[Player] 🔄 开始预缓冲 (等待5片段或10s超时)...')
}

function stopPrebuffer(): void {
  if (!preBuffering.value) return
  preBuffering.value = false
  if (prebufferCheckTimer) { clearInterval(prebufferCheckTimer); prebufferCheckTimer = null }
  if (prebufferTimeout) { clearTimeout(prebufferTimeout); prebufferTimeout = null }
  stopLoadingStats()

  // 开始播放
  const v = getVideoEl()
  if (v && props.autoplay !== false) {
    loading.value = false
    if (!v.hasAttribute('data-autoplay-done')) {
      v.setAttribute('data-autoplay-done', '1')
      console.log('[Player] ▶ 预缓冲完成，开始播放')
      safePlay(true)
    }
  }
}

// 弹出面板状态（音量和倍速）
const showVolumePanel = ref(false)
const showSpeedPanel = ref(false)
const showPlaybackSettings = ref(false)
const showVideoInfo = ref(false)
const showShortcutModal = ref(false)

// B站风格时间显示 - 可点击跳转
const showTimeInput = ref(false)

// m3u8 流信息（码率/编码/分辨率）
interface StreamVariantInfo { bandwidth: number; resolution: string; codecs: string; url: string }
const m3u8StreamInfo = ref<StreamVariantInfo[]>([])
const timeInputValue = ref('')
const timeInputRef = ref<HTMLInputElement | null>(null)

// 记录打开时间输入框时的原始值，用于判断用户是否实际修改了时间
const timeInputOriginal = ref('')

function toggleTimeInput(): void {
  showTimeInput.value = !showTimeInput.value
  if (showTimeInput.value) {
    timeInputValue.value = fmt(current.value)
    timeInputOriginal.value = timeInputValue.value
    nextTick(() => { timeInputRef.value?.focus(); timeInputRef.value?.select() })
  }
}
function parseTimeInput(val: string): number {
  val = val.trim()
  // 支持格式: mm:ss, hh:mm:ss, 纯秒数
  const parts = val.split(':')
  if (parts.length === 3) {
    const h = parseInt(parts[0], 10) || 0
    const m = parseInt(parts[1], 10) || 0
    const s = parseInt(parts[2], 10) || 0
    return h * 3600 + m * 60 + s
  } else if (parts.length === 2) {
    const m = parseInt(parts[0], 10) || 0
    const s = parseInt(parts[1], 10) || 0
    return m * 60 + s
  } else {
    return parseInt(val, 10) || 0
  }
}
function jumpToTime(): void {
  // ⭐ 修复：只有用户实际修改了时间才执行跳转
  const trimmed = timeInputValue.value.trim()
  if (trimmed === timeInputOriginal.value.trim()) {
    showTimeInput.value = false
    return
  }
  const sec = parseTimeInput(timeInputValue.value)
  const v = getVideoEl()
  if (v && sec >= 0 && sec <= duration.value) {
    v.currentTime = sec
  }
  showTimeInput.value = false
}
function cancelTimeInput(): void {
  showTimeInput.value = false
}
const showReportAd = ref(false)
const reportAdDomains = ref<string[]>([])
const reportAdToast = ref('')
let _reportAdToastTimer: number | null = null
const autoNextEnabled = ref(true)
const speedOptions = [0.5, 0.75, 1, 1.25, 1.5, 2]

// ========= 快捷键设置 =========
interface ShortcutAction { id: string; labelKey: string; descriptionKey: string; defaultKeys: string[] }
const SHORTCUT_ACTIONS: ShortcutAction[] = [
  { id: 'togglePlay', labelKey: 'player.scTogglePlay', descriptionKey: 'player.scTogglePlayDesc', defaultKeys: ['Space', 'K'] },
  { id: 'seekBack', labelKey: 'player.scSeekBack', descriptionKey: 'player.scSeekBackDesc', defaultKeys: ['ArrowLeft'] },
  { id: 'seekForward', labelKey: 'player.scSeekForward', descriptionKey: 'player.scSeekForwardDesc', defaultKeys: ['ArrowRight'] },
  { id: 'seekBackBig', labelKey: 'player.scSeekBackBig', descriptionKey: 'player.scSeekBackBigDesc', defaultKeys: ['J'] },
  { id: 'seekForwardBig', labelKey: 'player.scSeekForwardBig', descriptionKey: 'player.scSeekForwardBigDesc', defaultKeys: ['L'] },
  { id: 'volumeUp', labelKey: 'player.scVolumeUp', descriptionKey: 'player.scVolumeUpDesc', defaultKeys: ['ArrowUp'] },
  { id: 'volumeDown', labelKey: 'player.scVolumeDown', descriptionKey: 'player.scVolumeDownDesc', defaultKeys: ['ArrowDown'] },
  { id: 'mute', labelKey: 'player.mute', descriptionKey: 'player.scMuteDesc', defaultKeys: ['M'] },
  { id: 'speedUp', labelKey: 'player.scSpeedUp', descriptionKey: 'player.scSpeedUpDesc', defaultKeys: [']'] },
  { id: 'speedDown', labelKey: 'player.scSpeedDown', descriptionKey: 'player.scSpeedDownDesc', defaultKeys: ['['] },
  { id: 'fullscreen', labelKey: 'player.fullscreen', descriptionKey: 'player.scFullscreenDesc', defaultKeys: ['F'] },
  { id: 'prevEp', labelKey: 'player.prev', descriptionKey: 'player.scPrevEpDesc', defaultKeys: ['P'] },
  { id: 'nextEp', labelKey: 'player.next', descriptionKey: 'player.scNextEpDesc', defaultKeys: ['N'] },
  { id: 'pip', labelKey: 'player.pip', descriptionKey: 'player.scPipDesc', defaultKeys: ['I'] },
]
const {
  shortcutMap,
  editingShortcutId,
  load: loadShortcuts,
  startEditShortcut,
  cancelEditShortcut,
  onShortcutKeyDown,
  resetShortcut,
  resetAllShortcuts,
  fmtKey,
} = usePlayerShortcuts(SHORTCUT_ACTIONS)

// 初始化快捷键
loadShortcuts()

function toggleAutoNext(): void {
  autoNextEnabled.value = !autoNextEnabled.value
  try { writeStorage('cczj_auto_next', autoNextEnabled.value) } catch { /* ignore */ }
}

// 初始化自动连播设置
try {
  const saved = readStorageBoolean('cczj_auto_next', false)
  if (!saved) autoNextEnabled.value = false
} catch { /* ignore */ }

// ========= 报告广告 =========
function toggleReportAd(): void {
  showReportAd.value = !showReportAd.value
  if (showReportAd.value) {
    // 从当前 m3u8 缓存中提取片段域名
    const domains = new Set<string>()
    try {
      const cached = TsCache.getM3u8FromCache(props.url)
      if (cached) {
        const base = props.url.substring(0, props.url.lastIndexOf('/') + 1)
        for (const line of cached.split('\n')) {
          const t = line.trim()
          if (!t || t.startsWith('#')) continue
          try {
            const abs = new URL(t, base).href
            const h = new URL(abs).hostname
            if (h) domains.add(h)
          } catch { }
        }
      }
    } catch { }
    // 如果 m3u8 缓存无片段，回退到 props.url 自身域名
    if (domains.size === 0 && props.url) {
      try {
        const h = new URL(props.url).hostname
        if (h) domains.add(h)
      } catch { }
    }
    const blacklist = TsCache.getAdDomains()
    reportAdDomains.value = [...domains].filter(d => !blacklist.some(b => d.includes(b) || b.includes(d)))
  }
}
function doReportAd(domain: string): void {
  const ok = TsCache.addAdDomain(domain)
  showReportAd.value = false
  if (ok) {
    reportAdToast.value = t('player.adBlacklisted', { domain })
  } else {
    reportAdToast.value = t('player.adAlreadyBlacklisted')
  }
  if (_reportAdToastTimer != null) clearTimeout(_reportAdToastTimer)
  _reportAdToastTimer = window.setTimeout(() => { reportAdToast.value = '' }, 3000)
}
// ========= 外挂字幕 =========
const subtitleFileRef = ref<HTMLInputElement | null>(null)
const {
  showSubtitlePanel,
  subtitleCues,
  subtitleName,
  subtitleVisible,
  subtitleError,
  activeSubtitle,
  openSubtitlePicker,
  onSubtitleFileChosen,
  clearSubtitle,
} = useSubtitles({ current, fileInput: subtitleFileRef })

// ⭐ 播放器设置统一存储在单个 JSON 对象中（key: 'vp_settings'），避免 localStorage 碎片化。
// 旧版散落的 vp_* 键会在首次读取时自动迁移并清理。
const { read: readSetting, write: writeSetting } = createPlayerSettings()

// ========= 画质模式与增强管线 =========
// 模式：原高清 / 动画增强 M·L / 影视增强 / 扩展包着色器档位（useVideoQuality）。
const {
  qualityOpen,
  qualityMode,
  qualityOptions,
  qualityDropdownValue,
  onQualityChange,
  showAiWarning,
  confirmAiMode,
  cancelAiMode,
  qualityToastText,
  compareEnabled,
  compareSplit,
  toggleEnhancementCompare,
  updateEnhancementCompare,
  qualityLabel,
  onMediaReady,
  notifySeeked,
  resetPipeline,
  hasPipeline,
} = useVideoQuality({
  getVideoEl,
  wrapperRef,
  settings: { read: readSetting, write: writeSetting },
  keepVisible,
})

// ========= 播放进度记录 =========
// 设置：autoResume = true 时直接跳到上次位置；false 时弹出 5 秒提示
const SAVE_INTERVAL_MS = 3000
const PROMPT_SEC = 5           // 提示存在时间
let _saveTimer: number | null = null

const savedTime = ref<number | null>(null)
const showResumePrompt = ref(false)
const resumeRemainSec = ref(PROMPT_SEC)
let _resumeTimer: number | null = null
let _resumeAutoJump = false  // 从 localStorage 读配置：是否自动跳
// 媒体还没 attach 时设 currentTime 会被静默丢掉，先记下来等能 seek 了再落地
let _pendingSeekSec: number | null = null

// 计算进度百分比（给 CSS 用）
const progressPct = computed(() => {
  const d = duration.value
  if (!d || d <= 0) return 0
  const pct = (current.value / d) * 100
  return Math.max(0, Math.min(100, pct))
})

// 缓冲进度百分比（通过 video.buffered 计算）
const bufferPct = ref(0)
function updateBuffer(): void {
  const v = getVideoEl()
  if (!v || !v.duration || v.duration <= 0) {
    TsCache.setPlaybackBufferAhead(0)
    return
  }
  const buf = v.buffered
  if (!buf || buf.length === 0) {
    bufferPct.value = 0
    TsCache.setPlaybackBufferAhead(0)
    return
  }
  // 用最后一段 buffer 的结尾来表示"已缓冲到的最远位置"
  const end = buf.end(buf.length - 1)
  bufferPct.value = Math.max(0, Math.min(100, (end / v.duration) * 100))
  TsCache.setPlaybackBufferAhead(Math.max(0, end - v.currentTime))
}

// 自动跳到上次播放位置的开关：由用户在"跳回并记住"时开启
function loadAutoJumpConfig(): boolean {
  return readSetting('auto_resume_jump', '0') === '1'
}
function saveAutoJumpConfig(on: boolean): void {
  writeSetting('auto_resume_jump', on ? '1' : '0')
}

// 缓存监控
const cacheStats = ref<{
  hits: number; misses: number; entries: number; bytes: number; hitRate: number;
  totalSegments: number; prefetched: number; prefetchTarget: number; queued: number; inflight: number;
}>({
  hits: 0, misses: 0, entries: 0, bytes: 0, hitRate: 0, totalSegments: 0, prefetched: 0,
  prefetchTarget: 0, queued: 0, inflight: 0,
})
let cacheStatsTimer: number | null = null
function formatBytes(n: number): string {
  if (n < 1024) return n + ' B'
  if (n < 1024 * 1024) return (n / 1024).toFixed(1) + ' KB'
  return (n / 1024 / 1024).toFixed(2) + ' MB'
}

// 从 TS URL 推断码率（优先匹配 "数字+k" 模式，如 2100k, 3000K）
function extractBitrateFromUrl(url: string): number {
  try {
    const path = new URL(url).pathname
    // 优先: /2100k/ 或 _3000K. 等明确的码率标记
    const mk = path.match(/[\/_.-](\d{3,5})[kK][\/_.-]/)
    if (mk) {
      const val = parseInt(mk[1])
      if (val >= 200 && val <= 50000) return val * 1000
    }
    // 其次: 2100k 作为路径段
    const segments = path.split('/').filter(Boolean)
    for (const seg of segments) {
      const m = seg.match(/^(\d{3,5})[kK]$/)
      if (m) {
        const val = parseInt(m[1])
        if (val >= 200 && val <= 50000) return val * 1000
      }
    }
  } catch { }
  return 0
}

// 从 m3u8 缓存的 TS 片段 URL 中提取码率
function extractBitrateFromCachedTs(): number {
  try {
    const cached = TsCache.getM3u8FromCache(props.url)
    if (cached) {
      const base = props.url.substring(0, props.url.lastIndexOf('/') + 1)
      const lines = cached.split('\n')
      for (const line of lines) {
        const t = line.trim()
        if (!t || t.startsWith('#')) continue
        try {
          const abs = new URL(t, base).href
          const br = extractBitrateFromUrl(abs)
          if (br > 0) return br
        } catch { }
      }
    }
  } catch { }
  return 0
}

// ========= 视频信息弹出面板 =========
const videoInfo = computed(() => {
  const v = getVideoEl()
  if (!v) return null
  const dur = duration.value
  const cur = current.value
  const pct = dur > 0 ? ((cur / dur) * 100).toFixed(1) : '0'
  // 分辨率
  const videoWidth = v.videoWidth || 0
  const videoHeight = v.videoHeight || 0
  const resolution = videoWidth && videoHeight ? `${videoWidth} x ${videoHeight}` : ''
  // 帧率
  let fps = ''
  let droppedFrames = 0
  try {
    const q = (v as any).getVideoPlaybackQuality?.()
    if (q && q.totalVideoFrames > 10 && cur > 2) {
      droppedFrames = q.droppedVideoFrames || 0
      const total = q.totalVideoFrames
      const rate = total > 0 ? ((total - droppedFrames) / total * 100).toFixed(0) : '100'
      const fpsNum = Math.round(total / cur)
      if (fpsNum >= 15 && fpsNum <= 120) fps = fpsNum + ' fps (' + t('player.smoothness') + ' ' + rate + '%)'
    }
  } catch { }

  // 码率和编码 - 多层级获取
  let streamBitrate = ''    // 源声明的码率 (URL/m3u8)
  let currentBitrate = ''   // hls.js 当前播放级别的码率
  let codec = ''

  // 1) 从 URL 提取码率
  let urlBitrate = extractBitrateFromUrl(props.url)
  // 1b) m3u8 URL 本身无码率标记 → 从缓存的 TS 片段 URL 提取
  if (urlBitrate === 0) {
    urlBitrate = extractBitrateFromCachedTs()
  }
  if (urlBitrate > 0) {
    streamBitrate = urlBitrate >= 1000000 ? (urlBitrate / 1000000).toFixed(1) + ' Mbps' : Math.round(urlBitrate / 1000) + ' Kbps'
  }
  // 2) 从 m3u8 streamInfo 提取
  if (!streamBitrate && m3u8StreamInfo.value.length > 0) {
    const best = m3u8StreamInfo.value.reduce((a, b) => a.bandwidth > b.bandwidth ? a : b)
    if (best.bandwidth > 0) {
      streamBitrate = best.bandwidth >= 1000000 ? (best.bandwidth / 1000000).toFixed(1) + ' Mbps' : Math.round(best.bandwidth / 1000) + ' Kbps'
    }
    if (best.codecs) {
      const vc = best.codecs.split(',')[0].split('.')[0]
      if (vc.startsWith('avc')) codec = 'H.264 (AVC)'
      else if (vc.startsWith('hev') || vc.startsWith('hvc')) codec = 'H.265 (HEVC)'
      else if (vc.startsWith('av01') || vc.startsWith('av1')) codec = 'AV1'
      else codec = best.codecs.split(',')[0]
    }
  }

  // 3) hls.js 当前级别信息
  let hlsLevelInfo = ''
  let hlsLevelsCount = 0
  try {
    const hls = (v as any).__hls
    if (hls && hls.levels && hls.levels.length > 0) {
      hlsLevelsCount = hls.levels.length
      const lvlIdx = hls.currentLevel >= 0 ? hls.currentLevel : hls.nextAutoLevel
      const lvl = hls.levels[lvlIdx] || hls.levels[hls.levels.length - 1]
      if (lvl) {
        if (lvl.bitrate) {
          const bps = lvl.bitrate
          currentBitrate = bps >= 1000000 ? (bps / 1000000).toFixed(1) + ' Mbps' : Math.round(bps / 1000) + ' Kbps'
        }
        if (!codec && lvl.videoCodec) {
          const vc = lvl.videoCodec.split('.')[0]
          if (vc.startsWith('avc')) codec = 'H.264 (AVC)'
          else if (vc.startsWith('hev') || vc.startsWith('hvc')) codec = 'H.265 (HEVC)'
          else if (vc.startsWith('av01') || vc.startsWith('av1')) codec = 'AV1'
          else if (vc.startsWith('vp9') || vc.startsWith('vp09')) codec = 'VP9'
          else codec = lvl.videoCodec
        }
        hlsLevelInfo = `Level ${lvlIdx + 1}/${hlsLevelsCount}`
      }
    }
  } catch { }

  // 缓冲进度
  let buffered = ''
  let bufferedSec = 0
  try {
    if (v.buffered.length > 0) {
      const bufEnd = v.buffered.end(v.buffered.length - 1)
      bufferedSec = bufEnd - cur
      const bufPct = dur > 0 ? ((bufEnd / dur) * 100).toFixed(0) : '0'
      buffered = bufPct + '%'
      if (bufferedSec > 0) buffered += ` (${t('player.bufferedAvailable', { time: fmt(bufferedSec) })})`
    }
  } catch { }

  // 源域名 + 完整源 URL
  let sourceHost = ''
  let sourceUrl = ''
  try {
    const u = new URL(props.url)
    sourceHost = u.hostname
    sourceUrl = props.url.length > 80 ? props.url.slice(0, 77) + '...' : props.url
  } catch { }

  // 缓存统计
  const cs = cacheStats.value
  const cacheInfo = t('player.cacheInfoLine', { cached: cs.entries, total: cs.totalSegments, bytes: formatBytes(cs.bytes), rate: (cs.hitRate * 100).toFixed(0) })
  const cacheMode = cs.totalSegments > 0 ? t('player.tscacheActive') : t('player.notActive')

  // 网络模式
  let networkMode = ''
  try {
    networkMode = (TsCache as any).getNetworkMode?.() || ''
  } catch { }

  // 画质模式
  const qm = qualityLabel(qualityMode.value)
  return {
    duration: fmt(dur),
    current: fmt(cur),
    progress: pct + '%',
    resolution,
    fps,
    droppedFrames,
    streamBitrate,
    currentBitrate,
    codec,
    qualityMode: qm,
    volume: Math.round((muted.value ? 0 : volume.value) * 100) + '%',
    speed: speed.value + 'x',
    buffered,
    bufferedSec,
    sourceHost,
    sourceUrl,
    hlsLevelInfo,
    hlsLevelsCount,
    cacheInfo,
    cacheMode,
    networkMode,
  }
})
function updateCacheStats(): void {
  const s = TsCache.stats()
  const changed = cacheStats.value.hits !== s.hits || cacheStats.value.misses !== s.misses
  cacheStats.value = {
    hits: s.hits, misses: s.misses, entries: s.entries, bytes: s.bytes,
    hitRate: s.hitRate, totalSegments: s.totalSegments, prefetched: s.entries,
    prefetchTarget: s.prefetchTarget, queued: s.queued, inflight: s.inflight,
  }
  if (changed && (s.hits + s.misses) > 0) {
    console.log(
      `[TsCache] 命中=${s.hits} 未命中=${s.misses} 命中率=${(s.hitRate * 100).toFixed(0)}% ` +
      `已缓存 ${s.entries} 片 / ${formatBytes(s.bytes)}`
    )
  }
  // 播放质量跟着这条已有的 1s 定时器取样，不再另开一个计时器。
  const v = getVideoEl()
  if (v) samplePerf(v)
}

// ------ 工具 ------
const fmt = (sec: number) => {
  if (!isFinite(sec) || sec <= 0) return '00:00'
  const s = Math.floor(sec)
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  const ss = s % 60
  const mm = m.toString().padStart(2, '0')
  const sss = ss.toString().padStart(2, '0')
  if (h > 0) return `${h}:${mm}:${sss}`
  return `${mm}:${sss}`
}
function isHls(u: string): boolean {
  return /\.m3u8(\?|$)/i.test(u || '')
}

// ------ 获取 video 元素 ------
function getVideoEl(): HTMLVideoElement | null {
  const el = wrapperRef.value?.querySelector('video') as HTMLVideoElement | null
  return el || null
}

// ⭐ 关键：计算唯一的"进度保存 key"
//   - 优先使用 props.videoKey（Player.vue 已传入 `player_${vodId}_${epIndex}`）
//   - 否则用 ep_url 的稳定 hash（去掉 query token，避免 ?token=xxx 变化导致 key 漂移）
function stableResumeKey(): string {
  if (props.videoKey) return 'vp_t_' + props.videoKey
  // 回退：取 URL 的 origin+pathname 部分（去掉 ?query / #hash）
  try {
    const u = new URL(props.url)
    return 'vp_t_url_' + u.origin + u.pathname
  } catch {
    // 非标准 URL：截断前 120 字符
    return 'vp_t_url_' + props.url.split('?')[0].split('#')[0].slice(0, 120)
  }
}

// 🔴 关键修复：区分两种 play 场景
// 场景 A：程序触发的自动播放（初始化/切源时）→ 允许静音fallback
// 场景 B：用户点击触发 → 必须尊重用户的音量设置，不要自动静音
let _playToken = 0
let _userGestureActive = false // 由用户点击触发的标志

function safePlay(auto: boolean): void {
  const v = getVideoEl()
  if (!v) return
  const token = ++_playToken
  // ⭐ 关键修复：不要先 pause() → 浏览器会把 pause 当成打断 play 请求，导致 AbortError
  // 直接调用 play() 即可，浏览器会处理冲突
  const p = v.play() as Promise<void> | undefined
  if (p && typeof p.then === 'function') {
    p
      .then(() => {
        if (_playToken !== token) return
        console.log('[Player] ▶ 播放成功')
      })
      .catch((err) => {
        if (_playToken !== token) return
        const errName = err?.name || ''
        console.warn('[Player] play 被拒绝:', errName, 'auto=', auto)
        // NotSupportedError: 媒体格式不支持，静音重试无效，直接报错
        if (errName === 'NotSupportedError') {
          errorMsg.value = t('player.errFormatUnsupported')
          loading.value = false
          return
        }
        // 只有自动播放（非用户点击）时才 fallback 到静音
        if (auto) {
          try { v.muted = true } catch { /* ignore */ }
          muted.value = true
          v.play().catch(() => { /* 最终失败，放弃 */ })
        }
      })
  }
}

// ------ 播放器加载 ------
// 一次 loadHls 里最多有三处 await（取续播位置、拉 hls.js、解析 m3u8），换集/换源只要
// 落在这些 await 上，旧的那次就会在停播之后继续往下跑：它会新建第二个 hls 实例挂到
// 同一个 video 上（hlsEngine 只认得后一个，前一个再也不被 dispose）、把上一集的片段表
// 灌进 TsCache、并把缓存统计定时器重复起一遍。用代号把这些半途醒来的调用一次性作废。
let _loadToken = 0
async function loadHls(video: HTMLVideoElement, url: string): Promise<void> {
	const playbackURL = proxyHlsURL(url)
  console.log('[Player] 🔄 开始加载视频:', url.slice(-80))
  // 首播耗时从这一刻开始量：它包含读进度、拉运行时、解析列表，也就是用户感知的等待。
  perf.beginLoad(hostOf(url))
  resetPerfBaseline(video)
  destroyPlayerInternal(video)
  const token = ++_loadToken
  // 清理旧播放器会解除事件监听；必须随后重新绑定，否则 play/pause
  // 不会同步到 playing，播放中的画面仍会显示暂停图标。
  bindCommonVideoEvents(video)
  errorMsg.value = ''
  clearNetworkErrTimer()
  loading.value = true
  try {
    console.log('[Player] 启动 TsCache')
    TsCache.enable()

    // 异步读取上次播放位置（不阻塞播放）
    showResumePrompt.value = false
    savedTime.value = null
    _resumeAutoJump = loadAutoJumpConfig()
    // ⭐ 使用稳定 key 读取；无 videoKey 且 URL 不稳定时不恢复，避免跨集污染
    const resumeKey = stableResumeKey()
    const localTime = readPlaybackTime(resumeKey)
    // localStorage 按 origin 分区（独立 exe 与 dev 端口互不可见），本地缺失时回退后端 watch_history。
    let t = localTime
    if (t <= 5) {
      t = (await props.resolveResume?.()) || 0
      // 这一趟出网回来时用户可能已经换集了，位置不能再往新媒体上跳。
      if (token !== _loadToken) return
    }
    if (t > 5) {
      savedTime.value = t
      if (_resumeAutoJump) {
        seekWhenReady(Math.max(0, t - 1))
        console.log(`[Player] ⏩ 自动跳到上次播放位置: ${t.toFixed(1)}s (key=${resumeKey} 来源=${localTime > 5 ? '本地' : '后端'})`)
      } else {
        startResumePrompt(t)
      }
    } else {
      console.log(`[Player] ℹ️ 无有效历史进度 (key=${resumeKey})，从头播放`)
    }

    console.log('[Player] 动态 import hls.js')
    const Hls = await hlsEngine.loadRuntime()
    if (token !== _loadToken) return
    if (Hls.isSupported()) {
      // 1) 用 TsCache 解析 m3u8（文本缓存，避免重复请求 m3u8）
      //    同时激活 fetch 拦截器，hls.js 的 TS 片段下载会透明经过缓存
      TsCache.enable()
      let parsed: { urls: string[], variantUrls: string[], targetduration: number, isMaster: boolean, streamInfo?: StreamVariantInfo[] }
      try {
        parsed = await TsCache.fetchAndParseM3u8(playbackURL)
      } catch {
        parsed = { urls: [], variantUrls: [], targetduration: 6, isMaster: false, streamInfo: [] }
      }
      if (token !== _loadToken) return

      // 存储 m3u8 流信息供视频信息弹窗使用
      if (parsed.streamInfo && parsed.streamInfo.length > 0) {
        m3u8StreamInfo.value = parsed.streamInfo
      }

      // 判断是否为单码率（media playlist），如是则立即设置 segments 列表
      const isMediaPlaylist = !parsed.isMaster && parsed.urls.length > 0 && parsed.urls[0].toLowerCase().match(/\.(ts|aac|mp4|m4s)(\?|$)/)
      if (isMediaPlaylist) {
        TsCache.setSegments(parsed.urls)
        TsCache.setTargetDuration(parsed.targetduration)
        // 单码率时从 TS URL 推断码率
        if (parsed.urls.length > 0 && m3u8StreamInfo.value.length === 0) {
          const bitrateHint = extractBitrateFromUrl(parsed.urls[0])
          if (bitrateHint > 0) {
            m3u8StreamInfo.value = [{ bandwidth: bitrateHint, resolution: '', codecs: '', url: parsed.urls[0] }]
          }
        }
        console.log(`[Player] ✅ m3u8 (单码率): ${parsed.urls.length} 片段, targetduration=${parsed.targetduration}`)
      } else {
        console.log(`[Player] ✅ m3u8 (多码率): ${parsed.variantUrls.length || '?'} 个码率, 由 hls.js 管理`)
      }

      // 2) hls.js 配置：v1.7.0-beta.1 统一 loader API
      //    - TsCache.TsCacheLoader 处理所有请求（manifest、level、fragment）
      //    - TS 片段命中 LRU 缓存 → 极速 onSuccess
      //    - m3u8 文本缓存 → 避免重复请求同一个播放列表
      //    - stats 完全匹配 hls.js LoadStats 结构 → ABR controller 正常工作
      //    - 【不调用 onProgress】→ 彻底避免 data.chunkCount 崩溃
      console.log('[Player] ✅ TsCacheLoader 已激活（hls.js v1.7.0-beta.1 统一 loader API）')

      const hlsConfig: any = {
        enableWorker: false,
        lowLatencyMode: false,
        maxBufferLength: 30,
        maxMaxBufferLength: 60,
        backBufferLength: 30,
        maxBufferSize: 60 * 1000 * 1000,
        fragLoadingTimeOut: 15000,
        fragLoadingMaxRetry: 8,
        fragLoadingRetryDelay: 500,
        manifestLoadingTimeOut: 10000,
        manifestLoadingMaxRetry: 3,
        manifestLoadingRetryDelay: 700,
        // ⭐ v1.7.0-beta.1 统一 loader：一个 loader 处理所有请求类型
        loader: TsCache.TsCacheLoader,
      }

      const hls = hlsEngine.create(video, hlsConfig, Hls)

      // ⭐ v3: 注册 ABR 降级回调 —— TsCache 检测到连续慢分片时主动降码率
      TsCache.setAbrSwitchCallback((targetLevel: number) => {
        if (!hls || !hls.levels || hls.levels.length <= 1) return
        const currentLevel = hls.currentLevel >= 0 ? hls.currentLevel : hls.nextAutoLevel
        if (targetLevel === -1) {
          // 降一级
          const newLevel = Math.max(0, currentLevel - 1)
          if (newLevel < currentLevel) {
            hls.nextAutoLevel = newLevel
            console.log(`[Player] ⬇️ ABR 降级: level ${currentLevel} → ${newLevel} (bitrate: ${hls.levels[newLevel]?.bitrate || '?'})`)
          }
        } else if (targetLevel >= 0 && targetLevel < hls.levels.length) {
          hls.nextAutoLevel = targetLevel
          console.log(`[Player] ↕️ ABR 切换: → level ${targetLevel}`)
        }
      })

      let firstPlayTriggered = false
      hls.on(Hls.Events.MANIFEST_PARSED, (_e: any, data: any) => {
        console.log('[Player] ✅ manifest 解析完成，levels=', data?.levels?.length || 0)
      })
      hls.on(Hls.Events.LEVEL_LOADED, (_e: any, data: any) => {
        // 从 hls.js 的 fragments 拿到真实 TS URL（多码率/单码率都适用）
        const frags = data?.details?.fragments || []
        const curTargetDur = data?.details?.targetduration || parsed.targetduration || 6
        if (frags.length > 0) {
          const absUrls = frags.map((f: any) => {
            try { return new URL(f.url, playbackURL).href }
            catch { return f.url }
          })
          TsCache.setSegments(absUrls)
          TsCache.setTargetDuration(curTargetDur)
        }
        // 首次加载完成 → 从 buffer 之后开始预取
        if (!firstPlayTriggered && props.autoplay !== false && frags.length > 0) {
          firstPlayTriggered = true
          // ⭐ 预取策略：从 hls.js 当前 buffer 之后 +15 片开始，超前预取
          //   hls.js 自己会拉取紧接的 5-10 片，我们专注于更远处的片段
          // hls.js owns startup buffering. TsCache starts its low-priority
          // near-playback work only after updateBuffer reports spare media.
          updateBuffer()
        }
      })
      hls.on(Hls.Events.FRAG_CHANGED, (_e: any, data: any) => {
        if (data?.frag?.url) {
          try { TsCache.notifyCurrentTs(new URL(data.frag.url, playbackURL).href) }
          catch { TsCache.notifyCurrentTs(data.frag.url) }
        }
      })
hls.on(Hls.Events.ERROR, (_e: any, data: any) => {
	        if (!data) return
	        const details = String(data.details || '')
	        const isSoft =
	          details === 'bufferStalledError' ||
	          details === 'bufferSeekOverHole' ||
	          details === 'levelLoadingError'
	        if (isSoft) {
	          console.debug(`[Player] 缓冲/网络波动: ${details}`)
	          return
	        }
	        const fatalFlag = data.fatal ? '🔴 FATAL ' : ''
	        console.log(`[Player] ${fatalFlag}ERROR type=${data.type} details=${details} err=${data.err || ''}`)
	        // 只记 fatal：软错误（缓冲波动、seek 空洞）在卡顿计数里已经算过一次，
	        // 记两遍会让「错误」变成噪音而不是故障信号。
	        if (data.fatal) perf.noteError(details || String(data.type || ''), hostOf(props.url))

	        if (data.fatal) {
	          // ⭐ 非 m3u8 URL 回退：manifest 加载/解析失败 → 尝试直接 video.src
	          const isManifestError =
	            details === 'manifestLoadError' ||
	            details === 'manifestParsingError' ||
	            details === 'manifestIncompatibleCodecsError' ||
	            details === 'manifestLoadTimeOut'
	          if (isManifestError) {
	            console.log('[Player] ⚠ HLS manifest 失败，回退到直接播放')
            hlsEngine.dispose(video)
            video.src = playbackURL
	            video.onerror = () => {
	              errorMsg.value = tr('player.errLoadFailed')
	            }
	            return
	          }
	          switch (data.type) {
            case Hls.ErrorTypes.NETWORK_ERROR:
              console.log('[Player] 网络错误，尝试恢复 startLoad()')
              try { hls.startLoad() } catch (e) { console.warn('[Player] startLoad 失败:', e) }
              // 用户主动暂停时不启动故障倒计时。暂停期间没有播放进度是正常状态，
              // 不能据此判断连接失败。
              if (!video.paused && !video.ended && !networkErrTimer) {
                networkErrTimer = setTimeout(() => {
                  if (video.paused || video.ended) {
                    networkErrTimer = null
                    return
                  }
                  showNetworkError.value = true
                  errorMsg.value = tr('player.errNetworkTimeout')
                  networkErrTimer = null
                }, 10000)
              }
              break
            case Hls.ErrorTypes.MEDIA_ERROR:
              console.log('[Player] 媒体错误，尝试恢复 recoverMediaError()')
              try { hls.recoverMediaError() } catch (e) { console.warn('[Player] recoverMediaError 失败:', e) }
              break
            default:
              errorMsg.value = tr('player.errStreamLoadFailed', { detail: details || data.type || tr('player.unknownError') })
              console.error('[Player] ❌ 无法恢复的错误:', data)
              destroyPlayerInternal(getVideoEl() || undefined as any)
              break
          }
        }
      })
      console.log('[Player] 调用 hls.loadSource:', url.slice(-80))
      hls.loadSource(playbackURL)
      hls.attachMedia(video)
      // 启动缓存监控定时器（每秒更新一次）
      if (cacheStatsTimer != null) { window.clearInterval(cacheStatsTimer); cacheStatsTimer = null }
      updateCacheStats()
      cacheStatsTimer = window.setInterval(updateCacheStats, 1000)
    } else if (video.canPlayType('application/vnd.apple.mpegurl')) {
      console.log('[Player] ⚠ hls.js 不支持，回退原生 HLS')
      video.src = playbackURL
      if (props.autoplay !== false) safePlay(true)
    } else {
      errorMsg.value = tr('player.errHlsUnsupported')
      console.error('[Player] ❌ 当前环境不支持 HLS 播放')
    }
  } catch (e: any) {
    console.error('[Player] ❌ 异常:', e)
    errorMsg.value = t('player.errInitFailed', { detail: e?.message || String(e) })
  }
}

let _retryCount = 0
let _setupRetryTimer: number | null = null
function setupPlayer(): void {
  const video = getVideoEl()
  if (!video) {
    if (_retryCount < 8) {
      _retryCount++
      _setupRetryTimer = window.setTimeout(setupPlayer, 50)
    } else {
      errorMsg.value = t('player.errPlayerCreateFailed')
    }
    return
  }
  _retryCount = 0

  // ⭐ 提前恢复音量：在视频开始加载前设置，避免 loadedmetadata 之前的默认音量覆盖
  const savedVol = parseFloat(readSetting('volume', '1'))
  if (isFinite(savedVol) && savedVol >= 0 && savedVol <= 1) {
    try { video.volume = savedVol; volume.value = savedVol } catch { /* ignore */ }
  }
  const savedMuted = readSetting('muted', '0') === '1'
  try { video.muted = savedMuted; muted.value = savedMuted } catch { /* ignore */ }
  const savedSpeed = parseFloat(readSetting('speed', '1'))
  if (isFinite(savedSpeed) && savedSpeed >= 0.5 && savedSpeed <= 2) {
    try { video.playbackRate = savedSpeed; speed.value = savedSpeed } catch { /* ignore */ }
  }

  const url = props.url
  if (!url) {
    errorMsg.value = t('player.errNoVideoUrl')
    return
  }
  // 非 .m3u8 后缀也走这条路：短链常 302 到 HLS，loadHls 先拆旧实例、按 m3u8 解析，
  // 拿不到清单再回退 video.src，两条分支本来就汇到同一个调用。
  loadHls(video, url)
}

function bindCommonVideoEvents(video: HTMLVideoElement): void {
  if ((video as any).__eventsBound) return
  ;(video as any).__eventsBound = true
  const eventController = new AbortController()
  ;(video as any).__eventAbortController = eventController
  const on = (name: string, listener: EventListenerOrEventListenerObject) =>
    video.addEventListener(name, listener, { signal: eventController.signal })
  bindOsdListeners(video, eventController.signal)

  on('play', () => {
    playing.value = true
    loading.value = false
    videoReady.value = true
    stopLoadingStats()
    clearNetworkErrTimer()

    // 暂停较久后 HLS 可能已经停止拉流。仅在没有可播放未来帧时，
    // 从当前位置恢复加载，避免无条件重启造成已有缓冲失效。
    if (video.readyState < HTMLMediaElement.HAVE_FUTURE_DATA) {
      const hls = (video as any).__hls
      if (hls) {
        try { hls.startLoad(video.currentTime) }
        catch (e) { console.warn('[Player] 恢复播放时重新拉流失败:', e) }
      }
    }
  })
  on('pause', () => {
    playing.value = false
    loading.value = false
    stopLoadingStats()
    clearNetworkErrTimer()
  })
  on('timeupdate', () => {
    current.value = video.currentTime
    if (video.duration) duration.value = video.duration
    updateBuffer()
    // ⭐ 节流保存进度：使用稳定 key，避免跨集污染
    if (_saveTimer == null) {
      _saveTimer = window.setInterval(() => {
        const key = stableResumeKey()
        savePlaybackTime(key, current.value, duration.value)
        // 同时保存用户音量
        const v = getVideoEl()
        if (v) { writeSetting('volume', String(v.volume)); writeSetting('muted', v.muted ? '1' : '0') }
      }, SAVE_INTERVAL_MS)
    }
  })
  on('loadedmetadata', () => {
    duration.value = video.duration
    // ⭐ 恢复用户上次的音量设置
    const savedVol = parseFloat(readSetting('volume', '1'))
    if (isFinite(savedVol) && savedVol >= 0 && savedVol <= 1) {
      try { video.volume = savedVol; volume.value = savedVol } catch { /* ignore */ }
    }
    const savedMuted = readSetting('muted', '0') === '1'
    try { video.muted = savedMuted; muted.value = savedMuted } catch { /* ignore */ }
    const savedSpeed = parseFloat(readSetting('speed', '1'))
    if (isFinite(savedSpeed) && savedSpeed >= 0.5 && savedSpeed <= 2) {
      try { video.playbackRate = savedSpeed; speed.value = savedSpeed } catch { /* ignore */ }
    }
    updateBuffer()
    flushPendingSeek()
    console.log(`[Player] loadedmetadata: duration=${video.duration.toFixed(1)}s, volume=${video.volume.toFixed(2)}`)
    // ⭐ 换集/换源后自动重建 AI 增强：destroyPlayerInternal 会 resetPipeline() 把 WebGL 上下文
    // 还掉，而元数据就绪是唯一安全的挂点（要有 videoWidth/Height 才能建管线）。要不要重试由
    // useVideoQuality 自己判断（它记得用户想要的档位）。
    onMediaReady()
  })
  on('progress', updateBuffer)
  on('seeking', updateBuffer)
  on('seeked', () => {
    updateBuffer()
    notifySeeked()
  })
  on('waiting', () => {
    if (video.paused || video.ended) return
    if (!_stallSince) _stallSince = performance.now()
    loading.value = true
    startLoadingStats()
  })
  on('playing', () => {
    perf.markFirstFrame()
    endStall()
  })
  on('canplay', () => {
    videoReady.value = true
    perf.markFirstFrame()
    endStall()
    // 预缓冲阶段：不设置 loading=false，不自动播放，等待预缓冲完成
    if (preBuffering.value) {
      console.log('[Player] canplay 但预缓冲尚未完成，等待中...')
      return
    }
    loading.value = false
    // ⭐ 修复：视频解码出帧后才触发自动播放，避免 AbortError
    if (props.autoplay !== false && !video.hasAttribute('data-autoplay-done')) {
      video.setAttribute('data-autoplay-done', '1')
      console.log('[Player] ✅ canplay → 触发自动播放')
      safePlay(true)
    }
  })
  on('volumechange', () => {
    volume.value = video.volume
    muted.value = video.muted
    writeSetting('volume', String(video.volume))
    writeSetting('muted', video.muted ? '1' : '0')
    // ⭐ 统一由 volumechange 触发 toast，覆盖所有场景（滚轮/键盘/按钮）
    showVolumeToastRef()
  })
  on('ratechange', () => { speed.value = video.playbackRate; writeSetting('speed', String(video.playbackRate)) })
  on('enterpictureinpicture', () => { isPiP.value = true })
  on('leavepictureinpicture', () => { isPiP.value = false })
  on('ended', () => {
    // 播放结束：移除当前进度（下次不跳回结尾）
    clearPlaybackTime(stableResumeKey())
    if (_saveTimer != null) { window.clearInterval(_saveTimer); _saveTimer = null }
  })
  on('error', () => {
    loading.value = false
    // 媒体元素自己的错误只在换源回退直连时出现（hls.js 路径的错误走 Hls.Events.ERROR）。
    // 首播耗时不结算：这次装载根本没有出画，记它会把它算成一次成功的快。
    endStall()
    perf.noteError(String(video.error?.code ?? ''), hostOf(props.url))
  })
}

// ====== 继续播放提示 ======
function startResumePrompt(t: number): void {
  savedTime.value = t
  showResumePrompt.value = true
  resumeRemainSec.value = PROMPT_SEC
  if (_resumeTimer != null) { window.clearInterval(_resumeTimer); _resumeTimer = null }
  _resumeTimer = window.setInterval(() => {
    resumeRemainSec.value -= 1
    if (resumeRemainSec.value <= 0) {
      if (_resumeTimer != null) { window.clearInterval(_resumeTimer); _resumeTimer = null }
      showResumePrompt.value = false
    }
  }, 1000)
}
function dismissResumePrompt(): void {
  showResumePrompt.value = false
  if (_resumeTimer != null) { window.clearInterval(_resumeTimer); _resumeTimer = null }
}

/**
 * 恢复进度用的跳转。TsCache/hls.js 要等 attachMedia 之后才有可 seek 的媒体，
 * 在这之前直接写 video.currentTime 会被浏览器丢掉、于是从 0 开始播，
 * 所以不可用时先挂起，由 loadedmetadata 兜底执行。
 */
function seekWhenReady(sec: number): void {
  const v = getVideoEl()
  if (!v) return
  if (v.readyState >= HTMLMediaElement.HAVE_METADATA && Number.isFinite(v.duration) && v.duration > 0) {
    _pendingSeekSec = null
    try { v.currentTime = sec } catch { /* ignore */ }
    console.log(`[Player] ⏩ 跳到: ${sec.toFixed(1)}s`)
    return
  }
  _pendingSeekSec = sec
  console.log(`[Player] ⏳ 媒体尚未就绪，稍后跳到: ${sec.toFixed(1)}s`)
}

function flushPendingSeek(): void {
  if (_pendingSeekSec == null) return
  const sec = _pendingSeekSec
  _pendingSeekSec = null
  const v = getVideoEl()
  if (!v) return
  try { v.currentTime = sec; console.log(`[Player] ⏩ 补跳上次位置: ${sec.toFixed(1)}s`) } catch { /* ignore */ }
}

function jumpToSavedTime(autoRememberChoice: boolean): void {
  const t = savedTime.value
  if (t != null) seekWhenReady(Math.max(0, t - 1))
  if (autoRememberChoice) { saveAutoJumpConfig(true); console.log('[Player] ✅ 已记住：自动跳到上次播放位置') }
  dismissResumePrompt()
}

function destroyPlayerInternal(video: HTMLVideoElement): void {
  ++_playToken
  // 拆实例的同时作废所有在途的 loadHls，见 _loadToken。
  ++_loadToken
  TsCache.setAbrSwitchCallback(null)
  hlsEngine.dispose(video)
  try {
    try { video.pause() } catch { /* ignore */ }
    try { video.removeAttribute('src') } catch { /* ignore */ }
    try { video.load() } catch { /* ignore */ }
    try { video.removeAttribute('data-autoplay-done') } catch { /* ignore */ }
    try { (video as any).__eventAbortController?.abort() } catch { /* ignore */ }
    try { delete (video as any).__eventAbortController } catch { /* ignore */ }
    try { delete (video as any).__eventsBound } catch { /* ignore */ }
  } catch { /* ignore */ }
  if (cacheStatsTimer != null) { window.clearInterval(cacheStatsTimer); cacheStatsTimer = null }
  if (_saveTimer != null) { window.clearInterval(_saveTimer); _saveTimer = null }
  if (_resumeTimer != null) { window.clearInterval(_resumeTimer); _resumeTimer = null }
  // 换集/换源时挂起的跳转必须作废，否则会把上一集的位置跳到新媒体上
  _pendingSeekSec = null
  // 清理预缓冲定时器
  preBuffering.value = false
  if (prebufferCheckTimer) { clearInterval(prebufferCheckTimer); prebufferCheckTimer = null }
  if (prebufferTimeout) { clearTimeout(prebufferTimeout); prebufferTimeout = null }
  stopLoadingStats()
  resetPipeline()
  cacheStats.value = {
    hits: 0, misses: 0, entries: 0, bytes: 0, hitRate: 0, totalSegments: 0, prefetched: 0,
    prefetchTarget: 0, queued: 0, inflight: 0,
  }
  loading.value = true
  videoReady.value = false
  showResumePrompt.value = false
}

function destroyPlayer(): void {
  const v = getVideoEl()
  if (v) destroyPlayerInternal(v)
}

// ------ 用户交互控制 ------
function togglePlay(): void {
  const v = getVideoEl()
  if (!v) return
  _userGestureActive = true
  if (v.paused) {
    safePlay(false) // 用户点击，不允许静音 fallback
  } else {
    v.pause()
  }
  setTimeout(() => { _userGestureActive = false }, 100)
  keepVisible()
}

// 滚轮调整音量：向上=+2%，向下=-2%
function onWheel(e: WheelEvent): void {
  const v = getVideoEl()
  if (!v) return
  const delta = e.deltaY < 0 ? 0.02 : -0.02
  v.volume = Math.max(0, Math.min(1, v.volume + delta))
  if (v.volume > 0 && v.muted) { v.muted = false }
  if (v.volume === 0 && !v.muted) { v.muted = true }
  volume.value = v.volume
  muted.value = v.muted
  keepVisible()
  // ⭐ volumechange 事件会统一触发 showVolumeToastRef()，此处不再重复调用
}

function toggleMute(): void {
  const v = getVideoEl()
  if (!v) return
  v.muted = !v.muted
  muted.value = v.muted
  if (!v.muted && v.volume === 0) {
    v.volume = 0.5
    volume.value = 0.5
  }
}

function changeVolume(e: Event): void {
  const v = getVideoEl()
  if (!v) return
  const target = e.target as HTMLInputElement
  const val = parseFloat(target.value)
  v.volume = val
  volume.value = val
  if (val > 0 && v.muted) { v.muted = false; muted.value = false }
  keepVisible()
}

// ⭐ 新：新的音量处理 + 可视化临时 toast
const showVolumeToast = ref(false)
const toastVolumePct = ref(100)
let _toastTimer: number | null = null
function onVolumeInput(e: Event): void {
  changeVolume(e)
  // ⭐ volumechange 事件已统一触发 showVolumeToastRef()
}
function showVolumeToastRef(): void {
  const v = getVideoEl()
  if (!v) return
  toastVolumePct.value = Math.round(v.volume * 100)
  showVolumeToast.value = true
  if (_toastTimer != null) window.clearTimeout(_toastTimer)
  _toastTimer = window.setTimeout(() => { showVolumeToast.value = false }, 1000)
}

const progressContainerRef = ref<HTMLDivElement>()
const thumbCanvasRef = ref<HTMLCanvasElement | null>(null)
const {
  progressHoverPct,
  progressHoverTime,
  thumbPreviewImg,
  thumbPreviewVisible,
  initThumbSampler,
  destroyThumbSampler,
  onProgressHover,
} = useThumbnailScrubber({
  wrapperRef,
  progressContainerRef,
  thumbCanvasRef,
  getVideoEl,
  getUrl: () => props.url,
  isHlsUrl: isHls,
})

function seek(e: Event): void {
  const v = getVideoEl()
  if (!v) return
  const target = e.target as HTMLInputElement
  const val = parseFloat(target.value)
  if (!isFinite(val)) return
  v.currentTime = val
  current.value = val
    // ⭐ 修复：seek 后让输入框失焦，避免键盘事件被拦截
    ; (target as HTMLElement).blur()
  wrapperRef.value?.focus?.()
}

function seekRelative(delta: number): void {
  const v = getVideoEl()
  if (!v) return
  const newTime = Math.max(0, Math.min(v.duration || 0, v.currentTime + delta))
  v.currentTime = newTime
  current.value = newTime
}

// 通过鼠标位置直接计算跳转时间（比 range input 更精确）
function onProgressMouseDown(e: MouseEvent): void {
  const v = getVideoEl()
  if (!v || !progressContainerRef.value || !v.duration) return
  const rect = progressContainerRef.value.getBoundingClientRect()
  const pct = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width))
  const newTime = pct * v.duration
  v.currentTime = newTime
  current.value = newTime
  // ⭐ 修复：seek 后让输入框失焦，避免键盘事件被拦截
  const slider = progressContainerRef.value.querySelector('.progress-slider') as HTMLElement | null
  if (slider) slider.blur()
  wrapperRef.value?.focus?.()
}

// ========= 操作 OSD（屏幕中央提示：快进/快退/音量/倍速） =========
const { osdText, osdIcon, bindOsdListeners, clearOsdTimer } = usePlayerOsd()

function changeSpeed(s: number): void {
  const v = getVideoEl()
  if (v) {
    v.playbackRate = s
    speed.value = s
    writeSetting('speed', String(s))
  }
  showSpeedPanel.value = false
}

// ====== 全屏（优先使用 Wails 系统级全屏 WindowFullscreen）======
// 要点：
//   · Wails WindowFullscreen 让整个应用窗口全屏，隐藏标题栏并覆盖 Windows 任务栏，这样视频播放时，"真全屏"
//   · 为避免关闭播放器时，通过 document.body 设置 data-player-fullscreen=1 让父级 player-modal/player-box 也扩展到整个窗口 100vw/100vh，从而让视频真全屏时全级联到整个窗口。
//   · onFsChange() 同步更新 isFullscreen 并确保播放器在系统态。
//   · 注意退出全屏前记录当时播放：设置 videoSrc 播放状态：
let _wasMax = false

async function toggleFullscreen(): Promise<void> {
  // 只有在 Wails 环境（window.go 存在）下才能调用 Go 侧窗口 API
  const useNative = !!(window as any).go

  // ========== 退出全屏流程 ==========
  if (isFullscreen.value) {
    // 1) Wails 系统级全屏 → 用 WindowSetFullscreen(false) 退出
    if (useNative) {
      try { WindowSetFullscreen(false) } catch (e) { console.warn('WindowSetFullscreen(false) 失败:', e) }
      // 2) 如果进入前窗口是最大化 → 恢复最大化
      if (_wasMax) {
        try {
          const isMaxNow = await WindowIsMax()
          if (!isMaxNow) WindowToggleMax()
        } catch { }
      }
    }
    // 3) 浏览器元素级全屏兜底：同时尝试退出
    const doc = document as any
    if (doc.fullscreenElement || doc.webkitFullscreenElement) {
      try {
        if (doc.exitFullscreen) await doc.exitFullscreen()
        else if (doc.webkitExitFullscreen) doc.webkitExitFullscreen()
      } catch (e) { console.warn('exitFullscreen 失败:', e) }
    }
    isFullscreen.value = false
    document.body.removeAttribute('data-player-fullscreen')
    keepVisible()
    return
  }

  // ========== 进入全屏流程 ==========
  // 记录进入前是否最大化（退出时恢复）
  if (useNative) {
    try { _wasMax = await WindowIsMax() } catch { _wasMax = false }
  }

  // 优先 Wails WindowSetFullscreen(true)（系统级全屏，覆盖任务栏）
  if (useNative) {
    try { WindowSetFullscreen(true) } catch (e) { console.warn('WindowSetFullscreen 失败:', e) }
  } else {
    // 非 Wails 环境：回退到浏览器元素级全屏
    const el = wrapperRef.value
    if (el) {
      try {
        const req = (el as any).requestFullscreen || (el as any).webkitRequestFullscreen
        if (req) {
          const p = req.call(el)
          if (p && typeof p.then === 'function') await p
        }
      } catch (e) { console.warn('requestFullscreen 失败:', e) }
    }
  }

  isFullscreen.value = true
  document.body.setAttribute('data-player-fullscreen', '1')
  keepVisible()
}

function onFsChange(): void {
  const doc = document as any
  const onFs = !!doc.fullscreenElement || !!(doc as any).webkitFullscreenElement
  isFullscreen.value = onFs
  if (onFs) document.body.setAttribute('data-player-fullscreen', '1')
  else document.body.removeAttribute('data-player-fullscreen')
}

// ====== 画中画 (PiP) ======
const isPiP = ref(false)

async function togglePiP(): Promise<void> {
  const v = getVideoEl()
  if (!v) return
  // ⭐ 视频元数据未加载时不能进入画中画
  if (v.readyState < 1) {
    console.warn('[Player] PiP: 视频尚未就绪')
    return
  }
  try {
    if (document.pictureInPictureElement) {
      await document.exitPictureInPicture()
      isPiP.value = false
    } else {
      const pip = (v as any).requestPictureInPicture?.()
      if (pip && typeof pip.then === 'function') {
        await pip
      }
      isPiP.value = true
    }
  } catch (e) {
    console.warn('[Player] PiP 切换失败:', e)
    isPiP.value = false
  }
}

// ------ 控制条显示/隐藏 ------
// 规则：
//   · mousemove → 显示 + 启动 3 秒隐藏定时器（每次移动都重置）
//   · mouseleave → 延迟检查，如果鼠标在弹出面板内则不隐藏
//   · 键盘操作（上下键等）→ 显示 + 重置定时器
//   · 暂停时（!playing）→ 控制条保持可见（方便点击播放）
function toggleShow(visible: boolean): void {
  showControls.value = visible
  if (hideTimer !== null) {
    window.clearTimeout(hideTimer)
    hideTimer = null
  }
  if (visible) {
    hideTimer = window.setTimeout(() => {
      showControls.value = false
    }, 3000)
  }
}

// 延迟检查鼠标是否在弹出面板内，如果是则保持控制条可见
let _mouseLeaveTimer: number | null = null
function onMouseLeave(): void {
  mouseInside.value = false
  if (_mouseLeaveTimer) {
    window.clearTimeout(_mouseLeaveTimer)
    _mouseLeaveTimer = null
  }
  _mouseLeaveTimer = window.setTimeout(() => {
    _mouseLeaveTimer = null
    // 检查鼠标是否在弹出面板内
    const activeEl = document.activeElement
    const dropdownPanels = document.querySelectorAll('.select-panel')
    const isInDropdown = Array.from(dropdownPanels).some(panel => {
      return panel.contains(activeEl) || panel.matches(':hover')
    })
    if (!isInDropdown && !showVolumePanel.value && !showSpeedPanel.value && !qualityOpen.value) {
      toggleShow(false)
    }
  }, 500)
}

// 新工具：供键盘/点击调用（只刷新“可见 3 秒”，不会误切换显示状态）
function keepVisible(): void {
  showControls.value = true
  if (hideTimer !== null) {
    window.clearTimeout(hideTimer)
    hideTimer = null
  }
  hideTimer = window.setTimeout(() => {
    showControls.value = false
  }, 3000)
}

// ------ 键盘控制（仅在鼠标位于播放器内 或 全屏时生效） ------
function onKeyDown(e: KeyboardEvent): void {
  // ⭐ V2：键盘快捷键现已由 Player.vue 页面级统一处理
  //   VideoPlayer 仅在全屏模式下保留 ESC 退出逻辑，避免重复响应
  if (!isFullscreen.value) return
  if (e.key === 'Escape') {
    e.preventDefault()
    toggleFullscreen()
  }
}

// 点击空白处关闭弹出面板
function onWrapperClick(): void {
  // 只有视频已就绪（或正在播放/暂停中）时才 toggle play
  if (!loading.value || videoReady.value) {
    togglePlay()
  }
  // 关闭所有弹出面板
  showVolumePanel.value = false
  showSpeedPanel.value = false
}

// 挂载后那两件事（抢焦点、建缩略图采样器）都排在 100ms 之后，快速进出播放页时这个
// 定时器会跑到卸载之后的组件上，所以和 setupPlayer 的重试定时器一起记账、离开工件时清掉。
let _bootTimer: number | null = null

onMounted(async () => {
  await nextTick()
  setupPlayer()
  document.addEventListener('fullscreenchange', onFsChange)
  document.addEventListener('webkitfullscreenchange', onFsChange)
  document.addEventListener('keydown', onKeyDown)
  // 让播放器区域自动获得键盘焦点（上下键调节音量等）
  _bootTimer = window.setTimeout(() => {
    _bootTimer = null
    wrapperRef.value?.focus?.()
    initThumbSampler()
  }, 100)
})

onBeforeUnmount(() => {
  if (_bootTimer !== null) { window.clearTimeout(_bootTimer); _bootTimer = null }
  if (_setupRetryTimer !== null) { window.clearTimeout(_setupRetryTimer); _setupRetryTimer = null; _retryCount = 0 }
  destroyPlayer()
  destroyThumbSampler()
  clearNetworkErrTimer()
  document.removeEventListener('fullscreenchange', onFsChange)
  document.removeEventListener('webkitfullscreenchange', onFsChange)
  document.removeEventListener('keydown', onKeyDown)
  if (hideTimer !== null) window.clearTimeout(hideTimer)
  // ⭐ 关键：关闭播放器时强制退出系统级全屏
  // 1) 如果当前处于浏览器元素级全屏 → 退出
  const doc = document as any
  if (doc.fullscreenElement || doc.webkitFullscreenElement) {
    try {
      if (doc.exitFullscreen) doc.exitFullscreen()
      else if (doc.webkitExitFullscreen) doc.webkitExitFullscreen()
    } catch { }
  }
  // 2) 如果 Wails 仍在系统级全屏（历史遗留状态）→ 强制退出
  if ((window as any).go) {
    try {
      WindowIsFs().then((fs: boolean) => {
        if (fs) {
          try { WindowSetFullscreen(false) } catch { }
        }
      }).catch(() => { })
    } catch { }
  }
  document.body.removeAttribute('data-player-fullscreen')
  clearOsdTimer()
})

watch(() => props.url, (newUrl, oldUrl) => {
  if (!newUrl) {
    destroyPlayer()
    return
  }
  const restoreFullscreen = Boolean(oldUrl && oldUrl !== newUrl && isFullscreen.value)
  if (oldUrl && oldUrl !== newUrl) {
    const v = getVideoEl()
    if (v) {
      try { v.pause() } catch { /* ignore */ }
      try { v.removeAttribute('src') } catch { /* ignore */ }
      try { v.load() } catch { /* ignore */ }
    }
    preBuffering.value = false
    loading.value = true
    videoReady.value = false
    playing.value = false
    // 字幕文件是对着某一集配的，换集后时间轴基本必然错位，直接清掉比留着误导人好
    clearSubtitle()
  }
  setTimeout(() => {
    setupPlayer()
    if (!restoreFullscreen) return
    setTimeout(async () => {
      if ((window as any).go) {
        try { WindowSetFullscreen(true) } catch { /* ignore */ }
      } else if (!document.fullscreenElement && wrapperRef.value) {
        try {
          const el = wrapperRef.value as any
          const request = el.requestFullscreen || el.webkitRequestFullscreen
          if (request) await request.call(el)
        } catch { /* non-gesture fullscreen may be rejected */ }
      }
      isFullscreen.value = true
      document.body.setAttribute('data-player-fullscreen', '1')
    }, 80)
  }, 0)
})

// 监听 loading 状态：播放中卡顿时自动显示加载统计
watch(loading, (val) => {
  if (val && !preBuffering.value) {
    // 播放中卡顿，显示加载速度
    startLoadingStats()
  }
})

// 暴露 PiP 方法给父组件（Player.vue 快捷键调用）
defineExpose({ togglePiP })
</script>

<template>
  <div class="player-wrapper" :class="{ fullscreen: isFullscreen, 'cursor-hidden': playing && !showControls, 'show-controls': showControls }"
    ref="wrapperRef" tabindex="0" @mousemove="toggleShow(true); mouseInside = true; updateEnhancementCompare($event)" @mouseenter="mouseInside = true"
    @mouseleave="onMouseLeave" @click="onWrapperClick" @dblclick.stop="toggleFullscreen()" @wheel.prevent="onWheel">
    <!-- 顶部栏：标题 + 缓存统计 + 收藏按钮 -->
    <div v-show="showTitleBar && (showControls || !playing)" class="player-title-bar" @click.stop @dblclick.stop>
      <span class="player-title">{{ title || url }}</span>
      <span v-if="isHls(url) && cacheStats.totalSegments > 0" class="cache-info"
        :class="{ working: cacheStats.queued + cacheStats.inflight > 0 }"
        :title="t('player.cacheStatsTip', { hits: cacheStats.hits, misses: cacheStats.misses, entries: cacheStats.entries, target: cacheStats.prefetchTarget })">
        {{ t('player.cachedCountShort', { count: cacheStats.entries }) }}<span v-if="cacheStats.queued + cacheStats.inflight > 0"> · {{ t('player.prefetching') }}</span><span v-else> · {{ t('player.ready') }}</span> ·
        {{ t('player.hitRateShort') }} {{ (cacheStats.hitRate * 100).toFixed(0) }}%
      </span>
      <button class="fav-btn-in-player" :class="{ 'is-fav': isFav }" :disabled="favBusy"
        :aria-label="isFav ? t('detail.removeFav') : t('player.addFavorite')"
        :title="isFav ? t('detail.removeFav') : t('player.addFavorite')" @click.stop="emit('toggleFavorite')">
        {{ isFav ? '★' : '☆' }}
      </button>
    </div>

    <!-- 拖拽遮罩条：鼠标经过视频顶部时出现，可拖拽移动窗口 -->
    <!-- 注意：
         1) 用 v-show 而非 v-if —— 保持元素始终在 DOM 中，避免 Wails 的
            --wails-draggable 命中测试与动态挂载产生竞态（参考 TitleBar.vue 始终渲染）。
         2) 不加 @click.stop / @dblclick.stop —— 这些会干扰 Wails 在 mousedown 阶段
            的拖拽识别（参考 TitleBar.vue 的稳定写法：只设 --wails-draggable: drag）。
         3) z-index 高于 player-title-bar，保证拖拽区不被标题栏遮住；标题栏容器
            pointer-events:none，仅按钮区单独 pointer-events:auto。 -->
    <div v-show="mouseInside" class="player-drag-handle" :title="t('player.dragMoveWindow')" />

    <video class="native-video" playsinline preload="auto" @click.stop="togglePlay"></video>
    <div v-show="activeSubtitle" class="vp-subtitle" :class="{ 'with-controls': showControls }">
      <span>{{ activeSubtitle }}</span>
    </div>
    <div v-if="compareEnabled" class="enhance-compare-line" :style="{ left: compareSplit + '%' }" aria-hidden="true">
      <span>{{ t('player.compareOriginal') }}</span><i></i><span>{{ t('player.compareEnhanced') }}</span>
    </div>

    <!-- 操作 OSD：快进/快退/倍速 屏幕中央提示 -->
    <transition name="osd-fade">
      <div v-show="osdText" class="action-osd">
        <span class="action-osd-icon">{{ osdIcon }}</span>
        <span class="action-osd-text">{{ osdText }}</span>
      </div>
    </transition>

    <div v-if="loading" class="loading">
      <div class="loading-overlay">
        <!-- 加载动画 -->
        <img :src="loadingGif" class="loading-spinner" :alt="t('common.loading')" />
        <!-- 缓冲信息 -->
        <div class="loading-info">
          <div class="loading-text">
            <template v-if="loadingTotal > 0 && preBuffering">{{ t('player.bufferingSegments', { cached: loadingCached, total: loadingTotal }) }}</template>
            <template v-else-if="loadingTotal > 0">{{ t('player.cachedSegments', { cached: loadingCached, total: loadingTotal }) }}</template>
            <template v-else>{{ t('player.connecting') }} {{ loadingElapsed }}s</template>
          </div>
          <div class="loading-speed" v-if="loadingSpeed">{{ loadingSpeed }}</div>
          <div class="loading-bar-wrap" v-if="loadingTotal > 0">
            <div class="loading-bar-fill"
              :style="{ width: (loadingTotal > 0 ? loadingCached / loadingTotal * 100 : 0) + '%' }">
            </div>
          </div>
        </div>
      </div>
    </div>

    <div v-if="errorMsg" class="player-error">
      <span>⚠</span>
      <span>{{ errorMsg }}</span>
      <div v-if="showNetworkError" class="error-actions">
        <button class="error-btn error-btn-primary" @click.stop="retryPlayback()">{{ t('player.retryPlayback') }}</button>
      </div>
    </div>

    <!-- B 站风格：底部左侧继续播放提示（小胶囊，仅在有记忆时出现） -->
    <div v-if="showResumePrompt" class="resume-bili" @click.stop>
      <span class="resume-bili-text">{{ t('player.resumeLocatedAt') }} <b>{{ savedTime != null ? fmt(savedTime) : '00:00' }}</b></span>
      <button class="resume-bili-link" @click.stop="jumpToSavedTime(false)">{{ t('player.jumpBack') }}</button>
      <button class="resume-bili-link" @click.stop="jumpToSavedTime(true)">{{ t('player.jumpBackRemember') }}</button>
      <button class="resume-bili-link resume-bili-dismiss" @click.stop="dismissResumePrompt">
        {{ t('player.playFromStart') }} ({{ resumeRemainSec }}s)
      </button>
    </div>

    <!-- 画质切换 toast（左下角，1.5 秒自动消失） -->
    <div v-show="qualityToastText" class="quality-toast">
      <span>{{ qualityToastText }}</span>
    </div>

    <!-- AI 画质增强提示弹窗 -->
    <div v-if="showAiWarning" class="ai-warning-overlay" @click.stop @wheel.stop>
      <div class="ai-warning-dialog">
        <div class="ai-warning-header">
          <span class="ai-warning-icon">⚡</span>
          <span>{{ t('player.aiEnhanceTitle') }}</span>
        </div>
        <div class="ai-warning-body">
          <p>{{ t('player.aiEnhanceIntro') }}</p>
          <ul>
            <li><b>{{ t('player.animeEnhance') }}</b> — {{ t('player.animeEnhanceDesc') }}</li>
            <li><b>{{ t('player.filmEnhance') }}</b> — {{ t('player.filmEnhanceDesc') }}</li>
          </ul>
          <p>{{ t('player.aiEnhanceNotice') }}</p>
          <ul>
            <li><b>{{ t('player.aiWarnGpuLoad') }}</b> — {{ t('player.aiWarnGpuLoadDesc') }}</li>
            <li><b>{{ t('player.aiWarnBattery') }}</b> — {{ t('player.aiWarnBatteryDesc') }}</li>
            <li><b>{{ t('player.aiWarnPerf') }}</b> — {{ t('player.aiWarnPerfDesc') }}</li>
          </ul>
          <p class="ai-warning-note">{{ t('player.aiEnhanceFallbackNote') }}</p>
        </div>
        <div class="ai-warning-footer">
          <button class="ai-warning-btn ai-warning-btn--cancel" @click="cancelAiMode">{{ t('common.cancel') }}</button>
          <button class="ai-warning-btn ai-warning-btn--confirm" @click="confirmAiMode">{{ t('player.confirmEnable') }}</button>
        </div>
      </div>
    </div>

    <!-- 暂停图标：右下角大字，仅暂停时显示 -->
    <div v-show="!playing && !loading" class="pause-overlay" @click.stop="togglePlay">
      <img :src="pauseImg" class="pause-icon" :alt="t('detail.paused')" />
    </div>

    <!-- 进度条（独立行，在控制条上方） -->
    <!-- ⭐ @mousemove.stop 阻止冒泡到 player-wrapper，避免悬停进度条时触发控制栏和音量弹出 -->
    <div class="progress-bar-wrapper" v-show="showControls || !playing || qualityOpen || showVideoInfo" @click.stop
      @mousedown.stop @dblclick.stop @mousemove.stop>
      <div class="progress-container" ref="progressContainerRef" @mousedown.stop="onProgressMouseDown"
        @mousemove="onProgressHover" @mouseleave="progressHoverPct = -1; thumbPreviewVisible = false">
        <div class="progress-track-bg"></div>
        <div class="progress-buffer" :style="{ width: bufferPct + '%' }"></div>
        <div class="progress-played" :style="{ width: progressPct + '%' }"></div>
        <!-- 悬停预览线 + 时间提示 + 缩略图 -->
        <div v-show="progressHoverPct >= 0" class="progress-hover-line" :style="{ left: progressHoverPct + '%' }"></div>
        <div v-show="progressHoverPct >= 0 && progressHoverTime >= 0" class="progress-time-preview"
          :style="{ left: progressHoverPct + '%' }">
          <canvas v-show="false" ref="thumbCanvasRef"></canvas>
          <img v-if="thumbPreviewVisible && thumbPreviewImg" class="preview-thumb-img" :src="thumbPreviewImg" alt="" />
          <span class="preview-time">{{ fmt(progressHoverTime) }}</span>
        </div>
        <!-- 当前播放位置的“独特”指示点（白色内圆 + 蓝色光晕 + 外圈） -->
        <div class="progress-thumb" :style="{ left: progressPct + '%' }" :class="{ playing: playing }">
          <span class="thumb-halo"></span>
          <span class="thumb-core"></span>
          <span class="thumb-ring"></span>
        </div>
        <input class="progress-slider" type="range" min="0" :max="duration || 0" step="0.1" :value="current"
          @input="seek" />
      </div>
    </div>

    <!-- 底部控制条 -->
    <!-- ⭐ @wheel.stop 阻止 wheel 从控制栏/弹出面板冒泡到 player-wrapper，
         避免音量滑块和弹出面板内的滚动被 onWheel 劫持 -->
    <div class="ctrl-bar" v-show="showControls || !playing || qualityOpen || showVideoInfo" @click.stop @mousedown.stop
      @pointerdown.stop @dblclick.stop @wheel.stop>
      <!-- 上一集 -->
      <button v-if="hasPrev" class="ctrl-btn" @click="emit('prev')" :aria-label="t('player.prev')" :title="t('player.prev')">
        <Icon name="prev" :size="16" />
      </button>

      <!-- 播放/暂停（中间） -->
      <button class="ctrl-btn play-btn" @click="togglePlay"
        :aria-label="playing ? t('player.pause') : t('player.play')" :title="playing ? t('player.pause') : t('player.play')">
        <Icon :name="playing ? 'pause' : 'play'" :size="18" />
      </button>

      <!-- 下一集 -->
      <button v-if="hasNext" class="ctrl-btn" @click="emit('next')" :aria-label="t('player.next')" :title="t('player.next')">
        <Icon name="next" :size="16" />
      </button>

      <!-- B站风格时间显示 / 跳转 -->
      <div class="time-display" @click.stop>
        <template v-if="showTimeInput">
          <input ref="timeInputRef" class="time-input" v-model="timeInputValue" @keydown.enter.prevent="jumpToTime"
            @keydown.escape.prevent="cancelTimeInput" @blur="jumpToTime" @click.stop @mousedown.stop />
        </template>
        <template v-else>
          <div class="time-bar" @click="toggleTimeInput" :title="t('player.seekTimeTip')">
            <span class="time-current">{{ fmt(current) }}</span>
            <span class="time-sep">/</span>
            <span class="time-duration">{{ fmt(duration) }}</span>
          </div>
        </template>
      </div>

      <!-- 画质选择。
           inline + inline-drop="up"：面板不 Teleport，留在控制条 DOM 树内，
           (1) 解决全屏下面板 fixed 定位 rect 归零打不开；
           (2) 解决 Teleport 后父级 scoped 的 :deep 深色样式失效（与播放页不协调）。
           qualityOpen 状态在面板打开期间锁定控制条可见，避免 2.5s 自动隐藏导致面板错位。 -->
      <div class="quality-group" @click.stop style="margin-left: auto">
        <SelectDropdown :model-value="qualityDropdownValue" :options="qualityOptions" size="sm" inline inline-drop="up"
          @change="onQualityChange" @open-change="(v: boolean) => qualityOpen = v" />
      </div>

      <!-- 音量 + 垂直滑块弹出（纯 CSS hover；鼠标从图标移动到滑块不会消失） -->
      <div class="volume-group" @click.stop @mouseenter="showVolumePanel = true" @mouseleave="showVolumePanel = false">
        <button class="ctrl-btn" @click.stop="toggleMute(); keepVisible()"
          :aria-label="muted ? t('player.unmute') : t('player.muteWithHotkey')" :title="muted ? t('player.unmute') : t('player.muteWithHotkey')">
          <Icon :name="muted ? 'volume-off' : 'volume'" :size="16" />
        </button>
        <div class="volume-popup" :class="{ show: showVolumePanel }" @click.stop>
          <div class="volume-slider-wrap">
            <input class="volume-slider-v" type="range" min="0" max="1" step="0.05" :value="muted ? 0 : volume"
              @input="onVolumeInput" @change="keepVisible()" />
          </div>
          <span class="volume-label">{{ Math.round((muted ? 0 : volume) * 100) }}</span>
        </div>
      </div>

      <!-- 倍速按钮 + 弹出垂直列表（hover 显示，点击切换） -->
      <div class="speed-group" @click.stop>
        <button class="ctrl-btn speed-btn"
          @click="showSpeedPanel = !showSpeedPanel; showVolumePanel = false; keepVisible()" :title="t('player.playbackSpeed')">
          <span class="speed-text">{{ speed }}x</span>
          <Icon name="chevron-down" :size="10" />
        </button>
        <div class="speed-popup" :class="{ show: showSpeedPanel }">
          <button v-for="s in speedOptions" :key="s" class="speed-item" :class="{ active: speed === s }"
            @click="changeSpeed(s)">
            {{ s }}x
            <Icon v-if="speed === s" name="check" :size="12" />
          </button>
        </div>
      </div>

      <button v-if="qualityMode !== 'original' && hasPipeline()" class="ctrl-btn" :class="{ active: compareEnabled }"
        @click.stop="toggleEnhancementCompare" :aria-label="t('player.compareToggleTip')" :title="t('player.compareToggleTip')">
        <Icon name="layers" :size="16" />
      </button>

      <!-- 播放设置 -->
      <div class="playback-settings-group" @click.stop>
        <button class="ctrl-btn" @click="showPlaybackSettings = !showPlaybackSettings; keepVisible()" :aria-label="t('settings.playback')" :title="t('settings.playback')">
          <Icon name="settings" :size="16" />
        </button>
        <div class="playback-settings-popup" :class="{ show: showPlaybackSettings }" @click.stop>
          <div class="playback-settings-item">
            <span>{{ t('player.autoPlay') }}</span>
            <label class="ps-toggle">
              <input type="checkbox" :checked="props.autoplay" @change="emit('toggleAutoplay')" />
              <span class="ps-switch"></span>
            </label>
          </div>
          <div class="playback-settings-item">
            <span>{{ t('player.autoNext') }}</span>
            <label class="ps-toggle">
              <input type="checkbox" :checked="autoNextEnabled" @change="toggleAutoNext" />
              <span class="ps-switch"></span>
            </label>
          </div>
          <div class="playback-settings-divider"></div>
          <button class="playback-settings-link" @click.stop="showPlaybackSettings = false; showShortcutModal = true">
            <Icon name="keyboard" :size="14" />
            <span>{{ t('player.shortcutSettings') }}</span>
            <Icon name="chevron-right" :size="12" />
          </button>
        </div>
      </div>
      <!-- 外挂字幕 -->
      <div class="subtitle-group" @click.stop>
        <button class="ctrl-btn" :class="{ active: subtitleCues.length > 0 }"
          @click.stop="showSubtitlePanel = !showSubtitlePanel; keepVisible()" :aria-label="t('player.subtitle')" :title="t('player.subtitle')">
          <Icon name="subtitles" :size="16" />
        </button>
        <div class="subtitle-popup" :class="{ show: showSubtitlePanel }" @click.stop>
          <div class="subtitle-title">{{ t('player.subtitleTitle') }}</div>
          <div v-if="subtitleName" class="subtitle-current">
            <span class="subtitle-name" :title="subtitleName">{{ subtitleName }}</span>
            <span class="subtitle-count">{{ t('player.subtitleCount', { count: subtitleCues.length }) }}</span>
          </div>
          <div v-if="subtitleError" class="subtitle-error">{{ subtitleError }}</div>
          <button class="subtitle-item" @click.stop="openSubtitlePicker()">
            <Icon name="folder" :size="13" />
            <span>{{ subtitleCues.length > 0 ? t('player.subtitleReplace') : t('player.subtitleLoad') }}</span>
          </button>
          <button v-if="subtitleCues.length > 0" class="subtitle-item"
            @click.stop="subtitleVisible = !subtitleVisible; keepVisible()">
            <Icon name="subtitles" :size="13" />
            <span>{{ subtitleVisible ? t('player.subtitleHide') : t('player.subtitleShow') }}</span>
          </button>
          <button v-if="subtitleCues.length > 0" class="subtitle-item subtitle-item--danger"
            @click.stop="clearSubtitle(); keepVisible()">
            <Icon name="trash" :size="13" />
            <span>{{ t('player.subtitleRemove') }}</span>
          </button>
          <div class="subtitle-hint">{{ t('player.subtitleHint') }}</div>
        </div>
      </div>
      <input ref="subtitleFileRef" type="file" accept=".srt,.vtt,text/plain" class="subtitle-file-input"
        @change="onSubtitleFileChosen" />
      <!-- 报告广告 -->
      <div class="report-ad-group" @click.stop>
        <button class="ctrl-btn" @click.stop="toggleReportAd(); keepVisible()" :aria-label="t('player.reportAdTip')" :title="t('player.reportAdTip')">
          <Icon name="flag" :size="16" />
        </button>
        <div class="report-ad-popup" :class="{ show: showReportAd }" @click.stop>
          <div class="report-ad-title">{{ t('player.reportAdHint') }}</div>
          <div v-if="reportAdDomains.length === 0" class="report-ad-empty">{{ t('player.reportAdEmpty') }}</div>
          <button v-for="d in reportAdDomains" :key="d" class="report-ad-item" @click.stop="doReportAd(d)">
            <Icon name="shield" :size="13" />
            <span class="report-ad-domain">{{ d }}</span>
          </button>
        </div>
      </div>
      <!-- 视频信息 -->
      <div class="video-info-group" @click.stop>
        <button class="ctrl-btn" @click.stop="showVideoInfo = !showVideoInfo; keepVisible()" :aria-label="t('player.videoInfo')" :title="t('player.videoInfo')">
          <Icon name="info" :size="16" />
        </button>
      </div>
      <!-- 豆瓣评论（仅有豆瓣ID时显示） -->
      <button v-if="doubanId" class="ctrl-btn" @click.stop="emit('showComments'); keepVisible()" :aria-label="t('player.doubanComments')" :title="t('player.doubanComments')">
        <Icon name="comment" :size="16" />
      </button>
      <!-- 画中画 -->
      <button class="ctrl-btn" @click="togglePiP" :aria-label="isPiP ? t('player.exitPip') : t('player.pipWithHotkey')" :title="isPiP ? t('player.exitPip') : t('player.pipWithHotkey')" :class="{ active: isPiP }">
        <Icon :name="isPiP ? 'pip-exit' : 'pip'" :size="16" />
      </button>
      <!-- 全屏 -->
      <button class="ctrl-btn" @click="toggleFullscreen" :aria-label="isFullscreen ? t('player.exitFullscreen') : t('player.fullscreenHotkey')" :title="isFullscreen ? t('player.exitFullscreen') : t('player.fullscreenHotkey')">
        <Icon :name="isFullscreen ? 'exit-fullscreen' : 'fullscreen'" :size="16" />
      </button>
    </div>

    <!-- 报告广告成功 toast -->
    <MotionTransition preset="fade">
      <div v-show="reportAdToast" class="report-ad-toast">
        <Icon name="check" :size="14" />
        <span>{{ reportAdToast }}</span>
      </div>
    </MotionTransition>

    <!-- ====== 视频信息模态框 ====== -->
    <MotionTransition preset="fade">
      <div v-if="showVideoInfo" class="vp-modal-overlay" @click="showVideoInfo = false" @keydown="onShortcutKeyDown"
        @wheel.stop>
        <MotionTransition preset="dialog" appear>
          <div v-if="showVideoInfo" class="vp-modal vp-modal-info" @click.stop>
          <div class="vp-modal-header">
            <span>{{ t('player.videoInfo') }}</span>
            <button class="vp-modal-close" @click="showVideoInfo = false" :aria-label="t('common.close')">
              <Icon name="x" :size="16" />
            </button>
          </div>
          <div class="vp-modal-body vp-modal-scroll" v-if="videoInfo">
            <!-- 基本播放信息 -->
            <div class="vp-info-section-title">{{ t('player.playbackInfo') }}</div>
            <div class="vp-info-grid">
              <div class="vp-info-item">
                <span class="vp-info-label">{{ t('player.durationLabel') }}</span>
                <span class="vp-info-val">{{ videoInfo.duration }}</span>
              </div>
              <div class="vp-info-item">
                <span class="vp-info-label">{{ t('player.currentTimeLabel') }}</span>
                <span class="vp-info-val">{{ videoInfo.current }} ({{ videoInfo.progress }})</span>
              </div>
              <div class="vp-info-item">
                <span class="vp-info-label">{{ t('player.playbackSpeed') }}</span>
                <span class="vp-info-val">{{ videoInfo.speed }}</span>
              </div>
              <div class="vp-info-item">
                <span class="vp-info-label">{{ t('player.volume') }}</span>
                <span class="vp-info-val">{{ videoInfo.volume }}</span>
              </div>
              <div v-if="videoInfo.buffered" class="vp-info-item">
                <span class="vp-info-label">{{ t('player.bufferProgress') }}</span>
                <span class="vp-info-val">{{ videoInfo.buffered }}</span>
              </div>
            </div>
            <!-- 视频流信息 -->
            <div class="vp-info-section-title">{{ t('player.videoStream') }}</div>
            <div class="vp-info-grid">
              <div v-if="videoInfo.resolution" class="vp-info-item">
                <span class="vp-info-label">{{ t('player.resolution') }}</span>
                <span class="vp-info-val">{{ videoInfo.resolution }}</span>
              </div>
              <div v-if="videoInfo.fps" class="vp-info-item">
                <span class="vp-info-label">{{ t('player.frameRate') }}</span>
                <span class="vp-info-val">{{ videoInfo.fps }}</span>
              </div>
              <div v-if="videoInfo.droppedFrames > 0" class="vp-info-item">
                <span class="vp-info-label">{{ t('player.droppedFrames') }}</span>
                <span class="vp-info-val vp-info-warn">{{ videoInfo.droppedFrames }}</span>
              </div>
              <div v-if="videoInfo.streamBitrate" class="vp-info-item">
                <span class="vp-info-label">{{ t('player.sourceBitrate') }}</span>
                <span class="vp-info-val vp-info-highlight">{{ videoInfo.streamBitrate }}</span>
              </div>
              <div v-if="videoInfo.currentBitrate" class="vp-info-item">
                <span class="vp-info-label">{{ t('player.currentBitrate') }}</span>
                <span class="vp-info-val">{{ videoInfo.currentBitrate }}</span>
              </div>
              <div v-if="videoInfo.codec" class="vp-info-item">
                <span class="vp-info-label">{{ t('player.codecFormat') }}</span>
                <span class="vp-info-val">{{ videoInfo.codec }}</span>
              </div>
              <div v-if="videoInfo.hlsLevelInfo" class="vp-info-item">
                <span class="vp-info-label">{{ t('player.hlsLevel') }}</span>
                <span class="vp-info-val">{{ videoInfo.hlsLevelInfo }}</span>
              </div>
            </div>
            <!-- 缓存 & 网络 -->
            <div class="vp-info-section-title">{{ t('player.cacheAndNetwork') }}</div>
            <div class="vp-info-grid">
              <div class="vp-info-item vp-info-full">
                <span class="vp-info-label">{{ t('player.cacheStatus') }}</span>
                <span class="vp-info-val">{{ videoInfo.cacheInfo }}</span>
              </div>
              <div class="vp-info-item">
                <span class="vp-info-label">{{ t('player.cacheModeLabel') }}</span>
                <span class="vp-info-val">{{ videoInfo.cacheMode }}</span>
              </div>
              <div v-if="videoInfo.networkMode" class="vp-info-item">
                <span class="vp-info-label">{{ t('player.networkModeLabel') }}</span>
                <span class="vp-info-val">{{ videoInfo.networkMode }}</span>
              </div>
            </div>
            <!-- 增强 & 源 -->
            <div class="vp-info-section-title">{{ t('player.enhanceAndSource') }}</div>
            <div class="vp-info-grid">
              <div class="vp-info-item">
                <span class="vp-info-label">{{ t('player.qualityModeLabel') }}</span>
                <span class="vp-info-val">{{ videoInfo.qualityMode }}</span>
              </div>
              <div v-if="videoInfo.sourceHost" class="vp-info-item vp-info-full">
                <span class="vp-info-label">{{ t('player.sourceHost') }}</span>
                <span class="vp-info-val vp-info-mono">{{ videoInfo.sourceHost }}</span>
              </div>
              <div v-if="videoInfo.sourceUrl" class="vp-info-item vp-info-full">
                <span class="vp-info-label">{{ t('player.sourceUrlLabel') }}</span>
                <span class="vp-info-val vp-info-mono vp-info-url">{{ videoInfo.sourceUrl }}</span>
              </div>
            </div>
          </div>
          <div class="vp-modal-body" v-else>
            <p class="vp-empty-text">{{ t('player.noVideoInfo') }}</p>
          </div>
          </div>
        </MotionTransition>
      </div>
    </MotionTransition>

    <!-- ====== 快捷键设置模态框 ====== -->
    <MotionTransition preset="fade">
      <div v-if="showShortcutModal" class="vp-modal-overlay" @click="showShortcutModal = false"
        @keydown="onShortcutKeyDown" @wheel.stop>
        <MotionTransition preset="dialog" appear>
          <div v-if="showShortcutModal" class="vp-modal vp-modal-shortcut" @click.stop>
          <div class="vp-modal-header">
            <span>{{ t('player.shortcutSettings') }}</span>
            <button class="vp-modal-close" @click="showShortcutModal = false" :aria-label="t('common.close')">
              <Icon name="x" :size="16" />
            </button>
          </div>
          <div class="vp-modal-body vp-modal-scroll">
            <p class="vp-modal-hint">{{ t('player.shortcutHint') }}</p>
            <div class="vp-shortcut-list">
              <div v-for="action in SHORTCUT_ACTIONS" :key="action.id" class="vp-shortcut-row">
                <span class="vp-sc-label">{{ t(action.labelKey) }}</span>
                <span class="vp-sc-desc">{{ t(action.descriptionKey) }}</span>
                <div class="vp-sc-actions">
                  <button class="vp-sc-btn" :class="{ editing: editingShortcutId === action.id }"
                    @click.stop="startEditShortcut(action.id)">
                    <template v-if="editingShortcutId === action.id">
                      <span class="vp-sc-recording">{{ t('player.pressNewKey') }}</span>
                    </template>
                    <template v-else>
                      <span v-for="(k, ki) in (shortcutMap[action.id] || [])" :key="ki" class="vp-sc-key">{{ fmtKey(k)
                      }}</span>
                      <span v-if="!shortcutMap[action.id] || shortcutMap[action.id].length === 0"
                        class="vp-sc-none">{{ t('player.notSet') }}</span>
                    </template>
                  </button>
                  <button class="vp-sc-reset" @click.stop="resetShortcut(action.id)" :aria-label="t('player.restoreDefault')" :title="t('player.restoreDefault')">
                    <Icon name="reset" :size="12" />
                  </button>
                </div>
              </div>
            </div>
          </div>
          <div class="vp-modal-footer">
            <button class="vp-btn-secondary" @click="resetAllShortcuts">{{ t('player.resetAll') }}</button>
            <button class="vp-btn-primary" @click="showShortcutModal = false">{{ t('common.done') }}</button>
          </div>
          </div>
        </MotionTransition>
      </div>
    </MotionTransition>
  </div>
</template>

<style scoped src="../styles/components/player.css"></style>
