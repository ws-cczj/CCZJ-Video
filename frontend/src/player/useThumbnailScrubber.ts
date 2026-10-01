import { ref, watch, type Ref } from 'vue'
import { TsCache } from '../utils/tsCache'
import { proxyHlsURL } from './hls/proxy'

export interface ThumbnailScrubberDeps {
  /** 采样用的 video 与 canvas 都挂在容器里：容器一消失它们跟着消失，不需要另行摘除 */
  wrapperRef: Ref<HTMLDivElement | undefined>
  /** 进度条容器：悬停位置要按它的实际宽度换算，全屏/窄窗下才不会算错 */
  progressContainerRef: Ref<HTMLDivElement | undefined>
  /** 画布元素由模板持有 ref，采样器只负责往里塞一个动态创建的 canvas */
  thumbCanvasRef: Ref<HTMLCanvasElement | null>
  getVideoEl: () => HTMLVideoElement | null
  getUrl: () => string
  isHlsUrl: (url: string) => boolean
}

/**
 * 进度条悬停预览：时间提示 + 缩略图抓帧。
 *
 * 缩略图必须自己另起一条低缓冲的 HLS 流来 seek：主视频流是边播边下的，跳去读未来的
 * 位置会把它的缓冲区搅乱，用户会看见正在看的那一段突然重新加载。
 */
export function useThumbnailScrubber(deps: ThumbnailScrubberDeps) {
  const progressHoverPct = ref(-1) // 鼠标悬停在进度条上的百分比位置（-1 表示不显示）
  const progressHoverTime = ref(-1) // 悬停时间（秒），用于显示时间预览
  const thumbPreviewImg = ref('') // 缩略图 dataURL
  const thumbPreviewVisible = ref(false)

  // 缩略图采样：用一个隐藏的 <video> 元素 seek + canvas 抓帧
  const thumbVideoRef = ref<HTMLVideoElement | null>(null)
  let _thumbLastSeekTime = 0 // 节流：上次 seek 时间戳
  const THUMB_SEEK_THROTTLE_MS = 150 // 至少 150ms 间隔才触发新 seek
  let _thumbSeekPending = false

  function captureThumbnail(timeSec: number): void {
    const v = thumbVideoRef.value
    const canvas = deps.thumbCanvasRef.value
    if (!v || !canvas) { _thumbSeekPending = false; return }
    const ctx = canvas.getContext('2d')
    if (!ctx) { _thumbSeekPending = false; return }

    // ⭐ 检查视频是否可 seek：readyState >= 1 (HAVE_METADATA) 才允许设置 currentTime
    if (v.readyState < 1) {
      try { (v as any).__thumbHls?.startLoad(timeSec) } catch { /* ignore */ }
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
      try { (v as any).__thumbHls?.startLoad(timeSec) } catch { /* ignore */ }
      v.currentTime = timeSec
    } catch {
      v.removeEventListener('seeked', onSeeked)
      clearTimeout(timeoutId)
      _thumbSeekPending = false
    }
  }

  // 采样器的代号：init 是 async 的（动态 import hls.js），换集或卸载落在这个 await 上时，
  // 旧的那次会继续往下建第二个 HLS 实例，挂到一个已经从 DOM 上摘掉的 video 上，谁也不会
  // 再销毁它。cleanup 时代号 +1，半途醒来的 init 看到对不上就直接走。
  let _thumbGen = 0
  let _thumbRestartTimer: number | null = null

  async function initThumbSampler(): Promise<void> {
    const mainVideo = deps.getVideoEl()
    if (!mainVideo) return
    const gen = ++_thumbGen
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
    deps.wrapperRef.value?.appendChild(sampleVideo)
    thumbVideoRef.value = sampleVideo

    // 创建 canvas（隐藏）
    const canvas = document.createElement('canvas')
    canvas.style.display = 'none'
    deps.wrapperRef.value?.appendChild(canvas)
    deps.thumbCanvasRef.value = canvas

    let thumbHls: any = null

    // 主 video 元素销毁时清理
    const cleanup = () => {
      _thumbGen++
      if (_thumbRestartTimer !== null) { window.clearTimeout(_thumbRestartTimer); _thumbRestartTimer = null }
      try { thumbHls?.destroy() } catch { }
      try { sampleVideo.remove() } catch { }
      try { canvas.remove() } catch { }
      thumbVideoRef.value = null
      deps.thumbCanvasRef.value = null
      thumbPreviewImg.value = ''
      thumbPreviewVisible.value = false
    }
      ; (mainVideo as any).__thumbCleanup = cleanup

    // ⭐ HLS 流：用第二个 hls.js 小实例加载同源流（低缓冲，仅用于 seek + 抓帧）
    if (deps.isHlsUrl(deps.getUrl())) {
      try {
        const { default: Hls } = await import('hls.js')
        if (gen !== _thumbGen) return
        if (Hls.isSupported()) {
          const hlsConfig: any = {
            enableWorker: false,
            lowLatencyMode: false,
            maxBufferLength: 1,
            maxMaxBufferLength: 1,
            maxBufferSize: 1024 * 1024,
            fragLoadingTimeOut: 8000,
            fragLoadingMaxRetry: 3,
            manifestLoadingTimeOut: 6000,
            manifestLoadingMaxRetry: 2,
            autoStartLoad: false,
            startLevel: 0,
            loader: TsCache.TsCacheLoader,
          }
          thumbHls = new Hls(hlsConfig)
          ;(sampleVideo as any).__thumbHls = thumbHls
          thumbHls.loadSource(proxyHlsURL(deps.getUrl()))
          thumbHls.attachMedia(sampleVideo)
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
    const origUrl = deps.getUrl()
    const urlWatch = watch(deps.getUrl, (newUrl) => {
      if (newUrl !== origUrl) {
        cleanup()
        _thumbRestartTimer = window.setTimeout(() => {
          _thumbRestartTimer = null
          initThumbSampler()
        }, 500)
      }
    })
      ; (mainVideo as any).__thumbUrlWatch = urlWatch
  }

  function destroyThumbSampler(): void {
    const v = deps.getVideoEl()
    if (v) {
      try { (v as any).__thumbCleanup?.() } catch { }
      try { (v as any).__thumbUrlWatch?.() } catch { }
    }
    if (thumbVideoRef.value) {
      try { thumbVideoRef.value.remove() } catch { }
      thumbVideoRef.value = null
    }
    if (deps.thumbCanvasRef.value) {
      try { deps.thumbCanvasRef.value.remove() } catch { }
      deps.thumbCanvasRef.value = null
    }
    thumbPreviewImg.value = ''
    thumbPreviewVisible.value = false
  }

  // 进度条悬停：计算百分比位置 + 显示时间预览 + 缩略图
  function onProgressHover(e: MouseEvent): void {
    if (!deps.progressContainerRef.value) return
    const rect = deps.progressContainerRef.value.getBoundingClientRect()
    const pct = Math.max(0, Math.min(100, ((e.clientX - rect.left) / rect.width) * 100))
    progressHoverPct.value = pct
    const v = deps.getVideoEl()
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

  return {
    progressHoverPct,
    progressHoverTime,
    thumbPreviewImg,
    thumbPreviewVisible,
    initThumbSampler,
    destroyThumbSampler,
    onProgressHover,
  }
}
