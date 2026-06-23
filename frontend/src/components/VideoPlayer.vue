<script setup lang="ts">
import { ref, nextTick, onMounted, onBeforeUnmount, watch, computed } from 'vue'
import Icon from './Icon.vue'
import { Select as SelectDropdown } from './ui'
import { TsCache } from '../utils/tsCache'
import { FilmUpscaler, FILM_PRESET, checkFilmSupport } from '../utils/filmUpscaler'
import { Anime4kUpscaler, ANIME4K_PRESET, checkAnime4kSupport } from '../utils/anime4kUpscaler'
import type { Anime4kTier } from '../utils/anime4kUpscaler'
import loadingGif from '../assets/videos/loading.gif'
import pauseImg from '../assets/images/pause.png'
import {
  WindowIsFs, WindowIsMax, WindowSetFullscreen, WindowToggleMax
} from '../../bindings/cczjVideo/app'

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
}>(), {
  autoplay: true,
  hasPrev: false,
  hasNext: false,
  videoKey: '',
  showTitleBar: true,
  title: '',
  isFav: false,
  favBusy: false,
})

const emit = defineEmits(['back', 'prev', 'next', 'toggleFavorite', 'toggleAutoplay'])

const wrapperRef = ref<HTMLDivElement>()
const errorMsg = ref('')
let networkErrTimer: ReturnType<typeof setTimeout> | null = null
const showNetworkError = ref(false)
function clearNetworkErrTimer(): void {
  if (networkErrTimer) { clearTimeout(networkErrTimer); networkErrTimer = null }
  showNetworkError.value = false
}
function refreshPage(): void { location.reload() }

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
      loadingSpeed.value = '连接中...'
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
interface ShortcutAction { id: string; label: string; description: string; defaultKeys: string[] }
const SHORTCUT_ACTIONS: ShortcutAction[] = [
  { id: 'togglePlay', label: '播放/暂停', description: '切换播放和暂停状态', defaultKeys: ['Space', 'K'] },
  { id: 'seekBack', label: '快退 5s', description: '后退 5 秒', defaultKeys: ['ArrowLeft'] },
  { id: 'seekForward', label: '快进 5s', description: '前进 5 秒', defaultKeys: ['ArrowRight'] },
  { id: 'seekBackBig', label: '快退 30s', description: '后退 30 秒', defaultKeys: ['J'] },
  { id: 'seekForwardBig', label: '快进 30s', description: '前进 30 秒', defaultKeys: ['L'] },
  { id: 'volumeUp', label: '音量+', description: '音量增加 5%', defaultKeys: ['ArrowUp'] },
  { id: 'volumeDown', label: '音量-', description: '音量减少 5%', defaultKeys: ['ArrowDown'] },
  { id: 'mute', label: '静音', description: '切换静音', defaultKeys: ['M'] },
  { id: 'speedUp', label: '加速', description: '播放速度 +0.25x', defaultKeys: [']'] },
  { id: 'speedDown', label: '减速', description: '播放速度 -0.25x', defaultKeys: ['['] },
  { id: 'fullscreen', label: '全屏', description: '切换全屏', defaultKeys: ['F'] },
  { id: 'prevEp', label: '上一集', description: '切换到上一集', defaultKeys: ['P'] },
  { id: 'nextEp', label: '下一集', description: '切换到下一集', defaultKeys: ['N'] },
  { id: 'pip', label: '画中画', description: '切换画中画模式', defaultKeys: ['I'] },
]
const shortcutMap = ref<Record<string, string[]>>({})
const editingShortcutId = ref<string | null>(null)

function loadShortcuts(): void {
  try {
    const raw = localStorage.getItem('cczj_shortcuts')
    if (raw) {
      const parsed = JSON.parse(raw)
      if (parsed && typeof parsed === 'object') {
        shortcutMap.value = parsed
        return
      }
    }
  } catch { }
  const defaults: Record<string, string[]> = {}
  SHORTCUT_ACTIONS.forEach(a => { defaults[a.id] = [...a.defaultKeys] })
  shortcutMap.value = defaults
}
function saveShortcuts(): void {
  try { localStorage.setItem('cczj_shortcuts', JSON.stringify(shortcutMap.value)) } catch { }
}
function startEditShortcut(id: string): void {
  editingShortcutId.value = id
}
function cancelEditShortcut(): void {
  editingShortcutId.value = null
}
function onShortcutKeyDown(e: KeyboardEvent): void {
  if (!editingShortcutId.value) return
  e.preventDefault()
  e.stopPropagation()
  if (e.key === 'Escape') { cancelEditShortcut(); return }
  let key = e.key
  if (key === ' ') key = 'Space'
  shortcutMap.value[editingShortcutId.value] = [key]
  saveShortcuts()
  editingShortcutId.value = null
}
function resetShortcut(id: string): void {
  const action = SHORTCUT_ACTIONS.find(a => a.id === id)
  if (!action) return
  shortcutMap.value[id] = [...action.defaultKeys]
  saveShortcuts()
}
function resetAllShortcuts(): void {
  const defaults: Record<string, string[]> = {}
  SHORTCUT_ACTIONS.forEach(a => { defaults[a.id] = [...a.defaultKeys] })
  shortcutMap.value = defaults
  saveShortcuts()
}
function fmtKey(k: string): string {
  if (k === 'Space') return '空格'
  if (k === 'ArrowLeft') return '←'
  if (k === 'ArrowRight') return '→'
  if (k === 'ArrowUp') return '↑'
  if (k === 'ArrowDown') return '↓'
  if (k === '[') return '['
  if (k === ']') return ']'
  if (k === 'Escape') return 'Esc'
  if (k === 'Enter') return 'Enter'
  if (k === 'Tab') return 'Tab'
  if (k.length === 1) return k.toUpperCase()
  return k
}

// 初始化快捷键
loadShortcuts()

function toggleAutoNext(): void {
  autoNextEnabled.value = !autoNextEnabled.value
  try { localStorage.setItem('cczj_auto_next', autoNextEnabled.value ? '1' : '0') } catch { /* ignore */ }
}

// 初始化自动连播设置
try {
  const saved = localStorage.getItem('cczj_auto_next')
  if (saved === '0') autoNextEnabled.value = false
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
    reportAdToast.value = `已加入黑名单: ${domain}`
  } else {
    reportAdToast.value = `域名已在黑名单中`
  }
  if (_reportAdToastTimer != null) clearTimeout(_reportAdToastTimer)
  _reportAdToastTimer = window.setTimeout(() => { reportAdToast.value = '' }, 3000)
}
// 画质下拉框是否展开 —— 展开期间锁定控制条可见，避免全屏下 2.5s 自动隐藏导致面板错位
const qualityOpen = ref(false)

// ⭐ 播放器设置统一存储在单个 JSON 对象中（key: 'vp_settings'），避免 localStorage 碎片化。
// 旧版散落的 vp_* 键会在首次读取时自动迁移并清理。
const VP_SETTINGS_KEY = 'vp_settings'
const LEGACY_KEYS = [
  'vp_quality_mode', 'vp_anime4k_tier', 'vp_ai_warning_accepted',
  'vp_auto_resume_jump', 'vp_volume', 'vp_muted', 'vp_speed',
  'vp_auto_next',
]
let _settingsCache: Record<string, string> | null = null

function _loadSettings(): Record<string, string> {
  if (_settingsCache) return _settingsCache
  try {
    const raw = localStorage.getItem(VP_SETTINGS_KEY)
    if (raw) {
      const parsed = JSON.parse(raw)
      if (parsed && typeof parsed === 'object') {
        _settingsCache = parsed
        return _settingsCache ? _settingsCache : {}
      }
    }
  } catch { /* ignore */ }
  const migrated: Record<string, string> = {}
  for (const oldKey of LEGACY_KEYS) {
    const val = localStorage.getItem(oldKey)
    if (val != null) {
      migrated[oldKey.replace(/^vp_/, '')] = val
      localStorage.removeItem(oldKey)
    }
  }
  _settingsCache = migrated
  if (Object.keys(migrated).length > 0) {
    try { localStorage.setItem(VP_SETTINGS_KEY, JSON.stringify(migrated)) } catch { /* ignore */ }
  }
  return _settingsCache
}

function _saveSettings(): void {
  if (!_settingsCache) return
  try { localStorage.setItem(VP_SETTINGS_KEY, JSON.stringify(_settingsCache)) } catch { /* ignore */ }
}

function readSetting(key: string, def: string): string {
  const s = _loadSettings()
  return s[key] ?? def
}
function writeSetting(key: string, val: string): void {
  const s = _loadSettings()
  if (s[key] === val) return
  s[key] = val
  _saveSettings()
}

// ========= 画质模式 =========
// 模式：原高清 / 动画增强 M·L·VL / 影视增强（M/L/VL 直接在画质下拉框中选择）
// 兼容旧版 localStorage 中存的 'ai_frame_interp' 和 'ai_enhance' 值。
type QualityMode = 'original' | 'ai_anime' | 'ai_film'
const qualityMode = ref<QualityMode>(normalizeQualityMode(readSetting('quality_mode', 'original')))

const anime4kTier = ref<Anime4kTier>(
  (readSetting('anime4k_tier', 'M') as Anime4kTier) || 'M'
)

/** 根据视频分辨率推荐最佳档位 */
const recommendedTier = computed<Anime4kTier>(() => {
  const h = (getVideoEl()?.videoHeight) || 0
  if (h <= 0) return 'M'        // 元数据未就绪，默认 M
  if (h >= 1080) return 'S'
  if (h >= 720) return 'M'
  return 'L'
})

// 画质下拉框（原高清 + 动画增强三档(含推荐) + 影视增强）
const qualityOptions = computed(() => {
  const rec = recommendedTier.value
  const tiers: Anime4kTier[] = ['S', 'M', 'L']
  const animeOptions = tiers.map(t => ({
    value: `ai_anime_${t}`,
    label: `动画增强 ${t}${t === rec ? ' (推荐)' : ''}`,
  }))
  return [
    { value: 'original', label: '原高清' },
    ...animeOptions,
    { value: 'ai_film', label: '影视增强' },
  ]
})
// 当前下拉框选中值（根据 qualityMode + anime4kTier 计算）
const qualityDropdownValue = computed(() => {
  if (qualityMode.value === 'ai_anime') return `ai_anime_${anime4kTier.value}`
  return qualityMode.value
})
function normalizeQualityMode(v: string): QualityMode {
  if (v === 'ai_frame_interp' || v === 'ai_enhance') return 'ai_anime' // 旧版统一迁移
  if (v === 'ai_anime' || v === 'ai_film') return v
  return 'original'
}
function isAiMode(mode: string): mode is 'ai_anime' | 'ai_film' {
  return mode === 'ai_anime' || mode === 'ai_film'
}
const showAiWarning = ref(false)
const aiWarningAccepted = ref(readSetting('ai_warning_accepted', '0') === '1')

// 切换画质时的短暂提示（左下角）
const qualityToastText = ref('')
let qualityToastTimer: ReturnType<typeof setTimeout> | null = null
function showQualityToast(text: string): void {
  qualityToastText.value = text
  if (qualityToastTimer) clearTimeout(qualityToastTimer)
  qualityToastTimer = setTimeout(() => { qualityToastText.value = '' }, 1500)
}

// 画质增强管线（WebGL2 实时增强：锐化/对比度/边缘/去色带）
let upscaler: Anime4kUpscaler | FilmUpscaler | null = null
let upscalerStatsTimer: ReturnType<typeof setInterval> | null = null
const upscalerSupported = ref(false)
const upscalerStats = ref<{ fps: number; gpuEnabled: boolean }>({ fps: 0, gpuEnabled: false })
let _aiReady = false // 视频是否已就绪（loadedmetadata 之后），AI 才会启动

let _pendingQualityMode: QualityMode = 'original'
function onQualityChange(value: string | number): void {
  const raw = String(value)
  // 解析合并选项值：ai_anime_M / ai_anime_L / ai_anime_VL / ai_film / original
  let mode: QualityMode
  if (raw.startsWith('ai_anime_')) {
    mode = 'ai_anime'
    const tier = raw.slice('ai_anime_'.length) as Anime4kTier
    anime4kTier.value = tier
    writeSetting('anime4k_tier', tier)
  } else {
    mode = raw as QualityMode
  }
  if (isAiMode(mode) && !aiWarningAccepted.value) {
    _pendingQualityMode = mode
    showAiWarning.value = true
    return
  }
  applyQualityMode(mode)
}

function applyQualityMode(mode: QualityMode): void {
  qualityMode.value = mode
  writeSetting('quality_mode', mode)
  if (isAiMode(mode)) {
    if (_aiReady) {
      startAiPipeline(mode)
    }
    const tierLabel = mode === 'ai_anime' ? `动画增强 ${anime4kTier.value}` : '影视增强'
    showQualityToast(`已切换至${tierLabel}（GPU 实时增强）`)
  } else {
    stopAiPipeline()
    showQualityToast('已切换至原高清')
  }
}

function confirmAiMode(): void {
  aiWarningAccepted.value = true
  writeSetting('ai_warning_accepted', '1')
  showAiWarning.value = false
  applyQualityMode(_pendingQualityMode)
}

function cancelAiMode(): void {
  showAiWarning.value = false
  qualityMode.value = 'original'
  writeSetting('quality_mode', 'original')
}

// AI 增强管线：动画模式用 Anime4K CNN 超分，影视模式用 FSRCNNX + CAS
async function startAiPipeline(mode: 'ai_anime' | 'ai_film'): Promise<void> {
  // 先清除旧的统计定时器（避免切换模式时泄漏）
  if (upscalerStatsTimer) {
    clearInterval(upscalerStatsTimer)
    upscalerStatsTimer = null
  }
  // 先销毁旧实例（切换模式时）
  if (upscaler) {
    upscaler.stop()
    upscaler.destroy()
    upscaler = null
  }

  const v = getVideoEl()
  if (!v) return

  // 动画模式：Anime4K CNN 超分
  if (mode === 'ai_anime') {
    const a4kSupport = checkAnime4kSupport()
    if (a4kSupport.recommended) {
      upscaler = new Anime4kUpscaler({ ...ANIME4K_PRESET, tier: anime4kTier.value })
      const ok = await upscaler.init(v, wrapperRef.value ?? undefined)
      if (ok) {
        upscaler.start()
        console.log(`[Player] Anime4K CNN 2x 超分管线已启动 (${anime4kTier.value} 档, WebGL2)`)
        upscalerStatsTimer = setInterval(() => {
          if (!upscaler) { if (upscalerStatsTimer) { clearInterval(upscalerStatsTimer); upscalerStatsTimer = null }; return }
          const s = upscaler.getStats()
          upscalerStats.value = { fps: s.fps, gpuEnabled: s.gpuEnabled }
        }, 2000)
        return
      }
      console.warn('[Player] Anime4K 初始化失败:', upscaler.error)
      upscaler.destroy()
      upscaler = null
    } else {
      console.warn('[Player] Anime4K 不可用:', a4kSupport.message)
    }
    // Anime4K 不可用或初始化失败 → 回退到原高清
    qualityMode.value = 'original'
    writeSetting('quality_mode', 'original')
    return
  }

  // 影视模式：FSRCNNX + CAS
  const filmSupport = checkFilmSupport()
  upscalerSupported.value = filmSupport.supported

  if (!filmSupport.supported) {
    console.warn('[Player] 影视增强不可用:', filmSupport.message)
    qualityMode.value = 'original'
    writeSetting('quality_mode', 'original')
    return
  }

  upscaler = new FilmUpscaler({ ...FILM_PRESET })

  const ok = await upscaler.init(v, wrapperRef.value ?? undefined)
  if (!ok) {
    console.error('[Player] FSRCNNX 影视增强初始化失败:', upscaler.error)
    upscaler.destroy()
    upscaler = null
    qualityMode.value = 'original'
    writeSetting('quality_mode', 'original')
    return
  }

  upscaler.start()
  console.log('[Player] FSRCNNX + CAS 影视增强管线已启动 (WebGL2 多 Pass GPU 加速)')

  // 定期更新性能统计
  upscalerStatsTimer = setInterval(() => {
    if (!upscaler) {
      if (upscalerStatsTimer) clearInterval(upscalerStatsTimer)
      upscalerStatsTimer = null
      return
    }
    const s = upscaler.getStats()
    upscalerStats.value = { fps: s.fps, gpuEnabled: s.gpuEnabled }
  }, 2000)
}

function stopAiPipeline(): void {
  if (upscalerStatsTimer) {
    clearInterval(upscalerStatsTimer)
    upscalerStatsTimer = null
  }
  if (upscaler) {
    upscaler.stop()
    upscaler.destroy()
    upscaler = null
  }
  upscalerStats.value = { fps: 0, gpuEnabled: false }
  console.log('[Player] AI 增强管线已停止')
}

// ========= 播放进度记录 =========
// 设置：autoResume = true 时直接跳到上次位置；false 时弹出 5 秒提示
const SAVE_INTERVAL_MS = 3000
const RESUME_THRESHOLD_SEC = 5  // 已播放超过 5 秒才记录
const PROMPT_SEC = 5           // 提示存在时间
let _saveTimer: number | null = null

const savedTime = ref<number | null>(null)
const showResumePrompt = ref(false)
const resumeRemainSec = ref(PROMPT_SEC)
let _resumeTimer: number | null = null
let _resumeAutoJump = false  // 从 localStorage 读配置：是否自动跳

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
  if (!v || !v.duration || v.duration <= 0) return
  const buf = v.buffered
  if (!buf || buf.length === 0) {
    bufferPct.value = 0
    return
  }
  // 用最后一段 buffer 的结尾来表示"已缓冲到的最远位置"
  const end = buf.end(buf.length - 1)
  bufferPct.value = Math.max(0, Math.min(100, (end / v.duration) * 100))
}

function readLocalTime(videoKey: string): number {
  try {
    const s = localStorage.getItem('vp_t_' + videoKey)
    const n = s ? parseFloat(s) : 0
    return isFinite(n) ? n : 0
  } catch { return 0 }
}
function writeLocalTime(videoKey: string, t: number): void {
  try { localStorage.setItem('vp_t_' + videoKey, String(t)) } catch { /* ignore */ }
}

function saveResumeTime(videoKey: string, t: number, dur: number): void {
  if (!videoKey) return
  if (t <= RESUME_THRESHOLD_SEC) return
  if (dur > 0 && t >= dur - 1) return // 已播到结尾，不保存
  writeLocalTime(videoKey, t)
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
  totalSegments: number; prefetched: number;
}>({
  hits: 0, misses: 0, entries: 0, bytes: 0, hitRate: 0, totalSegments: 0, prefetched: 0,
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
      if (fpsNum >= 15 && fpsNum <= 120) fps = fpsNum + ' fps (流畅度 ' + rate + '%)'
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
      if (bufferedSec > 0) buffered += ` (${fmt(bufferedSec)} 可用)`
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
  const cacheInfo = `${cs.entries}/${cs.totalSegments} 片段 · ${formatBytes(cs.bytes)} · 命中率 ${(cs.hitRate * 100).toFixed(0)}%`
  const cacheMode = cs.totalSegments > 0 ? 'TsCache 已激活' : '未激活'

  // 网络模式
  let networkMode = ''
  try {
    networkMode = (TsCache as any).getNetworkMode?.() || ''
  } catch { }

  // 画质模式
  const qm = qualityMode.value === 'ai_anime' ? `动画增强 ${anime4kTier.value}` : qualityMode.value === 'ai_film' ? '影视增强' : '原高清'
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
  }
  if (changed && (s.hits + s.misses) > 0) {
    console.log(
      `[TsCache] 命中=${s.hits} 未命中=${s.misses} 命中率=${(s.hitRate * 100).toFixed(0)}% ` +
      `已缓存 ${s.entries} 片 / ${formatBytes(s.bytes)}`
    )
  }
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
          errorMsg.value = '视频格式不支持，无法播放'
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
async function loadHls(video: HTMLVideoElement, url: string): Promise<void> {
  console.log('[Player] 🔄 开始加载视频:', url.slice(-80))
  destroyPlayerInternal(video)
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
    const t = readLocalTime(resumeKey)
    if (t > 5) {
      savedTime.value = t
      if (_resumeAutoJump) {
        const v = getVideoEl()
        if (v) {
          try { v.currentTime = Math.max(0, t - 1) } catch { /* ignore */ }
          console.log(`[Player] ⏩ 自动跳到上次播放位置: ${t.toFixed(1)}s (key=${resumeKey})`)
        }
      } else {
        startResumePrompt(t)
      }
    } else {
      console.log(`[Player] ℹ️ 无有效历史进度 (key=${resumeKey})，从头播放`)
    }

    console.log('[Player] 动态 import hls.js')
    const { default: Hls } = await import('hls.js')
    if (Hls.isSupported()) {
      // 1) 用 TsCache 解析 m3u8（文本缓存，避免重复请求 m3u8）
      //    同时激活 fetch 拦截器，hls.js 的 TS 片段下载会透明经过缓存
      TsCache.enable()
      let parsed: { urls: string[], variantUrls: string[], targetduration: number, isMaster: boolean, streamInfo?: StreamVariantInfo[] }
      try {
        parsed = await TsCache.fetchAndParseM3u8(url)
      } catch {
        parsed = { urls: [], variantUrls: [], targetduration: 6, isMaster: false, streamInfo: [] }
      }

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

      const hls = new Hls(hlsConfig)
        ; (video as any).__hls = hls

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
      hls.on(Hls.Events.MANIFEST_PARSED, (_e, data: any) => {
        console.log('[Player] ✅ manifest 解析完成，levels=', data?.levels?.length || 0)
      })
      hls.on(Hls.Events.LEVEL_LOADED, (_e, data: any) => {
        // 从 hls.js 的 fragments 拿到真实 TS URL（多码率/单码率都适用）
        const frags = data?.details?.fragments || []
        const curTargetDur = data?.details?.targetduration || parsed.targetduration || 6
        if (frags.length > 0) {
          const absUrls = frags.map((f: any) => {
            try { return new URL(f.url, url).href }
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
          const hlsBufferFrags = Math.ceil(30 / Math.max(curTargetDur, 1))
          const prefetchCount = Math.min(60, Math.max(20, Math.floor(frags.length / 2)))
          const startIdx = Math.max(0, Math.min(frags.length - 1, hlsBufferFrags + 15))
          const segUrls = frags.map((f: any) => {
            try { return new URL(f.url, url).href } catch { return f.url }
          })
          TsCache.prefetchFromSegments(segUrls, 0, startIdx, prefetchCount)
          console.log(`[Player] 📡 TsCache 预取: 片段 #${startIdx}+${prefetchCount} 片 (共 ${segUrls.length})`)
          // ⭐ 启动预缓冲：等待5个TS分片或10秒超时后才开始播放
          startPrebuffer()
        }
      })
      hls.on(Hls.Events.FRAG_CHANGED, (_e, data: any) => {
        if (data?.frag?.url) {
          try { TsCache.notifyCurrentTs(new URL(data.frag.url, url).href) }
          catch { TsCache.notifyCurrentTs(data.frag.url) }
        }
      })
hls.on(Hls.Events.ERROR, (_e, data: any) => {
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

	        if (data.fatal) {
	          // ⭐ 非 m3u8 URL 回退：manifest 加载/解析失败 → 尝试直接 video.src
	          const isManifestError =
	            details === 'manifestLoadError' ||
	            details === 'manifestParsingError' ||
	            details === 'manifestIncompatibleCodecsError' ||
	            details === 'manifestLoadTimeOut'
	          if (isManifestError) {
	            console.log('[Player] ⚠ HLS manifest 失败，回退到直接播放')
	            try { hls.destroy() } catch { /* ignore */ }
	            ;(video as any).__hls = null
	            video.src = url
	            video.onerror = () => {
	              errorMsg.value = '视频加载失败，请检查链接或网络'
	            }
	            return
	          }
	          switch (data.type) {
            case Hls.ErrorTypes.NETWORK_ERROR:
              console.log('[Player] 网络错误，尝试恢复 startLoad()')
              try { hls.startLoad() } catch (e) { console.warn('[Player] startLoad 失败:', e) }
              // 启动 10 秒超时计时器：若 10 秒内视频未开始播放，显示错误提示
              if (!networkErrTimer) {
                networkErrTimer = setTimeout(() => {
                  showNetworkError.value = true
                  errorMsg.value = '播放链接超过 10 秒无法连接'
                  networkErrTimer = null
                }, 10000)
              }
              break
            case Hls.ErrorTypes.MEDIA_ERROR:
              console.log('[Player] 媒体错误，尝试恢复 recoverMediaError()')
              try { hls.recoverMediaError() } catch (e) { console.warn('[Player] recoverMediaError 失败:', e) }
              break
            default:
              errorMsg.value = '视频流加载失败：' + (details || data.type || '未知错误')
              console.error('[Player] ❌ 无法恢复的错误:', data)
              destroyPlayerInternal(getVideoEl() || undefined as any)
              break
          }
        }
      })
      console.log('[Player] 调用 hls.loadSource:', url.slice(-80))
      hls.loadSource(url)
      hls.attachMedia(video)
      // 启动缓存监控定时器（每秒更新一次）
      if (cacheStatsTimer != null) { window.clearInterval(cacheStatsTimer); cacheStatsTimer = null }
      updateCacheStats()
      cacheStatsTimer = window.setInterval(updateCacheStats, 1000)
    } else if (video.canPlayType('application/vnd.apple.mpegurl')) {
      console.log('[Player] ⚠ hls.js 不支持，回退原生 HLS')
      video.src = url
      if (props.autoplay !== false) safePlay(true)
    } else {
      errorMsg.value = '当前环境不支持 HLS 播放'
      console.error('[Player] ❌ 当前环境不支持 HLS 播放')
    }
  } catch (e: any) {
    console.error('[Player] ❌ 异常:', e)
    errorMsg.value = '播放器初始化失败：' + (e?.message || String(e))
  }
}

let _retryCount = 0
function setupPlayer(): void {
  const video = getVideoEl()
  if (!video) {
    if (_retryCount < 8) {
      _retryCount++
      setTimeout(setupPlayer, 50)
    } else {
      errorMsg.value = '无法初始化视频播放器'
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
    errorMsg.value = '未获取到视频地址'
    return
  }
  bindCommonVideoEvents(video)
  if (isHls(url)) {
    loadHls(video, url)
  } else {
    destroyPlayerInternal(video)
    // ⭐ 修复：非 m3u8 后缀的 URL 可能是重定向到 HLS 的短链接（如 hn.bfvvs.com/play/xxx）
    // 直接尝试用 hls.js 加载，如果解析到有效 m3u8 就播放，失败则回退到 video.src
    loadHls(video, url)
  }
}

function bindCommonVideoEvents(video: HTMLVideoElement): void {
  if ((video as any).__eventsBound) return
    ; (video as any).__eventsBound = true
  bindOsdListeners(video)

  video.addEventListener('play', () => { playing.value = true; loading.value = false; videoReady.value = true; stopLoadingStats(); clearNetworkErrTimer() })
  video.addEventListener('pause', () => { playing.value = false })
  video.addEventListener('timeupdate', () => {
    current.value = video.currentTime
    if (video.duration) duration.value = video.duration
    updateBuffer()
    // ⭐ 节流保存进度：使用稳定 key，避免跨集污染
    if (_saveTimer == null) {
      _saveTimer = window.setInterval(() => {
        const key = stableResumeKey()
        saveResumeTime(key, current.value, duration.value)
        // 同时保存用户音量
        const v = getVideoEl()
        if (v) { writeSetting('volume', String(v.volume)); writeSetting('muted', v.muted ? '1' : '0') }
      }, SAVE_INTERVAL_MS)
    }
  })
  video.addEventListener('loadedmetadata', () => {
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
    console.log(`[Player] loadedmetadata: duration=${video.duration.toFixed(1)}s, volume=${video.volume.toFixed(2)}`)
    _aiReady = true
  })
  video.addEventListener('progress', updateBuffer)
  video.addEventListener('seeking', updateBuffer)
  video.addEventListener('seeked', () => {
    updateBuffer()
    if (upscaler) upscaler.onSeeked()
  })
  video.addEventListener('waiting', () => { loading.value = true; startLoadingStats() })
  video.addEventListener('canplay', () => {
    videoReady.value = true
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
  video.addEventListener('volumechange', () => {
    volume.value = video.volume
    muted.value = video.muted
    writeSetting('volume', String(video.volume))
    writeSetting('muted', video.muted ? '1' : '0')
    // ⭐ 统一由 volumechange 触发 toast，覆盖所有场景（滚轮/键盘/按钮）
    showVolumeToastRef()
  })
  video.addEventListener('ratechange', () => { speed.value = video.playbackRate; writeSetting('speed', String(video.playbackRate)) })
  video.addEventListener('enterpictureinpicture', () => { isPiP.value = true })
  video.addEventListener('leavepictureinpicture', () => { isPiP.value = false })
  video.addEventListener('ended', () => {
    // 播放结束：移除当前进度（下次不跳回结尾）
    try { localStorage.removeItem(stableResumeKey()) } catch { /* ignore */ }
    if (_saveTimer != null) { window.clearInterval(_saveTimer); _saveTimer = null }
  })
  video.addEventListener('error', () => { loading.value = false })
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
function jumpToSavedTime(autoRememberChoice: boolean): void {
  const t = savedTime.value
  const v = getVideoEl()
  if (t != null && v) {
    try { v.currentTime = Math.max(0, t - 1); console.log(`[Player] ⏩ 跳到: ${t.toFixed(1)}s`) } catch { /* ignore */ }
  }
  if (autoRememberChoice) { saveAutoJumpConfig(true); console.log('[Player] ✅ 已记住：自动跳到上次播放位置') }
  dismissResumePrompt()
}

function destroyPlayerInternal(video: HTMLVideoElement): void {
  ++_playToken
  TsCache.setAbrSwitchCallback(null)
  try {
    const hls = (video as any).__hls
    if (hls) {
      try { hls.destroy() } catch { /* ignore */ }
      ; (video as any).__hls = null
    }
    try { video.pause() } catch { /* ignore */ }
    try { video.removeAttribute('src') } catch { /* ignore */ }
    try { video.load() } catch { /* ignore */ }
    try { video.removeAttribute('data-autoplay-done') } catch { /* ignore */ }
    try { delete (video as any).__eventsBound } catch { /* ignore */ }
  } catch { /* ignore */ }
  if (cacheStatsTimer != null) { window.clearInterval(cacheStatsTimer); cacheStatsTimer = null }
  if (_saveTimer != null) { window.clearInterval(_saveTimer); _saveTimer = null }
  if (_resumeTimer != null) { window.clearInterval(_resumeTimer); _resumeTimer = null }
  // 清理预缓冲定时器
  preBuffering.value = false
  if (prebufferCheckTimer) { clearInterval(prebufferCheckTimer); prebufferCheckTimer = null }
  if (prebufferTimeout) { clearTimeout(prebufferTimeout); prebufferTimeout = null }
  stopLoadingStats()
  stopAiPipeline()
  _aiReady = false
  cacheStats.value = { hits: 0, misses: 0, entries: 0, bytes: 0, hitRate: 0, totalSegments: 0, prefetched: 0 }
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
const progressHoverPct = ref(-1) // 鼠标悬停在进度条上的百分比位置（-1 表示不显示）
const progressHoverTime = ref(-1) // 悬停时间（秒），用于显示时间预览
const thumbPreviewImg = ref('') // 缩略图 dataURL
const thumbPreviewVisible = ref(false)

// 缩略图采样：用一个隐藏的 <video> 元素 seek + canvas 抓帧
const thumbVideoRef = ref<HTMLVideoElement | null>(null)
const thumbCanvasRef = ref<HTMLCanvasElement | null>(null)
let _thumbLastSeekTime = 0 // 节流：上次 seek 时间戳
const THUMB_SEEK_THROTTLE_MS = 150 // 至少 150ms 间隔才触发新 seek
let _thumbSeekPending = false

function captureThumbnail(timeSec: number): void {
  const v = thumbVideoRef.value
  const canvas = thumbCanvasRef.value
  if (!v || !canvas) { _thumbSeekPending = false; return }
  const ctx = canvas.getContext('2d')
  if (!ctx) { _thumbSeekPending = false; return }

  // ⭐ 检查视频是否可 seek：readyState >= 1 (HAVE_METADATA) 才允许设置 currentTime
  if (v.readyState < 1) {
    _thumbSeekPending = false
    return
  }

  // 实际抓帧逻辑
  const doCapture = () => {
    try {
      const vw = v.videoWidth || 160
      const vh = v.videoHeight || 90
      if (vw <= 0 || vh <= 0) {
        thumbPreviewVisible.value = false
        _thumbSeekPending = false
        return
      }
      const scale = Math.min(160 / vw, 90 / vh, 1)
      const tw = Math.round(vw * scale)
      const th = Math.round(vh * scale)
      canvas.width = tw
      canvas.height = th
      ctx.drawImage(v, 0, 0, tw, th)
      thumbPreviewImg.value = canvas.toDataURL('image/jpeg', 0.7)
      thumbPreviewVisible.value = true
    } catch {
      thumbPreviewVisible.value = false
    }
    _thumbSeekPending = false
  }

  // ⭐ 超时保护：500ms 后强制重置 _thumbSeekPending，防止 seeked 事件永不触发导致卡死
  let timedOut = false
  const timeoutId = setTimeout(() => {
    timedOut = true
    _thumbSeekPending = false
  }, 500)

  // seeked 事件回调
  const onSeeked = () => {
    v.removeEventListener('seeked', onSeeked)
    if (timedOut) return
    clearTimeout(timeoutId)
    doCapture()
  }

  // ⭐ 如果已接近目标时间（差距 < 0.5s），无需 seek，直接抓帧
  if (Math.abs(v.currentTime - timeSec) <= 0.5) {
    clearTimeout(timeoutId)
    _thumbSeekPending = false
    doCapture()
    return
  }

  v.addEventListener('seeked', onSeeked)
  try {
    v.currentTime = timeSec
  } catch {
    v.removeEventListener('seeked', onSeeked)
    clearTimeout(timeoutId)
    _thumbSeekPending = false
  }
}

async function initThumbSampler(): Promise<void> {
  const mainVideo = getVideoEl()
  if (!mainVideo) return
  // 创建隐藏的采样 video（同源，静音，不播放）
  const sampleVideo = document.createElement('video')
  sampleVideo.muted = true
  sampleVideo.preload = 'auto'
  sampleVideo.playsInline = true
  sampleVideo.style.display = 'none'
  sampleVideo.style.position = 'absolute'
  sampleVideo.style.visibility = 'hidden'
  sampleVideo.style.pointerEvents = 'none'
  sampleVideo.setAttribute('tabindex', '-1')
  wrapperRef.value?.appendChild(sampleVideo)
  thumbVideoRef.value = sampleVideo

  // 创建 canvas（隐藏）
  const canvas = document.createElement('canvas')
  canvas.style.display = 'none'
  wrapperRef.value?.appendChild(canvas)
  thumbCanvasRef.value = canvas

  let thumbHls: any = null

  // 主 video 元素销毁时清理
  const cleanup = () => {
    try { thumbHls?.destroy() } catch { }
    try { sampleVideo.remove() } catch { }
    try { canvas.remove() } catch { }
    thumbVideoRef.value = null
    thumbCanvasRef.value = null
    thumbPreviewImg.value = ''
    thumbPreviewVisible.value = false
  }
    ; (mainVideo as any).__thumbCleanup = cleanup

  // ⭐ HLS 流：用第二个 hls.js 小实例加载同源流（低缓冲，仅用于 seek + 抓帧）
  if (isHls(props.url)) {
    try {
      const { default: Hls } = await import('hls.js')
      if (Hls.isSupported()) {
        const hlsConfig: any = {
          enableWorker: false,
          lowLatencyMode: false,
          maxBufferLength: 2,
          maxMaxBufferLength: 3,
          maxBufferSize: 3 * 1000 * 1000,
          fragLoadingTimeOut: 8000,
          fragLoadingMaxRetry: 3,
          manifestLoadingTimeOut: 6000,
          manifestLoadingMaxRetry: 2,
          autoStartLoad: true,
          startLevel: -1,
          loader: TsCache.TsCacheLoader,
        }
        thumbHls = new Hls(hlsConfig)
        thumbHls.loadSource(props.url)
        thumbHls.attachMedia(sampleVideo)
        // 加载后暂停，只用于 canvas 抓帧
        thumbHls.on(Hls.Events.MANIFEST_PARSED, () => {
          sampleVideo.play().then(() => {
            sampleVideo.pause()
          }).catch(() => { })
        })
        console.log('[Player] 🖼️ 缩略图 HLS 实例已创建')
      }
    } catch (e) {
      console.warn('[Player] 缩略图 HLS 初始化失败，回退到纯时间预览:', e)
    }
  } else {
    // 非 HLS：直接用 src
    sampleVideo.src = mainVideo.src || (mainVideo.querySelector('source') as HTMLSourceElement)?.src || ''
  }

  // 当主视频 url 变化时，重新加载缩略图采样视频
  const origUrl = props.url
  const urlWatch = watch(() => props.url, (newUrl) => {
    if (newUrl !== origUrl) {
      cleanup()
      setTimeout(() => initThumbSampler(), 500)
    }
  })
    ; (mainVideo as any).__thumbUrlWatch = urlWatch
}

function destroyThumbSampler(): void {
  const v = getVideoEl()
  if (v) {
    try { (v as any).__thumbCleanup?.() } catch { }
    try { (v as any).__thumbUrlWatch?.() } catch { }
  }
  if (thumbVideoRef.value) {
    try { thumbVideoRef.value.remove() } catch { }
    thumbVideoRef.value = null
  }
  if (thumbCanvasRef.value) {
    try { thumbCanvasRef.value.remove() } catch { }
    thumbCanvasRef.value = null
  }
  thumbPreviewImg.value = ''
  thumbPreviewVisible.value = false
}

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

// 进度条悬停：计算百分比位置 + 显示时间预览 + 缩略图
function onProgressHover(e: MouseEvent): void {
  if (!progressContainerRef.value) return
  const rect = progressContainerRef.value.getBoundingClientRect()
  const pct = Math.max(0, Math.min(100, ((e.clientX - rect.left) / rect.width) * 100))
  progressHoverPct.value = pct
  const v = getVideoEl()
  if (v && v.duration) {
    progressHoverTime.value = (pct / 100) * v.duration
    // 节流触发缩略图抓帧
    const now = performance.now()
    if (now - _thumbLastSeekTime >= THUMB_SEEK_THROTTLE_MS && !_thumbSeekPending) {
      _thumbLastSeekTime = now
      _thumbSeekPending = true
      captureThumbnail(progressHoverTime.value)
    }
  }
}

// ========= 操作 OSD（屏幕中央提示：快进/快退/音量/倍速） =========
const osdText = ref('')
const osdIcon = ref('')
let _osdTimer: ReturnType<typeof setTimeout> | null = null

function showOsd(icon: string, text: string, duration = 800): void {
  osdIcon.value = icon
  osdText.value = text
  if (_osdTimer) clearTimeout(_osdTimer)
  _osdTimer = setTimeout(() => { osdText.value = ''; osdIcon.value = '' }, duration)
}

// 监听视频 seek 事件（来自键盘快捷键或进度条拖动）
function bindOsdListeners(video: HTMLVideoElement): void {
  if ((video as any).__osdBound) return
    ; (video as any).__osdBound = true

  let _lastSeekTime = -1
  video.addEventListener('seeking', () => {
    const now = Date.now()
    // 只在短时间内连续 seek 时更新（避免进度条拖动时疯狂闪烁）
    if (now - _lastSeekTime > 300) {
      _lastSeekTime = now
    }
  })
  video.addEventListener('volumechange', () => {
    const pct = Math.round(video.volume * 100)
    showOsd(video.muted ? '🔇' : '🔊', `${pct}%`)
  })

  // 自定义 seek OSD 事件（由 Player.vue 键盘快捷键派发）
  video.addEventListener('cczj-seek-osd', ((e: CustomEvent) => {
    const { delta } = e.detail
    const icon = delta > 0 ? '⏩' : '⏪'
    const absSec = Math.abs(delta)
    showOsd(icon, `${delta > 0 ? '+' : '-'}${absSec}s`)
  }) as EventListener)
}

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

onMounted(async () => {
  await nextTick()
  setupPlayer()
  document.addEventListener('fullscreenchange', onFsChange)
  document.addEventListener('webkitfullscreenchange', onFsChange)
  document.addEventListener('keydown', onKeyDown)
  // 让播放器区域自动获得键盘焦点（上下键调节音量等）
  setTimeout(() => { wrapperRef.value?.focus?.(); initThumbSampler() }, 100)
})

onBeforeUnmount(() => {
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
  if (_osdTimer) { clearTimeout(_osdTimer); _osdTimer = null }
})

watch(() => props.url, (newUrl, oldUrl) => {
  if (!newUrl) {
    destroyPlayer()
    return
  }
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
  }
  setTimeout(setupPlayer, 0)
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
    ref="wrapperRef" tabindex="0" @mousemove="toggleShow(true); mouseInside = true" @mouseenter="mouseInside = true"
    @mouseleave="onMouseLeave" @click="onWrapperClick" @dblclick.stop="toggleFullscreen()" @wheel.prevent="onWheel">
    <!-- 顶部栏：标题 + 缓存统计 + 收藏按钮 -->
    <div v-show="showTitleBar && (showControls || !playing)" class="player-title-bar" @click.stop @dblclick.stop>
      <span class="player-title">{{ title || url }}</span>
      <span v-if="isHls(url) && cacheStats.totalSegments > 0" class="cache-info"
        :title="`命中: ${cacheStats.hits} 未命中: ${cacheStats.misses} 预取: ${cacheStats.entries}/${cacheStats.totalSegments} 片`">
        预取 {{ cacheStats.entries }}/{{ cacheStats.totalSegments }} ·
        命中 {{ (cacheStats.hitRate * 100).toFixed(0) }}%
      </span>
      <button class="fav-btn-in-player" :class="{ 'is-fav': isFav }" :disabled="favBusy"
        :title="isFav ? '取消收藏' : '加入收藏'" @click.stop="emit('toggleFavorite')">
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
    <div v-show="mouseInside" class="player-drag-handle" title="拖拽移动窗口" />

    <video class="native-video" playsinline preload="auto" @click.stop="togglePlay"></video>

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
        <img :src="loadingGif" class="loading-spinner" alt="加载中..." />
        <!-- 缓冲信息 -->
        <div class="loading-info" v-if="loadingTotal > 0">
          <div class="loading-text">
            <template v-if="preBuffering">缓冲中 {{ loadingCached }}/{{ loadingTotal }} 片段</template>
            <template v-else>已缓存 {{ loadingCached }}/{{ loadingTotal }} 片段</template>
          </div>
          <div class="loading-speed" v-if="loadingSpeed">{{ loadingSpeed }}</div>
          <div class="loading-bar-wrap">
            <div class="loading-bar-fill"
              :style="{ width: (loadingTotal > 0 ? loadingCached / loadingTotal * 100 : 0) + '%' }">
            </div>
          </div>
        </div>
        <!-- 无片段信息时显示文字提示 -->
        <div class="loading-text" v-else-if="loadingElapsed > 0">
          <template v-if="preBuffering">连接中... {{ loadingElapsed }}s</template>
          <template v-else>加载中...</template>
        </div>
      </div>
    </div>

    <div v-if="errorMsg" class="player-error">
      <span>⚠</span>
      <span>{{ errorMsg }}</span>
      <div v-if="showNetworkError" class="error-actions">
        <button class="error-btn error-btn-primary" @click.stop="refreshPage()">刷新视频</button>
      </div>
    </div>

    <!-- B 站风格：底部左侧继续播放提示（小胶囊，仅在有记忆时出现） -->
    <div v-if="showResumePrompt" class="resume-bili" @click.stop>
      <span class="resume-bili-text">已为您定位至 <b>{{ savedTime != null ? fmt(savedTime) : '00:00' }}</b></span>
      <button class="resume-bili-link" @click.stop="jumpToSavedTime(false)">跳回</button>
      <button class="resume-bili-link" @click.stop="jumpToSavedTime(true)">跳回并记住</button>
      <button class="resume-bili-link resume-bili-dismiss" @click.stop="dismissResumePrompt">
        从头播放 ({{ resumeRemainSec }}s)
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
          <span>AI 画质增强</span>
        </div>
        <div class="ai-warning-body">
          <p>即将开启 AI 画质增强，将使用 GPU 实时处理视频帧：</p>
          <ul>
            <li><b>动画增强</b> — 针对动画/动漫优化：去色带、线条增强、平坦区域降噪</li>
            <li><b>影视增强</b> — 针对真人影视优化：纹理锐化、暗部细节提升、压缩噪声抑制</li>
          </ul>
          <p>注意：动画增强不适用于真人影视，反之亦然，请根据内容类型选择。</p>
          <ul>
            <li><b>GPU 计算负载</b> — 可能导致显卡温度升高和风扇加速</li>
            <li><b>电池消耗</b> — 笔记本设备将显著增加耗电量</li>
            <li><b>性能影响</b> — 低端设备可能出现卡顿或掉帧</li>
          </ul>
          <p class="ai-warning-note">如遇到性能问题，可随时切换回"原高清"模式。</p>
        </div>
        <div class="ai-warning-footer">
          <button class="ai-warning-btn ai-warning-btn--cancel" @click="cancelAiMode">取消</button>
          <button class="ai-warning-btn ai-warning-btn--confirm" @click="confirmAiMode">确定开启</button>
        </div>
      </div>
    </div>

    <!-- 暂停图标：右下角大字，仅暂停时显示 -->
    <div v-show="!playing && !loading" class="pause-overlay" @click.stop="togglePlay">
      <img :src="pauseImg" class="pause-icon" alt="已暂停" />
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
      <button class="ctrl-btn" @click="emit('prev')" :disabled="!hasPrev" title="上一集">
        <Icon name="prev" :size="16" />
      </button>

      <!-- 播放/暂停（中间） -->
      <button class="ctrl-btn play-btn" @click="togglePlay" :title="playing ? '暂停' : '播放'">
        <Icon :name="playing ? 'pause' : 'play'" :size="18" />
      </button>

      <!-- 下一集 -->
      <button class="ctrl-btn" @click="emit('next')" :disabled="!hasNext" title="下一集">
        <Icon name="next" :size="16" />
      </button>

      <!-- B站风格时间显示 / 跳转 -->
      <div class="time-display" @click.stop>
        <template v-if="showTimeInput">
          <input ref="timeInputRef" class="time-input" v-model="timeInputValue" @keydown.enter.prevent="jumpToTime"
            @keydown.escape.prevent="cancelTimeInput" @blur="jumpToTime" @click.stop @mousedown.stop />
        </template>
        <template v-else>
          <div class="time-bar" @click="toggleTimeInput" title="点击跳转到指定时间">
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
        <button class="ctrl-btn" @click.stop="toggleMute(); keepVisible()" :title="muted ? '取消静音' : '静音（M）'">
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
          @click="showSpeedPanel = !showSpeedPanel; showVolumePanel = false; keepVisible()" title="播放速度">
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

      <!-- 播放设置 -->
      <div class="playback-settings-group" @click.stop>
        <button class="ctrl-btn" @click="showPlaybackSettings = !showPlaybackSettings; keepVisible()" title="播放设置">
          <Icon name="settings" :size="16" />
        </button>
        <div class="playback-settings-popup" :class="{ show: showPlaybackSettings }" @click.stop>
          <div class="playback-settings-item">
            <span>自动播放</span>
            <label class="ps-toggle">
              <input type="checkbox" :checked="props.autoplay" @change="emit('toggleAutoplay')" />
              <span class="ps-switch"></span>
            </label>
          </div>
          <div class="playback-settings-item">
            <span>自动连播</span>
            <label class="ps-toggle">
              <input type="checkbox" :checked="autoNextEnabled" @change="toggleAutoNext" />
              <span class="ps-switch"></span>
            </label>
          </div>
          <div class="playback-settings-divider"></div>
          <button class="playback-settings-link" @click.stop="showPlaybackSettings = false; showShortcutModal = true">
            <Icon name="keyboard" :size="14" />
            <span>快捷键设置</span>
            <Icon name="chevron-right" :size="12" />
          </button>
        </div>
      </div>
      <!-- 报告广告 -->
      <div class="report-ad-group" @click.stop>
        <button class="ctrl-btn" @click.stop="toggleReportAd(); keepVisible()" title="报告广告域名">
          <Icon name="flag" :size="16" />
        </button>
        <div class="report-ad-popup" :class="{ show: showReportAd }" @click.stop>
          <div class="report-ad-title">点击上报广告域名</div>
          <div v-if="reportAdDomains.length === 0" class="report-ad-empty">未检测到片段域名</div>
          <button v-for="d in reportAdDomains" :key="d" class="report-ad-item" @click.stop="doReportAd(d)">
            <Icon name="shield" :size="13" />
            <span class="report-ad-domain">{{ d }}</span>
          </button>
        </div>
      </div>
      <!-- 视频信息 -->
      <div class="video-info-group" @click.stop>
        <button class="ctrl-btn" @click.stop="showVideoInfo = !showVideoInfo; keepVisible()" title="视频信息">
          <Icon name="info" :size="16" />
        </button>
      </div>
      <!-- 画中画 -->
      <button class="ctrl-btn" @click="togglePiP" :title="isPiP ? '退出画中画' : '画中画（I）'" :class="{ active: isPiP }">
        <Icon :name="isPiP ? 'pip-exit' : 'pip'" :size="16" />
      </button>
      <!-- 全屏 -->
      <button class="ctrl-btn" @click="toggleFullscreen" :title="isFullscreen ? '退出全屏' : '全屏（F）'">
        <Icon :name="isFullscreen ? 'exit-fullscreen' : 'fullscreen'" :size="16" />
      </button>
    </div>

    <!-- 报告广告成功 toast -->
    <transition name="fade">
      <div v-show="reportAdToast" class="report-ad-toast">
        <Icon name="check" :size="14" />
        <span>{{ reportAdToast }}</span>
      </div>
    </transition>

    <!-- ====== 视频信息模态框 ====== -->
    <transition name="modal-fade">
      <div v-if="showVideoInfo" class="vp-modal-overlay" @click="showVideoInfo = false" @keydown="onShortcutKeyDown"
        @wheel.stop>
        <div class="vp-modal vp-modal-info" @click.stop>
          <div class="vp-modal-header">
            <span>视频信息</span>
            <button class="vp-modal-close" @click="showVideoInfo = false">
              <Icon name="x" :size="16" />
            </button>
          </div>
          <div class="vp-modal-body vp-modal-scroll" v-if="videoInfo">
            <!-- 基本播放信息 -->
            <div class="vp-info-section-title">播放信息</div>
            <div class="vp-info-grid">
              <div class="vp-info-item">
                <span class="vp-info-label">时长</span>
                <span class="vp-info-val">{{ videoInfo.duration }}</span>
              </div>
              <div class="vp-info-item">
                <span class="vp-info-label">当前时间</span>
                <span class="vp-info-val">{{ videoInfo.current }} ({{ videoInfo.progress }})</span>
              </div>
              <div class="vp-info-item">
                <span class="vp-info-label">播放速度</span>
                <span class="vp-info-val">{{ videoInfo.speed }}</span>
              </div>
              <div class="vp-info-item">
                <span class="vp-info-label">音量</span>
                <span class="vp-info-val">{{ videoInfo.volume }}</span>
              </div>
              <div v-if="videoInfo.buffered" class="vp-info-item">
                <span class="vp-info-label">缓冲进度</span>
                <span class="vp-info-val">{{ videoInfo.buffered }}</span>
              </div>
            </div>
            <!-- 视频流信息 -->
            <div class="vp-info-section-title">视频流</div>
            <div class="vp-info-grid">
              <div v-if="videoInfo.resolution" class="vp-info-item">
                <span class="vp-info-label">分辨率</span>
                <span class="vp-info-val">{{ videoInfo.resolution }}</span>
              </div>
              <div v-if="videoInfo.fps" class="vp-info-item">
                <span class="vp-info-label">帧率</span>
                <span class="vp-info-val">{{ videoInfo.fps }}</span>
              </div>
              <div v-if="videoInfo.droppedFrames > 0" class="vp-info-item">
                <span class="vp-info-label">丢帧</span>
                <span class="vp-info-val vp-info-warn">{{ videoInfo.droppedFrames }}</span>
              </div>
              <div v-if="videoInfo.streamBitrate" class="vp-info-item">
                <span class="vp-info-label">源码率</span>
                <span class="vp-info-val vp-info-highlight">{{ videoInfo.streamBitrate }}</span>
              </div>
              <div v-if="videoInfo.currentBitrate" class="vp-info-item">
                <span class="vp-info-label">当前码率</span>
                <span class="vp-info-val">{{ videoInfo.currentBitrate }}</span>
              </div>
              <div v-if="videoInfo.codec" class="vp-info-item">
                <span class="vp-info-label">编码格式</span>
                <span class="vp-info-val">{{ videoInfo.codec }}</span>
              </div>
              <div v-if="videoInfo.hlsLevelInfo" class="vp-info-item">
                <span class="vp-info-label">HLS级别</span>
                <span class="vp-info-val">{{ videoInfo.hlsLevelInfo }}</span>
              </div>
            </div>
            <!-- 缓存 & 网络 -->
            <div class="vp-info-section-title">缓存 & 网络</div>
            <div class="vp-info-grid">
              <div class="vp-info-item vp-info-full">
                <span class="vp-info-label">缓存状态</span>
                <span class="vp-info-val">{{ videoInfo.cacheInfo }}</span>
              </div>
              <div class="vp-info-item">
                <span class="vp-info-label">缓存模式</span>
                <span class="vp-info-val">{{ videoInfo.cacheMode }}</span>
              </div>
              <div v-if="videoInfo.networkMode" class="vp-info-item">
                <span class="vp-info-label">网络模式</span>
                <span class="vp-info-val">{{ videoInfo.networkMode }}</span>
              </div>
            </div>
            <!-- 增强 & 源 -->
            <div class="vp-info-section-title">增强 & 源</div>
            <div class="vp-info-grid">
              <div class="vp-info-item">
                <span class="vp-info-label">画质模式</span>
                <span class="vp-info-val">{{ videoInfo.qualityMode }}</span>
              </div>
              <div v-if="videoInfo.sourceHost" class="vp-info-item vp-info-full">
                <span class="vp-info-label">源域名</span>
                <span class="vp-info-val vp-info-mono">{{ videoInfo.sourceHost }}</span>
              </div>
              <div v-if="videoInfo.sourceUrl" class="vp-info-item vp-info-full">
                <span class="vp-info-label">源URL</span>
                <span class="vp-info-val vp-info-mono vp-info-url">{{ videoInfo.sourceUrl }}</span>
              </div>
            </div>
          </div>
          <div class="vp-modal-body" v-else>
            <p class="vp-empty-text">暂无视频信息</p>
          </div>
        </div>
      </div>
    </transition>

    <!-- ====== 快捷键设置模态框 ====== -->
    <transition name="modal-fade">
      <div v-if="showShortcutModal" class="vp-modal-overlay" @click="showShortcutModal = false"
        @keydown="onShortcutKeyDown" @wheel.stop>
        <div class="vp-modal vp-modal-shortcut" @click.stop>
          <div class="vp-modal-header">
            <span>快捷键设置</span>
            <button class="vp-modal-close" @click="showShortcutModal = false">
              <Icon name="x" :size="16" />
            </button>
          </div>
          <div class="vp-modal-body vp-modal-scroll">
            <p class="vp-modal-hint">点击按键可自定义，按下新的按键即可绑定。按 Esc 取消编辑。</p>
            <div class="vp-shortcut-list">
              <div v-for="action in SHORTCUT_ACTIONS" :key="action.id" class="vp-shortcut-row">
                <span class="vp-sc-label">{{ action.label }}</span>
                <span class="vp-sc-desc">{{ action.description }}</span>
                <div class="vp-sc-actions">
                  <button class="vp-sc-btn" :class="{ editing: editingShortcutId === action.id }"
                    @click.stop="startEditShortcut(action.id)">
                    <template v-if="editingShortcutId === action.id">
                      <span class="vp-sc-recording">按下新按键...</span>
                    </template>
                    <template v-else>
                      <span v-for="(k, ki) in (shortcutMap[action.id] || [])" :key="ki" class="vp-sc-key">{{ fmtKey(k)
                      }}</span>
                      <span v-if="!shortcutMap[action.id] || shortcutMap[action.id].length === 0"
                        class="vp-sc-none">未设置</span>
                    </template>
                  </button>
                  <button class="vp-sc-reset" @click.stop="resetShortcut(action.id)" title="还原默认">
                    <Icon name="reset" :size="12" />
                  </button>
                </div>
              </div>
            </div>
          </div>
          <div class="vp-modal-footer">
            <button class="vp-btn-secondary" @click="resetAllShortcuts">重置全部</button>
            <button class="vp-btn-primary" @click="showShortcutModal = false">完成</button>
          </div>
        </div>
      </div>
    </transition>
  </div>
</template>

<style scoped>
/* ========= 播放器容器 ========= */
.player-wrapper {
  background: #000;
  color: #fff;
  width: 100%;
  height: 100%;
  position: relative;
  overflow: hidden;
  outline: none;
  /* 键盘焦点时不显示默认 outline */
}

.player-wrapper.cursor-hidden {
  cursor: none;
}

.player-wrapper.cursor-hidden .native-video {
  cursor: none;
}

.player-wrapper.fullscreen {
  width: 100vw;
  height: 100vh;
  border-radius: 0;
  box-shadow: none;
  margin: 0;
  padding: 0;
}

/* ====== 系统级全屏时的全局样式覆盖 ======
   当 Wails WindowFullscreen 把整个应用窗口全屏时，让父级的弹窗/遮罩/容器
   也一起扩展到整个窗口，让视频真正铺满整个屏幕（而非局限于弹窗尺寸） */
:global(body[data-player-fullscreen='1']) {
  margin: 0;
  padding: 0;
  overflow: hidden;
}

:global(body[data-player-fullscreen='1'] #app),
:global(body[data-player-fullscreen='1'] .app-shell),
:global(body[data-player-fullscreen='1'] .app-body),
:global(body[data-player-fullscreen='1'] .main-content),
:global(body[data-player-fullscreen='1'] .player-page) {
  width: 100vw !important;
  height: 100vh !important;
  margin: 0 !important;
  padding: 0 !important;
  overflow: hidden !important;
}

:global(body[data-player-fullscreen='1'] .player-modal-mask),
:global(body[data-player-fullscreen='1'] .modal-backdrop) {
  background: #000;
  padding: 0;
}

:global(body[data-player-fullscreen='1'] .player-modal),
:global(body[data-player-fullscreen='1'] .modal-box) {
  width: 100vw !important;
  height: 100vh !important;
  max-width: 100vw !important;
  max-height: 100vh !important;
  border-radius: 0 !important;
  margin: 0 !important;
  border: none !important;
  box-shadow: none !important;
}

:global(body[data-player-fullscreen='1'] .player-modal-top),
:global(body[data-player-fullscreen='1'] .modal-head) {
  display: none;
}

:global(body[data-player-fullscreen='1'] .player-modal-body) {
  padding: 0;
}

:global(body[data-player-fullscreen='1'] .player-col-main),
:global(body[data-player-fullscreen='1'] .player-box),
:global(body[data-player-fullscreen='1'] .player-wrap),
:global(body[data-player-fullscreen='1'] .player-section) {
  width: 100vw;
  height: 100vh;
  padding: 0;
  margin: 0;
}

:global(body[data-player-fullscreen='1'] .player-col-side) {
  display: none;
}

/* 顶部栏：标题 + 缓存统计 + 关闭按钮（右）
   容器设为 pointer-events:none，让拖拽事件穿透到下层的 player-drag-handle；
   内部需要交互的元素（收藏按钮等）单独 pointer-events:auto。 */
.player-title-bar {
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 16px;
  background: linear-gradient(to bottom, rgba(0, 0, 0, 0.7), rgba(0, 0, 0, 0));
  color: #fff;
  font-size: 13px;
  z-index: 24;
  box-sizing: border-box;
  pointer-events: none;
}

/* 标题栏内所有可点击元素恢复交互 */
.player-title-bar button,
.player-title-bar .fav-btn-in-player {
  pointer-events: auto;
}

/* 拖拽遮罩条：鼠标经过视频顶部时浮现，可拖拽移动整个窗口。
   z-index 高于 player-title-bar(24)，确保拖拽区不被遮住。 */
.player-drag-handle {
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  height: 34px;
  z-index: 30;
  cursor: grab;
  background: linear-gradient(to bottom, rgba(0, 0, 0, 0.55), rgba(0, 0, 0, 0.25), transparent);
  /* Wails v3 在 Windows WebView2 下使用 --wails-draggable: drag 来实现窗口拖拽。
     放在 CSS 而非 inline style，避免被 Vue 的 style 绑定覆盖。 */
  --wails-draggable: drag;
  transition: opacity 0.2s ease, background 0.2s ease;
}

.player-drag-handle:hover {
  background: linear-gradient(to bottom, rgba(0, 0, 0, 0.7), rgba(0, 0, 0, 0.35), transparent);
}

.player-drag-handle:active {
  cursor: grabbing;
}

.player-title {
  color: #fff;
  font-size: 13px;
  font-weight: 500;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  flex: 1;
  min-width: 0;
}

.fav-btn-in-player {
  flex-shrink: 0;
  width: 32px;
  height: 26px;
  display: flex;
  align-items: center;
  justify-content: center;
  border: none;
  border-radius: 4px;
  background: rgba(255, 255, 255, 0.12);
  color: #fff;
  font-size: 16px;
  cursor: pointer;
  transition: background 0.15s ease, color 0.15s ease, transform 0.1s ease;
}

.fav-btn-in-player:hover {
  background: rgba(255, 255, 255, 0.22);
  transform: scale(1.08);
}

.fav-btn-in-player:disabled {
  opacity: 0.5;
  cursor: not-allowed;
  transform: none;
}

.fav-btn-in-player.is-fav {
  color: #fbbf24;
  background: rgba(251, 191, 36, 0.18);
}

.fav-btn-in-player.is-fav:hover {
  background: rgba(251, 191, 36, 0.3);
}

.player-url {
  color: rgba(255, 255, 255, 0.45);
  font-size: 11px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  flex: 1;
  min-width: 0;
}

.cache-info {
  flex: none;
  color: #7ee2b8;
  font-size: 11px;
  padding: 3px 10px;
  background: rgba(126, 226, 184, 0.12);
  border: 1px solid rgba(126, 226, 184, 0.3);
  border-radius: 999px;
  white-space: nowrap;
}

/* ========= 视频元素 ========= */
.native-video {
  position: absolute;
  inset: 0;
  z-index: 0;
  width: 100%;
  height: 100%;
  background: #000;
  display: block;
  object-fit: contain;
  outline: none;
}

.player-wrapper.fullscreen .native-video {
  /* 全屏时仍然保持比例，避免裁切画面 */
  object-fit: contain;
  width: 100vw;
  height: 100vh;
}

/* ========= 加载动画 ========= */
.loading {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  pointer-events: none;
  z-index: 3;
}

.loading-overlay {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 14px;
  padding: 28px 36px;
  background: rgba(0, 0, 0, 0.55);
  border-radius: 12px;
  backdrop-filter: blur(6px);
  min-width: 220px;
}

.loading-spinner {
  width: 64px;
  height: 64px;
  object-fit: contain;
}

.loading-info {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  width: 100%;
}

.loading-text {
  color: #fff;
  font-size: 14px;
  font-weight: 500;
  white-space: nowrap;
}

.loading-speed {
  color: rgba(255, 255, 255, 0.7);
  font-size: 12px;
  font-variant-numeric: tabular-nums;
}

.loading-bar-wrap {
  width: 100%;
  height: 4px;
  background: rgba(255, 255, 255, 0.15);
  border-radius: 2px;
  overflow: hidden;
}

.loading-bar-fill {
  height: 100%;
  background: linear-gradient(90deg, #1890ff 0%, #40a9ff 100%);
  border-radius: 2px;
  transition: width 0.4s ease;
  box-shadow: 0 0 6px rgba(24, 144, 255, 0.5);
}

/* ========= 错误提示 ========= */
.player-error {
  position: absolute;
  z-index: 4;
  bottom: 80px;
  left: 16px;
  right: 16px;
  padding: 10px 14px;
  background: rgba(220, 53, 69, 0.92);
  color: #fff;
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  font-size: 13px;
  border-radius: 8px;
  z-index: 4;
}

.error-actions {
  display: flex;
  gap: 8px;
  margin-left: auto;
}

.error-btn {
  padding: 4px 12px;
  border: 1px solid rgba(255, 255, 255, 0.6);
  border-radius: 4px;
  background: transparent;
  color: #fff;
  font-size: 12px;
  cursor: pointer;
  transition: background 0.15s;
}

.error-btn:hover {
  background: rgba(255, 255, 255, 0.15);
}

.error-btn-primary {
  background: rgba(255, 255, 255, 0.2);
  border-color: #fff;
}

/* ========= 暂停图标（右下角） ========= */
.pause-overlay {
  position: absolute;
  right: 24px;
  bottom: 90px;
  z-index: 6;
  cursor: pointer;
  pointer-events: auto;
  animation: pause-fade-in 0.3s ease;
  opacity: 0.85;
  transition: opacity 0.2s ease, transform 0.2s ease;
}

.pause-overlay:hover {
  opacity: 1;
  transform: scale(1.05);
}

.pause-icon {
  width: 80px;
  height: 80px;
  object-fit: contain;
  filter: drop-shadow(0 4px 12px rgba(0, 0, 0, 0.6));
}

@keyframes pause-fade-in {
  from {
    opacity: 0;
    transform: scale(0.85);
  }

  to {
    opacity: 0.85;
    transform: scale(1);
  }
}

/* ========= 进度条独立行（控制条上方） ========= */
/* z-index 低于 ctrl-bar(8)，确保 ctrl-bar 的弹出面板（音量/倍速/画质）
   始终在进度条上方，不会发生"点画质却点到进度条"的问题。 */
.progress-bar-wrapper {
  position: absolute;
  bottom: 52px;
  left: 0;
  right: 0;
  display: flex;
  align-items: center;
  padding: 4px 16px;
  z-index: 4;
  height: 28px;
  box-sizing: border-box;
  background: linear-gradient(to top, rgba(0, 0, 0, 0.5) 0%, rgba(0, 0, 0, 0) 100%);
}

/* B站风格时间显示 */
.time-display {
  display: flex;
  align-items: center;
  gap: 3px;
  font-size: 12px;
  font-variant-numeric: tabular-nums;
  color: rgba(255, 255, 255, 0.8);
  flex-shrink: 0;
  white-space: nowrap;
  line-height: 1;
}

.time-bar {
  cursor: text;
  padding: 2px 4px;
  border-radius: 3px;
  transition: background 0.15s ease, color 0.15s ease;
}

.time-bar:hover {
  background: rgba(255, 255, 255, 0.12);
  color: #fff;
}

.time-current {
  color: rgba(255, 255, 255, 0.5);
}

.time-sep {
  color: rgba(255, 255, 255, 0.4);
  font-size: 11px;
}

.time-duration {
  color: rgba(255, 255, 255, 0.5);
}

.time-input {
  width: 60px;
  padding: 2px 6px;
  border: 1px solid rgba(255, 255, 255, 0.3);
  border-radius: 4px;
  background: rgba(0, 0, 0, 0.6);
  color: #fff;
  font-size: 12px;
  font-variant-numeric: tabular-nums;
  text-align: center;
  outline: none;
  font-family: inherit;
}

.time-input:focus {
  border-color: var(--accent, #1890ff);
  box-shadow: 0 0 0 2px rgba(24, 144, 255, 0.2);
}

/* ========= 控制条 ========= */
/* z-index 高于 progress-bar-wrapper(4)，保证所有弹出面板在进度条上方。
   各弹出面板按钮组（volume-group, speed-group）各自定义 ::before 桥接区，
   不在此处设置全局桥接，避免阻碍进度条的悬停交互。 */
.ctrl-bar {
  position: absolute;
  bottom: 0;
  left: 0;
  right: 0;
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 16px 12px 16px;
  background: linear-gradient(to top, rgba(0, 0, 0, 0.85) 0%, rgba(0, 0, 0, 0.6) 60%, rgba(0, 0, 0, 0) 100%);
  color: #fff;
  font-size: 12px;
  user-select: none;
  z-index: 8;
  box-sizing: border-box;
  flex-wrap: nowrap;
  cursor: pointer;
}

.ctrl-btn {
  background: transparent;
  border: none;
  color: #fff;
  height: 32px;
  padding: 0 6px;
  min-width: 32px;
  border-radius: 6px;
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 4px;
  transition: all 0.15s;
  flex-shrink: 0;
  font-size: 11px;
  font-family: inherit;
}

.ctrl-btn:hover:not(:disabled) {
  background: rgba(24, 144, 255, 0.25);
  color: #40a9ff;
}

.ctrl-btn:disabled {
  opacity: 0.35;
  cursor: not-allowed;
}

.ctrl-btn.active {
  color: #40a9ff;
  background: rgba(24, 144, 255, 0.3);
}

.play-btn {
  /* 播放按钮略大一点，视觉上是中心 */
  height: 36px;
  min-width: 36px;
}

.play-btn:hover:not(:disabled) {
  background: rgba(24, 144, 255, 0.35);
}

.time {
  font-variant-numeric: tabular-nums;
  min-width: 100px;
  text-align: center;
  flex-shrink: 0;
  color: rgba(255, 255, 255, 0.85);
  display: none;
  /* 时间已移到进度条行 */
}

/* ============ 进度条（三层轨道 + 独特滑块） ============ */
.progress-container {
  position: relative;
  flex: 1;
  height: 24px;
  /* 更大的点击热区，避免误触 */
  display: flex;
  align-items: center;
  cursor: pointer;
  user-select: none;
}

.progress-track-bg,
.progress-buffer,
.progress-played {
  position: absolute;
  left: 0;
  top: 50%;
  transform: translateY(-50%);
  height: 5px;
  border-radius: 3px;
  transition: height 0.15s ease;
  pointer-events: none;
}

.progress-container:hover .progress-track-bg,
.progress-container:hover .progress-buffer,
.progress-container:hover .progress-played {
  height: 7px;
  /* 悬停时变粗，给用户反馈 */
}

.progress-track-bg {
  width: 100%;
  background: rgba(255, 255, 255, 0.18);
}

/* 缓冲层（灰色/淡色）—— 代表已经下载到的位置 */
.progress-buffer {
  background: rgba(255, 255, 255, 0.42);
  width: 0;
  transition: width 0.35s ease;
}

/* 已播放（蓝色渐变） */
.progress-played {
  background: linear-gradient(90deg, #1890ff 0%, #40a9ff 60%, #69c0ff 100%);
  width: 0;
  box-shadow: 0 0 6px rgba(64, 169, 255, 0.6);
  transition: width 0.12s linear;
}

/* 独特的当前播放位置滑块（白色核心 + 蓝色外圈 + 外部光晕） */
.progress-thumb {
  position: absolute;
  top: 50%;
  width: 14px;
  height: 14px;
  transform: translate(-50%, -50%) scale(0.85);
  pointer-events: none;
  transition: transform 0.15s ease;
  z-index: 2;
}

.progress-container:hover .progress-thumb {
  transform: translate(-50%, -50%) scale(1);
}

.thumb-core {
  position: absolute;
  inset: 4px;
  background: #fff;
  border-radius: 50%;
  box-shadow: 0 0 4px rgba(255, 255, 255, 0.9);
}

.thumb-ring {
  position: absolute;
  inset: 0;
  border-radius: 50%;
  border: 2px solid #40a9ff;
  box-shadow: 0 0 0 1px rgba(24, 144, 255, 0.5);
}

.thumb-halo {
  position: absolute;
  inset: -4px;
  border-radius: 50%;
  background: radial-gradient(circle, rgba(64, 169, 255, 0.35) 0%, transparent 70%);
  opacity: 0.9;
}

/* 播放中：外层光环轻微呼吸 */
.progress-thumb.playing .thumb-halo {
  animation: thumb-breathe 1.8s ease-in-out infinite;
}

@keyframes thumb-breathe {

  0%,
  100% {
    transform: scale(1);
    opacity: 0.6;
  }

  50% {
    transform: scale(1.2);
    opacity: 1;
  }
}

/* 透明 range input 覆盖在整个容器上 —— 只负责交互（拖动），不显示默认外观 */
.progress-slider {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  margin: 0;
  padding: 0;
  background: transparent;
  border: none;
  outline: none;
  appearance: none;
  -webkit-appearance: none;
  cursor: pointer;
  z-index: 3;
}

.progress-slider::-webkit-slider-runnable-track {
  background: transparent;
  height: 100%;
}

.progress-slider::-webkit-slider-thumb {
  -webkit-appearance: none;
  width: 16px;
  height: 16px;
  background: transparent;
  border: none;
  margin-top: 0;
  cursor: pointer;
}

.progress-slider::-moz-range-track {
  background: transparent;
}

.progress-slider::-moz-range-thumb {
  width: 16px;
  height: 16px;
  background: transparent;
  border: none;
  cursor: pointer;
}

/* 进度条悬停预览线 */
.progress-hover-line {
  position: absolute;
  top: 0;
  bottom: 0;
  width: 2px;
  background: rgba(255, 255, 255, 0.5);
  pointer-events: none;
  z-index: 1;
  transform: translateX(-50%);
}

/* 时间预览（悬停时显示） */
.progress-time-preview {
  position: absolute;
  bottom: 100%;
  transform: translateX(-50%);
  margin-bottom: 8px;
  pointer-events: none;
  z-index: 20;
  border-radius: 4px;
  background: rgba(0, 0, 0, 0.75);
  border: 1px solid rgba(255, 255, 255, 0.2);
  padding: 0;
  overflow: hidden;
  min-width: 56px;
  text-align: center;
}

/* 缩略图图片（B站风格） */
.progress-time-preview .preview-thumb-img {
  display: block;
  width: 160px;
  height: 90px;
  object-fit: cover;
  border-radius: 4px 4px 0 0;
}

.progress-time-preview .preview-time {
  display: block;
  text-align: center;
  font-size: 12px;
  font-weight: 500;
  color: #fff;
  font-variant-numeric: tabular-nums;
  padding: 2px 8px;
  background: rgba(0, 0, 0, 0.85);
}

/* 操作 OSD（快进/快退/倍速/音量 屏幕中央提示） */
.action-osd {
  position: absolute;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%);
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 12px 20px;
  background: rgba(0, 0, 0, 0.7);
  border-radius: 8px;
  pointer-events: none;
  z-index: 50;
  backdrop-filter: blur(4px);
}

.action-osd-icon {
  font-size: 20px;
}

.action-osd-text {
  font-size: 18px;
  font-weight: 600;
  color: #fff;
  font-variant-numeric: tabular-nums;
}

.osd-fade-enter-active,
.osd-fade-leave-active {
  transition: opacity 0.2s ease;
}

.osd-fade-enter-from,
.osd-fade-leave-to {
  opacity: 0;
}

/* ========= 音量组（垂直弹出滑块） ========= */
/* 总体策略：在 .volume-popup 中放一个"旋转容器"（120px 高），
   里面的 <input type="range"> 是水平的，宽 120px，旋转 90° 后
   变成高度 120px 的垂直滑块。百分比数字在下方固定显示。
   不依赖 padding 撑开，尺寸明确可靠。 */
.volume-group {
  position: relative;
  display: inline-flex;
  align-items: center;
}

/* 桥接区：仅覆盖按钮与 popup 之间的 6px 缝隙，不延伸到进度条区域 */
.volume-group::before {
  content: '';
  position: absolute;
  left: -4px;
  right: -4px;
  top: -8px;
  height: 10px;
  pointer-events: auto;
  z-index: 1;
}

.volume-group:hover .volume-popup,
.volume-popup.show {
  opacity: 1;
  pointer-events: auto;
  transform: translateX(-50%) translateY(0);
}

.volume-popup {
  position: absolute;
  bottom: calc(100% + 6px);
  left: 50%;
  transform: translateX(-50%) translateY(6px);
  width: 60px;
  height: 170px;
  background: rgba(20, 20, 20, 0.95);
  border: 1px solid rgba(255, 255, 255, 0.12);
  border-radius: 10px;
  padding: 12px 0 10px 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  opacity: 0;
  pointer-events: none;
  transition: opacity 0.18s ease, transform 0.18s ease;
  box-shadow: 0 6px 20px rgba(0, 0, 0, 0.55);
  z-index: 15;
}

.volume-slider-wrap {
  position: relative;
  width: 32px;
  height: 120px;
  /* 垂直滑块轨道高度 */
  display: flex;
  align-items: center;
  justify-content: center;
}

.volume-slider-v {
  width: 120px;
  height: 6px;
  -webkit-appearance: none;
  appearance: none;
  background: rgba(255,255,255,0.2);
  border-radius: 3px;
  outline: none;
  transform: rotate(-90deg);
  transform-origin: center center;
  cursor: pointer;
  accent-color: var(--accent, #1890ff);
}

.volume-slider-v::-webkit-slider-thumb {
  -webkit-appearance: none;
  width: 16px;
  height: 16px;
  border-radius: 50%;
  background: #1890ff;
  border: 2px solid #fff;
  box-shadow: 0 0 0 4px rgba(24, 144, 255, 0.18);
  cursor: pointer;
}

.volume-slider-v::-moz-range-thumb {
  width: 16px;
  height: 16px;
  border-radius: 50%;
  background: #1890ff;
  border: 2px solid #fff;
  cursor: pointer;
}

.volume-label {
  font-size: 11px;
  color: rgba(255, 255, 255, 0.85);
  font-variant-numeric: tabular-nums;
  min-width: 28px;
  text-align: center;
}

/* ========= 倍速组（带垂直弹出列表） ========= */
.speed-group {
  position: relative;
  display: inline-flex;
  align-items: center;
}

/* 桥接区：仅覆盖按钮与 popup 之间的缝隙，不延伸到进度条区域 */
.speed-group::before {
  content: '';
  position: absolute;
  left: -4px;
  right: -4px;
  top: -8px;
  height: 10px;
  pointer-events: auto;
  z-index: 1;
}

.speed-group:hover .speed-popup,
.speed-popup.show {
  opacity: 1;
  pointer-events: auto;
  transform: translateY(0);
}

.speed-btn {
  min-width: 52px;
  padding: 0 8px;
}

.speed-text {
  font-weight: 600;
  font-size: 12px;
  font-variant-numeric: tabular-nums;
}

.speed-popup {
  position: absolute;
  bottom: calc(100% + 10px);
  right: 0;
  background: rgba(20, 20, 20, 0.95);
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: 8px;
  padding: 6px;
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 92px;
  opacity: 0;
  pointer-events: none;
  transform: translateY(6px);
  transition: all 0.18s ease;
  box-shadow: 0 6px 20px rgba(0, 0, 0, 0.5);
  z-index: 10;
}

.speed-popup.show {
  opacity: 1;
  pointer-events: auto;
  transform: translateY(0);
}

.speed-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 6px;
  padding: 8px 12px;
  background: transparent;
  border: none;
  color: rgba(255, 255, 255, 0.75);
  font-size: 12px;
  font-family: inherit;
  border-radius: 6px;
  cursor: pointer;
  white-space: nowrap;
  transition: all 0.1s ease;
  font-variant-numeric: tabular-nums;
}

.speed-item:hover {
  background: rgba(24, 144, 255, 0.18);
  color: #fff;
}

.speed-item.active {
  background: rgba(24, 144, 255, 0.3);
  color: #40a9ff;
  font-weight: 600;
}

/* ========= 播放设置弹出面板 ========= */
.playback-settings-group {
  position: relative;
}

.playback-settings-popup {
  position: absolute;
  bottom: calc(100% + 10px);
  right: 0;
  background: rgba(20, 20, 20, 0.95);
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: 8px;
  padding: 6px;
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 180px;
  opacity: 0;
  pointer-events: none;
  transform: translateY(6px);
  transition: all 0.18s ease;
  box-shadow: 0 6px 20px rgba(0, 0, 0, 0.5);
  z-index: 10;
}

.playback-settings-popup.show {
  opacity: 1;
  pointer-events: auto;
  transform: translateY(0);
}

.playback-settings-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 12px;
  color: rgba(255, 255, 255, 0.75);
  font-size: 0.86rem;
  border-radius: 6px;
  gap: 12px;
}

.playback-settings-item:hover {
  background: rgba(255, 255, 255, 0.05);
}

.ps-toggle {
  position: relative;
  display: inline-flex;
  align-items: center;
  cursor: pointer;
}

.ps-toggle input {
  display: none;
}

.ps-switch {
  position: relative;
  width: 32px;
  height: 18px;
  background: rgba(255, 255, 255, 0.2);
  border-radius: 999px;
  transition: background 0.2s;
  flex-shrink: 0;
}

.ps-switch::after {
  content: '';
  position: absolute;
  top: 2px;
  left: 2px;
  width: 14px;
  height: 14px;
  background: #fff;
  border-radius: 50%;
  transition: transform 0.2s;
}

.ps-toggle input:checked~.ps-switch {
  background: #1890ff;
}

.ps-toggle input:checked~.ps-switch::after {
  transform: translateX(14px);
}

.ps-select {
  background: rgba(255, 255, 255, 0.1);
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: 4px;
  color: #fff;
  padding: 3px 6px;
  font-size: 0.79rem;
  font-family: inherit;
  cursor: pointer;
  outline: none;
}

.ps-select:hover {
  border-color: rgba(255, 255, 255, 0.3);
}

.ps-select option {
  background: #1a1a1a;
  color: #fff;
}

/* ========= 报告广告 ========= */
.report-ad-group {
  position: relative;
}

.report-ad-popup {
  position: absolute;
  bottom: calc(100% + 10px);
  right: 0;
  background: rgba(20, 20, 20, 0.95);
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: 8px;
  padding: 6px;
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 220px;
  max-width: 340px;
  opacity: 0;
  pointer-events: none;
  transform: translateY(6px);
  transition: all 0.18s ease;
  box-shadow: 0 6px 20px rgba(0, 0, 0, 0.5);
  z-index: 10;
}

.report-ad-popup.show {
  opacity: 1;
  pointer-events: auto;
  transform: translateY(0);
}

.report-ad-title {
  padding: 6px 12px 4px;
  color: rgba(255, 255, 255, 0.5);
  font-size: 0.75rem;
  user-select: none;
}

.report-ad-empty {
  padding: 8px 12px;
  color: rgba(255, 255, 255, 0.4);
  font-size: 0.82rem;
}

.report-ad-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 7px 12px;
  color: rgba(255, 255, 255, 0.8);
  font-size: 0.82rem;
  border-radius: 6px;
  cursor: pointer;
  background: none;
  border: none;
  width: 100%;
  text-align: left;
  font-family: inherit;
}

.report-ad-item:hover {
  background: rgba(255, 77, 77, 0.15);
  color: #ff6b6b;
}

.report-ad-domain {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.report-ad-toast {
  position: absolute;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%);
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 10px 18px;
  background: rgba(0, 0, 0, 0.85);
  color: #4ade80;
  border-radius: 10px;
  font-size: 0.85rem;
  z-index: 25;
  pointer-events: none;
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.4);
}

/* ========= B 站风格：底部左侧继续播放小提示 ========= */
.resume-bili {
  position: absolute;
  left: 12px;
  bottom: 52px;
  /* 放在控制条上方 */
  display: inline-flex;
  align-items: center;
  gap: 10px;
  padding: 6px 12px;
  background: rgba(0, 0, 0, 0.72);
  color: #fff;
  border-radius: 8px;
  font-size: 12px;
  z-index: 14;
  border: 1px solid rgba(255, 255, 255, 0.08);
  animation: slideInLeft 0.3s ease;
}

@keyframes slideInLeft {
  from {
    opacity: 0;
    transform: translateY(6px);
  }

  to {
    opacity: 1;
    transform: translateY(0);
  }
}

.resume-bili-text b {
  color: #40a9ff;
  font-weight: 600;
  margin: 0 2px;
}

.resume-bili-link {
  background: transparent;
  color: #40a9ff;
  border: none;
  padding: 2px 6px;
  border-radius: 4px;
  font-size: 12px;
  cursor: pointer;
  font-family: inherit;
  transition: all 0.15s;
}

.resume-bili-link:hover {
  background: rgba(24, 144, 255, 0.2);
  color: #fff;
}

.resume-bili-dismiss {
  color: rgba(255, 255, 255, 0.65);
}

.resume-bili-dismiss:hover {
  background: rgba(255, 255, 255, 0.08);
  color: #fff;
}

/* ========= 画质切换 toast（左下角 1.5 秒提示） ========= */
.quality-toast {
  position: absolute;
  left: 12px;
  bottom: 90px;
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 6px 14px;
  background: rgba(0, 0, 0, 0.78);
  color: #fff;
  border-radius: 8px;
  font-size: 13px;
  z-index: 30;
  pointer-events: none;
  animation: quality-toast-in 0.3s ease;
  border-left: 3px solid #40a9ff;
}

@keyframes quality-toast-in {
  from {
    opacity: 0;
    transform: translateX(-10px);
  }

  to {
    opacity: 1;
    transform: translateX(0);
  }
}

/* ========= 音量 toast（键盘调节音量 1 秒显示） ========= */
.volume-toast {
  position: absolute;
  left: 50%;
  top: 50%;
  transform: translate(-50%, -50%);
  background: rgba(0, 0, 0, 0.55);
  color: #fff;
  padding: 14px 20px;
  border-radius: 10px;
  pointer-events: none;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
  min-width: 200px;
  z-index: 20;
  backdrop-filter: blur(6px);
  box-shadow: 0 4px 20px rgba(0, 0, 0, 0.4);
}

.volume-toast-bar {
  width: 100%;
  height: 6px;
  background: rgba(255, 255, 255, 0.2);
  border-radius: 3px;
  overflow: hidden;
}

.volume-toast-fill {
  height: 100%;
  background: #1890ff;
  transition: width 0.18s ease;
}

.volume-toast-text {
  font-size: 16px;
  font-weight: 600;
  font-variant-numeric: tabular-nums;
}

.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.2s ease;
}

.fade-enter-from,
.fade-leave-to {
  opacity: 0;
}

/* ========= 画质选择器 ========= */
.quality-group {
  flex-shrink: 0;
  margin: 0 2px;
}

.quality-group :deep(.select-dropdown) {
  min-width: 100px;
}

.quality-group :deep(.select-trigger) {
  background: rgba(255, 255, 255, 0.08);
  border-color: rgba(255, 255, 255, 0.12);
  color: rgba(255, 255, 255, 0.75);
  font-size: 11px;
  padding: 3px 8px;
  min-height: 26px;
  border-radius: 4px;
}

.quality-group :deep(.select-trigger:hover) {
  border-color: rgba(24, 144, 255, 0.5);
  color: #fff;
  background: rgba(24, 144, 255, 0.15);
}

.quality-group :deep(.select-panel) {
  background: rgba(20, 20, 20, 0.96);
  border-color: rgba(255, 255, 255, 0.1);
  font-size: 12px;
  border-radius: 6px;
  min-width: 120px;
}

.quality-group :deep(.option) {
  color: rgba(255, 255, 255, 0.75);
  padding: 6px 10px;
  font-size: 12px;
}

.quality-group :deep(.option:hover) {
  background: rgba(24, 144, 255, 0.15);
  color: #fff;
}

.quality-group :deep(.option.is-selected) {
  background: rgba(24, 144, 255, 0.25);
  color: #40a9ff;
}

/* ========= 画质增强提示弹窗 ========= */
.ai-warning-overlay {
  position: absolute;
  inset: 0;
  z-index: 100;
  background: rgba(0, 0, 0, 0.7);
  display: flex;
  align-items: center;
  justify-content: center;
  backdrop-filter: blur(4px);
  animation: fadeIn 0.2s ease;
}

@keyframes fadeIn {
  from {
    opacity: 0;
  }

  to {
    opacity: 1;
  }
}

.ai-warning-dialog {
  background: rgba(28, 28, 36, 0.98);
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: 10px;
  padding: 24px;
  max-width: 440px;
  width: 90%;
  box-shadow: 0 12px 48px rgba(0, 0, 0, 0.5);
  color: #fff;
}

.ai-warning-header {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 17px;
  font-weight: 700;
  margin-bottom: 16px;
  color: #ffa940;
}

.ai-warning-icon {
  font-size: 22px;
}

.ai-warning-body {
  font-size: 13px;
  color: rgba(255, 255, 255, 0.75);
  line-height: 1.6;
}

.ai-warning-body p {
  margin: 0 0 8px;
}

.ai-warning-body ul {
  margin: 0 0 12px;
  padding-left: 20px;
}

.ai-warning-body li {
  margin-bottom: 6px;
}

.ai-warning-body b {
  color: #ffa940;
  font-weight: 600;
}

.ai-warning-note {
  font-size: 12px;
  color: rgba(255, 255, 255, 0.5);
  margin-top: 8px;
}

.ai-warning-footer {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
  margin-top: 20px;
  padding-top: 16px;
  border-top: 1px solid rgba(255, 255, 255, 0.08);
}

.ai-warning-btn {
  padding: 8px 20px;
  border-radius: 4px;
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  border: none;
  font-family: inherit;
  transition: all 0.15s ease;
}

.ai-warning-btn--cancel {
  background: rgba(255, 255, 255, 0.08);
  color: rgba(255, 255, 255, 0.6);
}

.ai-warning-btn--cancel:hover {
  background: rgba(255, 255, 255, 0.15);
  color: #fff;
}

.ai-warning-btn--confirm {
  background: #ff7a00;
  color: #fff;
}

.ai-warning-btn--confirm:hover {
  background: #ff9426;
  transform: translateY(-1px);
}

/* ========= 模态框通用 ========= */
.modal-fade-enter-active,
.modal-fade-leave-active {
  transition: opacity 0.2s ease;
}

.modal-fade-enter-active .vp-modal,
.modal-fade-leave-active .vp-modal {
  transition: transform 0.2s ease, opacity 0.2s ease;
}

.modal-fade-enter-from,
.modal-fade-leave-to {
  opacity: 0;
}

.modal-fade-enter-from .vp-modal,
.modal-fade-leave-to .vp-modal {
  transform: scale(0.95) translateY(10px);
  opacity: 0;
}

.vp-modal-overlay {
  position: absolute;
  inset: 0;
  background: rgba(0, 0, 0, 0.6);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 100;
}

.vp-modal {
  background: rgba(28, 28, 32, 0.97);
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: 14px;
  min-width: 320px;
  max-width: 90vw;
  width: 440px;
  box-shadow: 0 12px 40px rgba(0, 0, 0, 0.7);
  overflow: hidden;
  display: flex;
  flex-direction: column;
  max-height: 85vh;
}

.vp-modal-info {
  max-width: 480px;
  min-width: 360px;
  width: 480px;
}

.vp-modal-shortcut {
  max-width: 90vw;
  min-width: 380px;
  width: 520px;
}

.vp-modal-shortcut .vp-modal-body {
  max-height: calc(85vh - 140px);
}

.vp-modal-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 14px 18px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.08);
  font-size: 15px;
  font-weight: 600;
  color: #fff;
}

.vp-modal-close {
  width: 28px;
  height: 28px;
  border-radius: 6px;
  border: none;
  background: transparent;
  color: rgba(255, 255, 255, 0.5);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: all 0.15s;
}

.vp-modal-close:hover {
  background: rgba(255, 255, 255, 0.1);
  color: #fff;
}

.vp-modal-body {
  padding: 14px 18px;
  max-height: calc(85vh - 120px);
  overflow-y: auto;
  scrollbar-width: thin;
  scrollbar-color: rgba(255, 255, 255, 0.12) transparent;
}

.vp-modal-body::-webkit-scrollbar {
  width: 5px;
}

.vp-modal-body::-webkit-scrollbar-thumb {
  background: rgba(255, 255, 255, 0.12);
  border-radius: 3px;
}

.vp-modal-footer {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  padding: 12px 18px;
  border-top: 1px solid rgba(255, 255, 255, 0.08);
}

.vp-btn-primary,
.vp-btn-secondary {
  padding: 6px 16px;
  border-radius: 8px;
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  border: 1px solid transparent;
  transition: all 0.15s;
  font-family: inherit;
}

.vp-btn-primary {
  background: var(--accent, #1890ff);
  color: #fff;
}

.vp-btn-primary:hover {
  filter: brightness(1.1);
}

.vp-btn-secondary {
  background: rgba(255, 255, 255, 0.08);
  color: rgba(255, 255, 255, 0.7);
  border-color: rgba(255, 255, 255, 0.12);
}

.vp-btn-secondary:hover {
  background: rgba(255, 255, 255, 0.14);
  color: #fff;
}

.vp-modal-hint {
  font-size: 12px;
  color: rgba(255, 255, 255, 0.45);
  margin: 0 0 12px;
  line-height: 1.5;
}

.vp-empty-text {
  text-align: center;
  color: rgba(255, 255, 255, 0.4);
  padding: 20px 0;
  font-size: 13px;
}

/* 视频信息 - 网格布局 */
.vp-info-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 10px 20px;
}

.vp-info-item {
  display: flex;
  flex-direction: column;
  gap: 3px;
}

.vp-info-label {
  font-size: 11px;
  color: rgba(255, 255, 255, 0.4);
  text-transform: uppercase;
  letter-spacing: 0.3px;
}

.vp-info-val {
  font-size: 14px;
  color: #fff;
  font-weight: 500;
  font-variant-numeric: tabular-nums;
  word-break: break-all;
}

.vp-info-val.vp-info-warn {
  color: #ff6b6b;
}

.vp-info-val.vp-info-highlight {
  color: #40a9ff;
  font-weight: 600;
}

.vp-info-val.vp-info-mono {
  font-size: 12px;
  font-family: 'Consolas', 'Monaco', monospace;
}

.vp-info-val.vp-info-url {
  font-size: 11px;
  color: rgba(255, 255, 255, 0.6);
  line-height: 1.4;
}

.vp-info-item.vp-info-full {
  grid-column: 1 / -1;
}

.vp-info-section-title {
  font-size: 12px;
  font-weight: 600;
  color: rgba(255, 255, 255, 0.55);
  text-transform: uppercase;
  letter-spacing: 0.5px;
  margin: 16px 0 8px;
  padding-bottom: 4px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.06);
}

.vp-info-section-title:first-child {
  margin-top: 0;
}

/* 快捷键列表 */
.vp-shortcut-list {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.vp-shortcut-row {
  display: flex;
  align-items: center;
  padding: 8px 12px;
  border-radius: 8px;
  background: rgba(255, 255, 255, 0.03);
  border: 1px solid rgba(255, 255, 255, 0.06);
  gap: 10px;
  transition: border-color 0.15s;
}

.vp-shortcut-row:hover {
  border-color: rgba(255, 255, 255, 0.12);
}

.vp-sc-label {
  font-size: 13px;
  font-weight: 500;
  color: #fff;
  min-width: 55px;
}

.vp-sc-desc {
  font-size: 11px;
  color: rgba(255, 255, 255, 0.4);
  flex: 1;
  min-width: 0;
}

.vp-sc-actions {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-shrink: 0;
}

.vp-sc-btn {
  padding: 4px 12px;
  border-radius: 6px;
  border: 1px solid rgba(255, 255, 255, 0.14);
  background: rgba(255, 255, 255, 0.06);
  color: rgba(255, 255, 255, 0.7);
  font-size: 12px;
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  gap: 4px;
  min-width: 65px;
  justify-content: center;
  transition: all 0.15s;
  font-family: inherit;
}

.vp-sc-btn:hover {
  border-color: rgba(255, 255, 255, 0.28);
  background: rgba(255, 255, 255, 0.1);
}

.vp-sc-btn.editing {
  border-color: var(--accent, #1890ff);
  color: var(--accent, #1890ff);
  animation: vp-blink 0.8s ease infinite;
}

@keyframes vp-blink {

  0%,
  100% {
    opacity: 1;
  }

  50% {
    opacity: 0.5;
  }
}

.vp-sc-key {
  display: inline-block;
  padding: 2px 7px;
  background: rgba(255, 255, 255, 0.08);
  border: 1px solid rgba(255, 255, 255, 0.18);
  border-radius: 4px;
  font-size: 11px;
  font-weight: 600;
  line-height: 1.4;
}

.vp-sc-key+.vp-sc-key {
  margin-left: 3px;
}

.vp-sc-none {
  color: rgba(255, 255, 255, 0.3);
  font-size: 11px;
}

.vp-sc-recording {
  font-size: 12px;
  color: var(--accent, #1890ff);
}

.vp-sc-reset {
  width: 26px;
  height: 26px;
  border-radius: 6px;
  border: none;
  background: transparent;
  color: rgba(255, 255, 255, 0.3);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: all 0.15s;
  opacity: 0;
}

.vp-shortcut-row:hover .vp-sc-reset {
  opacity: 1;
}

.vp-sc-reset:hover {
  color: var(--accent, #1890ff);
  background: rgba(255, 255, 255, 0.08);
}

/* 播放设置弹窗 - 分隔线 + 快捷键入口 */
.playback-settings-divider {
  height: 1px;
  background: rgba(255, 255, 255, 0.08);
  margin: 4px 0;
}

.playback-settings-link {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 10px;
  border-radius: 6px;
  border: none;
  background: transparent;
  color: rgba(255, 255, 255, 0.7);
  font-size: 13px;
  cursor: pointer;
  transition: background 0.12s;
  font-family: inherit;
  width: 100%;
  text-align: left;
}

.playback-settings-link:hover {
  background: rgba(255, 255, 255, 0.08);
  color: #fff;
}

.playback-settings-link span {
  flex: 1;
}
</style>
