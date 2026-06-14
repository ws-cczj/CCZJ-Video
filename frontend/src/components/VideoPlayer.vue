<script setup lang="ts">
import { ref, nextTick, onMounted, onBeforeUnmount, watch, computed } from 'vue'
import Icon from './Icon.vue'
import SelectDropdown from './SelectDropdown.vue'
import { TsCache } from '../utils/tsCache'
import { AiUpscaler, checkUpscalerSupport } from '../utils/aiUpscaler'
import {
  WindowIsFs, WindowIsMax, WindowSetFullscreen, WindowToggleMax } from '../../bindings/cczjVideo/app'

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

const emit = defineEmits(['back', 'prev', 'next', 'toggleFavorite'])

const wrapperRef = ref<HTMLDivElement>()
const errorMsg = ref('')

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

// 弹出面板状态（音量和倍速）
const showVolumePanel = ref(false)
const showSpeedPanel = ref(false)
const speedOptions = [0.5, 0.75, 1, 1.25, 1.5, 2]

// ========= 画质模式 =========
type QualityMode = 'original' | 'ai_frame_interp'
const qualityMode = ref<QualityMode>(
  (readSetting('quality_mode', 'original') as QualityMode) || 'original'
)
const qualityOptions = [
  { value: 'original', label: '原高清' },
  { value: 'ai_frame_interp', label: 'AI 补帧' },
]
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

// AI 补帧管线（WebGL2 实时增强）
let upscaler: AiUpscaler | null = null
let upscalerStatsTimer: ReturnType<typeof setInterval> | null = null
const upscalerSupported = ref(false)
const upscalerStats = ref<{ fps: number; gpuEnabled: boolean }>({ fps: 0, gpuEnabled: false })
let _aiReady = false // 视频是否已就绪（loadedmetadata 之后），AI 才会启动

function onQualityChange(value: string | number): void {
  const mode = String(value) as QualityMode
  if (mode === 'ai_frame_interp' && !aiWarningAccepted.value) {
    // 首次开启 AI 补帧 → 弹出警告
    showAiWarning.value = true
    return
  }
  applyQualityMode(mode)
}

function applyQualityMode(mode: QualityMode): void {
  qualityMode.value = mode
  writeSetting('quality_mode', mode)
  if (mode === 'ai_frame_interp') {
    if (_aiReady) {
      startAiPipeline()
    }
    showQualityToast('已切换至 AI 补帧（GPU 实时增强）')
  } else {
    stopAiPipeline()
    showQualityToast('已切换至原高清')
  }
}

function confirmAiMode(): void {
  aiWarningAccepted.value = true
  writeSetting('ai_warning_accepted', '1')
  showAiWarning.value = false
  applyQualityMode('ai_frame_interp')
}

function cancelAiMode(): void {
  showAiWarning.value = false
  qualityMode.value = 'original'
  writeSetting('quality_mode', 'original')
}

// AI 增强管线：使用 AiUpscaler (WebGL2) 实时处理视频帧
async function startAiPipeline(): Promise<void> {
  if (upscaler) return // 已在运行

  const v = getVideoEl()
  if (!v) return

  // 检查支持情况
  const support = checkUpscalerSupport()
  upscalerSupported.value = support.webgl2 || support.webgpu

  if (!support.webgl2) {
    console.warn('[Player] WebGL2 不可用，AI 增强无法启动:', support.message)
    qualityMode.value = 'original'
    writeSetting('quality_mode', 'original')
    return
  }

  upscaler = new AiUpscaler({
    upscale: 1.0,         // 不上采样，保持原分辨率
    sharpness: 0.6,       // 锐化强度
    contrast: 0.3,        // 对比度增强
    deband: 0.4,          // 去色带
    edgeEnhance: 0.5,     // 边缘增强
  })

  const ok = await upscaler.init(v, wrapperRef.value ?? undefined)
  if (!ok) {
    console.error('[Player] AI 增强器初始化失败:', upscaler.error)
    upscaler.destroy()
    upscaler = null
    // 自动回退到原高清
    qualityMode.value = 'original'
    writeSetting('quality_mode', 'original')
    return
  }

  upscaler.start()
  console.log('[Player] 🎨 AI 增强管线已启动 (WebGL2 GPU 加速)')

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

// ⭐ 2026-06-09：简化——全部使用 localStorage 读写，避免依赖 Go 端的 GetSetting/SetSetting 签名差异。
//    （Wails 下 `go.main.App.GetSetting(key)` 需要传 1 个参数；之前漏传会导致
//    "received 0 arguments to method 'main.App.GetSetting', expected 1" 的控制台告警。）
function readSetting(key: string, def: string): string {
  try {
    const v = localStorage.getItem('vp_' + key)
    return v != null ? v : def
  } catch { return def }
}
function writeSetting(key: string, val: string): void {
  try { localStorage.setItem('vp_' + key, val) } catch { /* ignore */ }
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
        console.warn('[Player] play 被拒绝:', err?.name || err, 'auto=', auto)
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
      let parsed: { urls: string[], variantUrls: string[], targetduration: number, isMaster: boolean }
      try {
        parsed = await TsCache.fetchAndParseM3u8(url)
      } catch {
        parsed = { urls: [], variantUrls: [], targetduration: 6, isMaster: false }
      }

      // 判断是否为单码率（media playlist），如是则立即设置 segments 列表
      const isMediaPlaylist = !parsed.isMaster && parsed.urls.length > 0 && parsed.urls[0].toLowerCase().match(/\.(ts|aac|mp4|m4s)(\?|$)/)
      if (isMediaPlaylist) {
        TsCache.setSegments(parsed.urls)
        TsCache.setTargetDuration(parsed.targetduration)
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
      ;(video as any).__hls = hls

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
          switch (data.type) {
            case Hls.ErrorTypes.NETWORK_ERROR:
              console.log('[Player] 网络错误，尝试恢复 startLoad()')
              try { hls.startLoad() } catch (e) { console.warn('[Player] startLoad 失败:', e) }
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
    video.src = url
    if (props.autoplay !== false) safePlay(true)
    video.onerror = () => {
      errorMsg.value = '视频加载失败，请检查链接或网络'
    }
  }
}

function bindCommonVideoEvents(video: HTMLVideoElement): void {
  if ((video as any).__eventsBound) return
  ;(video as any).__eventsBound = true

  video.addEventListener('play', () => { playing.value = true; loading.value = false; videoReady.value = true })
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
    updateBuffer()
    console.log(`[Player] loadedmetadata: duration=${video.duration.toFixed(1)}s, volume=${video.volume.toFixed(2)}`)
    // ⭐ 视频元数据就绪后，根据用户保存的画质模式自动启动 AI 管线
    _aiReady = true
    if (qualityMode.value === 'ai_frame_interp') {
      // 延迟启动：确保视频帧已可供 WebGL 读取
      setTimeout(() => { startAiPipeline() }, 100)
    }
  })
  video.addEventListener('progress', updateBuffer)
  video.addEventListener('seeking', updateBuffer)
  video.addEventListener('seeked', updateBuffer)
  video.addEventListener('waiting', () => { loading.value = true })
  video.addEventListener('canplay', () => {
    loading.value = false
    videoReady.value = true
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
  })
  video.addEventListener('ratechange', () => { speed.value = video.playbackRate })
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
  try {
    const hls = (video as any).__hls
    if (hls) {
      try { hls.destroy() } catch { /* ignore */ }
      ;(video as any).__hls = null
    }
    try { video.pause() } catch { /* ignore */ }
    try { video.removeAttribute('src') } catch { /* ignore */ }
    try { video.removeAttribute('data-autoplay-done') } catch { /* ignore */ }
  } catch { /* ignore */ }
  if (cacheStatsTimer != null) { window.clearInterval(cacheStatsTimer); cacheStatsTimer = null }
  if (_saveTimer != null) { window.clearInterval(_saveTimer); _saveTimer = null }
  if (_resumeTimer != null) { window.clearInterval(_resumeTimer); _resumeTimer = null }
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

// 滚轮调整音量：向上=+5%，向下=-5%，同时显示音量 Toast
function onWheel(e: WheelEvent): void {
  const v = getVideoEl()
  if (!v) return
  const delta = e.deltaY < 0 ? 0.05 : -0.05
  v.volume = Math.max(0, Math.min(1, v.volume + delta))
  if (v.volume > 0 && v.muted) { v.muted = false }
  if (v.volume === 0 && !v.muted) { v.muted = true }
  volume.value = v.volume
  muted.value = v.muted
  keepVisible()
  showVolumeToastRef()
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
  showVolumeToastRef()
}
function showVolumeToastRef(): void {
  const v = getVideoEl()
  if (!v) return
  toastVolumePct.value = Math.round(v.volume * 100)
  showVolumeToast.value = true
  if (_toastTimer != null) window.clearTimeout(_toastTimer)
  _toastTimer = window.setTimeout(() => { showVolumeToast.value = false }, 1000)
}

function setVolume(val: number): void {
  const v = getVideoEl()
  if (!v) return
  val = Math.max(0, Math.min(1, val))
  v.volume = val
  volume.value = val
  if (val > 0 && v.muted) { v.muted = false; muted.value = false }
  if (val === 0) { v.muted = true; muted.value = true }
}

const progressContainerRef = ref<HTMLDivElement>()
const progressHoverPct = ref(-1) // 鼠标悬停在进度条上的百分比位置（-1 表示不显示）
const progressThumbnailImg = ref('') // 缩略图预览

function seek(e: Event): void {
  const v = getVideoEl()
  if (!v) return
  const target = e.target as HTMLInputElement
  const val = parseFloat(target.value)
  if (!isFinite(val)) return
  v.currentTime = val
  current.value = val
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
  // ⭐ 使用 setTimeout 确保在 range input 的 input 事件之后执行，用鼠标坐标覆盖更精确的跳转位置
  setTimeout(() => {
    v.currentTime = newTime
    current.value = newTime
  }, 0)
}

// 进度条悬停：计算百分比位置 + 捕获缩略图
let _thumbDebounceTimer: ReturnType<typeof setTimeout> | null = null
let _pendingThumbTime = -1

function onProgressHover(e: MouseEvent): void {
  if (!progressContainerRef.value) return
  const rect = progressContainerRef.value.getBoundingClientRect()
  const pct = Math.max(0, Math.min(100, ((e.clientX - rect.left) / rect.width) * 100))
  progressHoverPct.value = pct

  // 捕获缩略图（防抖 200ms）
  const v = getVideoEl()
  if (!v || !v.duration || v.readyState < 2) return
  const targetTime = (pct / 100) * v.duration
  if (Math.abs(targetTime - _pendingThumbTime) < 0.5) return
  _pendingThumbTime = targetTime
  if (_thumbDebounceTimer) clearTimeout(_thumbDebounceTimer)
  _thumbDebounceTimer = setTimeout(() => {
    captureThumbnail(targetTime)
    _thumbDebounceTimer = null
  }, 200)
}

function captureThumbnail(time: number): void {
  const v = getVideoEl()
  if (!v || !v.duration || v.readyState < 2) { progressThumbnailImg.value = ''; return }
  try {
    const canvas = document.createElement('canvas')
    canvas.width = 160
    canvas.height = 90
    const ctx = canvas.getContext('2d')
    if (!ctx) { progressThumbnailImg.value = ''; return }
    // 在 video 上 seek 到指定时间会中断播放，此处用已加载的帧近似
    // 如果 video 已加载该位置附近的数据，用 buffered 检查
    let hasBuffer = false
    for (let i = 0; i < v.buffered.length; i++) {
      if (time >= v.buffered.start(i) && time <= v.buffered.end(i)) {
        hasBuffer = true
        break
      }
    }
    if (hasBuffer) {
      // 临时暂停然后 seek 去截图
      const wasPlaying = !v.paused
      if (wasPlaying) v.pause()
      const origTime = v.currentTime
      const onSeeked = () => {
        v.removeEventListener('seeked', onSeeked)
        try {
          ctx.drawImage(v, 0, 0, canvas.width, canvas.height)
          progressThumbnailImg.value = canvas.toDataURL('image/jpeg', 0.7)
        } catch {
          progressThumbnailImg.value = ''
        }
        if (wasPlaying) v.play().catch(() => {})
        v.currentTime = origTime
      }
      v.addEventListener('seeked', onSeeked)
      v.currentTime = time
    }
  } catch {
    progressThumbnailImg.value = ''
  }
}

function changeSpeed(s: number): void {
  const v = getVideoEl()
  if (v) {
    v.playbackRate = s
    speed.value = s
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
        } catch {}
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

// ------ 控制条显示/隐藏 ------
// 规则：
//   · mousemove → 显示 + 启动 2.5 秒隐藏定时器（每次移动都重置）
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
    }, 2500)
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
    if (!isInDropdown && !showVolumePanel.value && !showSpeedPanel.value) {
      toggleShow(false)
    }
  }, 150)
}

// 新工具：供键盘/点击调用（只刷新"可见 2.5 秒"，不会误切换显示状态）
function keepVisible(): void {
  showControls.value = true
  if (hideTimer !== null) {
    window.clearTimeout(hideTimer)
    hideTimer = null
  }
  hideTimer = window.setTimeout(() => {
    showControls.value = false
  }, 2500)
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
  setTimeout(() => { wrapperRef.value?.focus?.() }, 100)
})

onBeforeUnmount(() => {
  destroyPlayer()
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
    } catch {}
  }
  // 2) 如果 Wails 仍在系统级全屏（历史遗留状态）→ 强制退出
  if ((window as any).go) {
    try {
      WindowIsFs().then((fs: boolean) => {
        if (fs) {
          try { WindowSetFullscreen(false) } catch {}
        }
      }).catch(() => {})
    } catch {}
  }
  document.body.removeAttribute('data-player-fullscreen')
})

watch(() => props.url, (newUrl) => {
  if (!newUrl) return
  setTimeout(setupPlayer, 0)
})
</script>

<template>
  <div
    class="player-wrapper"
    :class="{ fullscreen: isFullscreen, 'cursor-hidden': playing && !showControls }"
    ref="wrapperRef"
    tabindex="0"
    @mousemove="toggleShow(true); mouseInside = true"
    @mouseenter="mouseInside = true"
    @mouseleave="onMouseLeave"
    @click="onWrapperClick"
    @dblclick.stop="toggleFullscreen()"
    @wheel.prevent="onWheel"
  >
    <!-- 顶部栏：标题 + 缓存统计 + 收藏按钮 -->
    <div v-if="showTitleBar && (showControls || !playing)" class="player-title-bar" @click.stop @dblclick.stop>
      <span class="player-title">{{ title || url }}</span>
      <span
        v-if="isHls(url) && cacheStats.totalSegments > 0"
        class="cache-info"
        :title="`命中: ${cacheStats.hits} 未命中: ${cacheStats.misses} 预取: ${cacheStats.entries}/${cacheStats.totalSegments} 片`"
      >
        预取 {{ cacheStats.entries }}/{{ cacheStats.totalSegments }} ·
        命中 {{ (cacheStats.hitRate * 100).toFixed(0) }}%
      </span>
      <button
        class="fav-btn-in-player"
        :class="{ 'is-fav': isFav }"
        :disabled="favBusy"
        :title="isFav ? '取消收藏' : '加入收藏'"
        @click.stop="emit('toggleFavorite')"
      >
        {{ isFav ? '★' : '☆' }}
      </button>
    </div>

    <!-- 拖拽遮罩条：鼠标经过视频顶部时出现，可拖拽移动窗口 -->
    <div
      v-if="mouseInside"
      class="player-drag-handle"
      title="拖拽移动窗口"
      style="--wails-draggable: drag"
      @click.stop
      @dblclick.stop
    />

    <video class="native-video" playsinline preload="auto" @click.stop="togglePlay"></video>

    <!-- 音量 toast：键盘调节音量时显示 1 秒 -->
    <transition name="fade">
      <div v-if="showVolumeToast" class="volume-toast">
        <div class="volume-toast-bar">
          <div class="volume-toast-fill" :style="{ width: toastVolumePct + '%' }"></div>
        </div>
        <span class="volume-toast-text">{{ toastVolumePct }}%</span>
      </div>
    </transition>

    <div v-if="loading" class="loading">
      <div class="spinner"></div>
    </div>

    <div v-if="errorMsg" class="player-error">
      <span>⚠</span>
      <span>{{ errorMsg }}</span>
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
    <div v-if="qualityToastText" class="quality-toast">
      <span>{{ qualityToastText }}</span>
    </div>

    <!-- AI 补帧警告弹窗 -->
    <div v-if="showAiWarning" class="ai-warning-overlay" @click.stop>
      <div class="ai-warning-dialog">
        <div class="ai-warning-header">
          <span class="ai-warning-icon">⚡</span>
          <span>AI 补帧模式</span>
        </div>
        <div class="ai-warning-body">
          <p>开启 AI 补帧模式将消耗额外的系统资源：</p>
          <ul>
            <li><b>GPU 计算负载</b> — 将使用本地 GPU 实时处理视频帧，可能导致显卡温度升高和风扇加速</li>
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

    <!-- 底部控制条 -->
    <div class="ctrl-bar" v-show="showControls || !playing" @click.stop @mousedown.stop @pointerdown.stop @dblclick.stop>
      <!-- 上一集 -->
      <button
        class="ctrl-btn"
        @click="emit('prev')"
        :disabled="!hasPrev"
        title="上一集"
      >
        <Icon name="prev" :size="16" />
      </button>

      <!-- 播放/暂停（中间） -->
      <button class="ctrl-btn play-btn" @click="togglePlay" :title="playing ? '暂停' : '播放'">
        <Icon :name="playing ? 'pause' : 'play'" :size="18" />
      </button>

      <!-- 下一集 -->
      <button
        class="ctrl-btn"
        @click="emit('next')"
        :disabled="!hasNext"
        title="下一集"
      >
        <Icon name="next" :size="16" />
      </button>

      <span class="time">{{ fmt(current) }} / {{ fmt(duration) }}</span>

      <!-- 进度条：三层结构（背景 / 缓冲 / 已播放）+ 透明 range input 提供拖动交互 -->
      <div
        class="progress-container"
        ref="progressContainerRef"
        @mousedown.stop="onProgressMouseDown"
        @mousemove="onProgressHover"
        @mouseleave="progressHoverPct = -1"
      >
        <div class="progress-track-bg"></div>
        <div class="progress-buffer" :style="{ width: bufferPct + '%' }"></div>
        <div class="progress-played" :style="{ width: progressPct + '%' }"></div>
        <!-- 悬停预览线 -->
        <div
          v-if="progressHoverPct >= 0"
          class="progress-hover-line"
          :style="{ left: progressHoverPct + '%' }"
        ></div>
        <!-- 缩略图预览 -->
        <div
          v-if="progressHoverPct >= 0 && progressThumbnailImg"
          class="progress-thumbnail-preview"
          :style="{ left: progressHoverPct + '%' }"
        >
          <img :src="progressThumbnailImg" />
          <span class="preview-time">{{ fmt((progressHoverPct / 100) * (duration || 0)) }}</span>
        </div>
        <!-- 当前播放位置的"独特"指示点（白色内圆 + 蓝色光晕 + 外圈） -->
        <div
          class="progress-thumb"
          :style="{ left: progressPct + '%' }"
          :class="{ playing: playing }"
        >
          <span class="thumb-halo"></span>
          <span class="thumb-core"></span>
          <span class="thumb-ring"></span>
        </div>
        <input
          class="progress-slider"
          type="range"
          min="0"
          :max="duration || 0"
          step="0.1"
          :value="current"
          @input="seek"
        />
      </div>

      <!-- 画质选择 -->
      <div class="quality-group" @click.stop>
        <SelectDropdown
          :model-value="qualityMode"
          :options="qualityOptions"
          size="sm"
          @change="onQualityChange"
        />
      </div>

      <!-- 音量 + 垂直滑块弹出（纯 CSS hover；鼠标从图标移动到滑块不会消失） -->
      <div class="volume-group" @click.stop>
        <button
          class="ctrl-btn"
          @click.stop="toggleMute(); keepVisible()"
          :title="muted ? '取消静音' : '静音（M）'"
        >
          <Icon :name="muted ? 'volume-off' : 'volume'" :size="16" />
        </button>
        <div class="volume-popup" :class="{ show: showVolumePanel }" @click.stop>
          <div class="volume-slider-wrap">
            <input
              class="volume-slider-v"
              type="range"
              min="0"
              max="1"
              step="0.05"
              :value="muted ? 0 : volume"
              @input="onVolumeInput"
              @change="keepVisible()"
            />
          </div>
          <span class="volume-label">{{ Math.round((muted ? 0 : volume) * 100) }}</span>
        </div>
      </div>

      <!-- 倍速按钮 + 弹出垂直列表（hover 显示，点击切换） -->
      <div class="speed-group" @click.stop>
        <button
          class="ctrl-btn speed-btn"
          @click="showSpeedPanel = !showSpeedPanel; showVolumePanel = false; keepVisible()"
          title="播放速度"
        >
          <span class="speed-text">{{ speed }}x</span>
          <Icon name="chevron-down" :size="10" />
        </button>
        <div
          class="speed-popup"
          :class="{ show: showSpeedPanel }"
        >
          <button
            v-for="s in speedOptions"
            :key="s"
            class="speed-item"
            :class="{ active: speed === s }"
            @click="changeSpeed(s)"
          >
            {{ s }}x
            <Icon v-if="speed === s" name="check" :size="12" />
          </button>
        </div>
      </div>

      <!-- 全屏 -->
      <button class="ctrl-btn" @click="toggleFullscreen" :title="isFullscreen ? '退出全屏' : '全屏（F）'">
        <Icon :name="isFullscreen ? 'exit-fullscreen' : 'fullscreen'" :size="16" />
      </button>
    </div>
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
  cursor: pointer;
  overflow: hidden;
  outline: none; /* 键盘焦点时不显示默认 outline */
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

/* 顶部栏：标题 + 缓存统计 + 关闭按钮（右） */
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
  z-index: 25;
  box-sizing: border-box;
}

/* 拖拽遮罩条：鼠标经过视频顶部时浮现，可拖拽移动整个窗口 */
.player-drag-handle {
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  height: 34px;
  z-index: 20;
  cursor: grab;
  background: linear-gradient(to bottom, rgba(0, 0, 0, 0.55), rgba(0, 0, 0, 0.25), transparent);
  /* Wails v3 在 Windows WebView2 下使用 --wails-draggable: drag 来实现窗口拖拽 */
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
  position: absolute; inset: 0;
  display: flex; align-items: center; justify-content: center;
  pointer-events: none;
  z-index: 3;
}
.spinner {
  width: 42px; height: 42px;
  border: 3px solid rgba(255,255,255,0.15);
  border-top-color: #1890ff;
  border-radius: 50%;
  animation: spin 0.9s linear infinite;
}
@keyframes spin { to { transform: rotate(360deg); } }

/* ========= 错误提示 ========= */
.player-error {
  position: absolute;
  bottom: 80px;
  left: 16px;
  right: 16px;
  padding: 10px 14px;
  background: rgba(220, 53, 69, 0.92);
  color: #fff;
  display: flex; align-items: center; gap: 8px;
  font-size: 13px;
  border-radius: 8px;
  z-index: 4;
}

/* ========= 控制条 ========= */
.ctrl-bar {
  position: absolute;
  bottom: 0;
  left: 0;
  right: 0;
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 14px 16px;
  background: linear-gradient(to top, rgba(0,0,0,0.85), rgba(0,0,0,0));
  color: #fff;
  font-size: 12px;
  user-select: none;
  z-index: 5;
  box-sizing: border-box;
  flex-wrap: nowrap;
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
  display: inline-flex; align-items: center; justify-content: center;
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
  color: rgba(255,255,255,0.85);
}

/* ============ 进度条（重构：三层轨道 + 独特滑块） ============ */
.progress-container {
  position: relative;
  flex: 1;
  height: 18px;               /* 更大的点击热区，避免误触 */
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
  height: 4px;
  border-radius: 3px;
  transition: height 0.15s ease;
  pointer-events: none;
}
.progress-container:hover .progress-track-bg,
.progress-container:hover .progress-buffer,
.progress-container:hover .progress-played {
  height: 6px;        /* 悬停时变粗，给用户反馈 */
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
  0%, 100% { transform: scale(1); opacity: 0.6; }
  50%      { transform: scale(1.2); opacity: 1; }
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

/* 缩略图预览 */
.progress-thumbnail-preview {
  position: absolute;
  bottom: 100%;
  transform: translateX(-50%);
  margin-bottom: 10px;
  pointer-events: none;
  z-index: 10;
  border-radius: 6px;
  overflow: hidden;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.5);
  border: 1px solid rgba(255, 255, 255, 0.15);
}
.progress-thumbnail-preview img {
  display: block;
  width: 160px;
  height: 90px;
  object-fit: cover;
}
.progress-thumbnail-preview .preview-time {
  display: block;
  text-align: center;
  font-size: 11px;
  color: rgba(255, 255, 255, 0.85);
  background: rgba(0, 0, 0, 0.6);
  padding: 2px 6px;
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
/* 桥接区：从按钮到 popup 之间不会丢失 hover */
.volume-group::before {
  content: '';
  position: absolute;
  left: -4px;
  right: -4px;
  top: -120px;
  height: 128px;
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
  height: 120px;   /* 垂直滑块轨道高度 */
  display: flex;
  align-items: center;
  justify-content: center;
}
.volume-slider-v {
  /* 水平方向的 input，轨道宽 120px → 旋转 90° 后变成 120px 高 */
  width: 120px;
  height: 6px;
  -webkit-appearance: none;
  appearance: none;
  background: rgba(255, 255, 255, 0.2);
  border-radius: 3px;
  outline: none;
  transform: rotate(-90deg);
  transform-origin: center center;
  cursor: pointer;
  accent-color: #1890ff;
}
.volume-slider-v::-webkit-slider-thumb {
  -webkit-appearance: none;
  width: 16px; height: 16px;
  border-radius: 50%;
  background: #1890ff;
  border: 2px solid #fff;
  box-shadow: 0 0 0 4px rgba(24, 144, 255, 0.18);
  cursor: pointer;
}
.volume-slider-v::-moz-range-thumb {
  width: 16px; height: 16px;
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
/* 桥接区：从按钮到 popup 之间不会丢失 hover */
.speed-group::before {
  content: '';
  position: absolute;
  left: -4px;
  right: -4px;
  top: -110px;
  height: 118px;
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

/* ========= B 站风格：底部左侧继续播放小提示 ========= */
.resume-bili {
  position: absolute;
  left: 12px;
  bottom: 52px;     /* 放在控制条上方 */
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
  from { opacity: 0; transform: translateY(6px); }
  to   { opacity: 1; transform: translateY(0); }
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
  bottom: 52px;
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
  from { opacity: 0; transform: translateX(-10px); }
  to   { opacity: 1; transform: translateX(0); }
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
.fade-enter-active, .fade-leave-active {
  transition: opacity 0.2s ease;
}
.fade-enter-from, .fade-leave-to {
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
  bottom: 100%;
  top: auto;
  margin-bottom: 6px;
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

/* ========= AI 补帧警告弹窗 ========= */
.ai-warning-overlay {
  position: absolute;
  inset: 0;
  background: rgba(0, 0, 0, 0.7);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 20;
  backdrop-filter: blur(4px);
  animation: fadeIn 0.2s ease;
}
@keyframes fadeIn {
  from { opacity: 0; }
  to { opacity: 1; }
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
</style>
