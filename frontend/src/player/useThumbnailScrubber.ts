import { getCurrentScope, onScopeDispose, ref, watch, type Ref } from 'vue'
import { TsCache } from '../utils/tsCache'
import { proxyHlsURL } from './hls/proxy'

export interface ThumbnailScrubberDeps {
  wrapperRef: Ref<HTMLDivElement | undefined>
  progressContainerRef: Ref<HTMLDivElement | undefined>
  thumbCanvasRef: Ref<HTMLCanvasElement | null>
  getVideoEl: () => HTMLVideoElement | null
  getUrl: () => string
  isHlsUrl: (url: string) => boolean
  idleReleaseMs?: number
  keepPreviousFrame?: boolean
}
interface Preview {
  video: HTMLVideoElement
  canvas: HTMLCanvasElement
  hls: any
  ready: Promise<void>
  cancelSeek: (() => void) | null
  metadata: () => void
}

/** Owns one lazy preview decoder independently of the main video's DOM ref. */
export function useThumbnailScrubber(deps: ThumbnailScrubberDeps) {
  const progressHoverPct = ref(-1)
  const progressHoverTime = ref(-1)
  const thumbPreviewImg = ref('')
  const thumbPreviewVisible = ref(false)
  const thumbPreviewState = ref<'idle' | 'loading' | 'ready' | 'unavailable'>('idle')
  const thumbPreviewTime = ref(-1)
  let armed = false
  let preview: Preview | null = null
  let lastSeekAt = -Infinity
  let target: number | null = null
  let idleTimer: ReturnType<typeof setTimeout> | null = null
  let requestTimer: ReturnType<typeof setTimeout> | null = null
  let loadingTimer: ReturnType<typeof setTimeout> | null = null
  let capturedTime = -1

  function clearIdle(): void {
    if (idleTimer !== null) clearTimeout(idleTimer)
    idleTimer = null
  }
  function releasePreview(): void {
    clearIdle()
    if (requestTimer !== null) clearTimeout(requestTimer)
    if (loadingTimer !== null) clearTimeout(loadingTimer)
    requestTimer = loadingTimer = null
    const owned = preview
    preview = null
    target = null
    if (owned) {
      owned.cancelSeek?.()
      owned.video.removeEventListener('loadedmetadata', owned.metadata)
      try { owned.hls?.destroy() } catch { /* Finish other cleanup even if HLS fails. */ }
      owned.hls = null
      try { owned.video.pause() } catch { /* Already detached media is safe. */ }
      owned.video.removeAttribute('src')
      try { owned.video.load() } catch { /* Reset decoder resources before removing DOM. */ }
      owned.video.remove()
      owned.canvas.width = owned.canvas.height = 0
      owned.canvas.remove()
    }
    deps.thumbCanvasRef.value = null
    thumbPreviewImg.value = ''
    thumbPreviewVisible.value = false
    thumbPreviewState.value = 'idle'
    capturedTime = -1
    thumbPreviewTime.value = -1
    lastSeekAt = -Infinity
  }
  // Simply watching does not allocate a second video or HLS instance.
  async function initThumbSampler(): Promise<void> { armed = true }
  function destroyThumbSampler(): void {
    armed = false
    releasePreview()
    progressHoverPct.value = progressHoverTime.value = -1
  }
  async function ensurePreview(): Promise<Preview | null> {
    if (!armed || !deps.getVideoEl() || !deps.wrapperRef.value || !deps.getUrl()) return null
    if (preview) {
      const existing = preview
      await existing.ready
      return preview === existing ? existing : null
    }
    const url = deps.getUrl()
    const video = document.createElement('video')
    const canvas = document.createElement('canvas')
    video.muted = true
    video.preload = 'metadata'
    video.playsInline = true
    video.style.display = 'none'
    video.setAttribute('tabindex', '-1')
    canvas.style.display = 'none'
    deps.wrapperRef.value.appendChild(video)
    deps.wrapperRef.value.appendChild(canvas)
    deps.thumbCanvasRef.value = canvas
    const owned: Preview = { video, canvas, hls: null, ready: Promise.resolve(), cancelSeek: null, metadata: () => {
      if (preview === owned && target !== null) captureThumbnail(owned, target)
    } }
    preview = owned
    video.addEventListener('loadedmetadata', owned.metadata)
    owned.ready = (async () => {
      if (deps.isHlsUrl(url)) {
        try {
          const { default: Hls } = await import('hls.js')
          if (preview !== owned || !armed) return
          if (Hls.isSupported()) {
            owned.hls = new Hls({ enableWorker: false, lowLatencyMode: false,
              maxBufferLength: 1, maxMaxBufferLength: 1, backBufferLength: 0,
              maxBufferSize: 1024 * 1024, fragLoadingTimeOut: 8000, fragLoadingMaxRetry: 3,
              manifestLoadingTimeOut: 6000, manifestLoadingMaxRetry: 2,
              autoStartLoad: false, startLevel: 0, loader: TsCache.TsCacheLoader,
            })
            owned.hls.on(Hls.Events.ERROR, (_event: unknown, data: { fatal?: boolean }) => {
              if (preview === owned && data.fatal && target !== null) thumbPreviewState.value = 'unavailable'
            })
            owned.hls.loadSource(proxyHlsURL(url))
            owned.hls.attachMedia(video)
            return
          }
        } catch { /* Preview failure must not disturb the primary player. */ }
      }
      if (preview !== owned || !armed) return
      video.src = proxyHlsURL(url)
    })()
    await owned.ready
    return preview === owned ? owned : null
  }
  function captureThumbnail(owned: Preview, timeSec: number): void {
    if (preview !== owned || !armed || target === null || owned.cancelSeek) return
    const video = owned.video
    if (video.readyState < 1) {
      try { owned.hls?.startLoad(timeSec) } catch { /* Metadata retries the pending target. */ }
      return
    }
    // Keep one seek in flight. Rapid finger moves replace the queued target,
    // rather than continually cancelling decoding before a frame is available.
    const requested = Math.max(0, Math.min(timeSec, Number.isFinite(video.duration) ? Math.max(0, video.duration - .05) : timeSec))
    let timer: ReturnType<typeof setTimeout> | null = null
    const cancel = () => {
      video.removeEventListener('seeked', capture)
      video.removeEventListener('loadeddata', capture)
      if (timer !== null) clearTimeout(timer)
      timer = null
      if (owned.cancelSeek === cancel) owned.cancelSeek = null
    }
    const capture = () => {
      if (preview !== owned || !armed || target === null || video.readyState < 2 || video.seeking || Math.abs(video.currentTime - requested) > .25) return
      cancel()
      const superseded = Math.abs(target - timeSec) > .25
      try {
        const width = video.videoWidth || 256, height = video.videoHeight || 144
        const scale = Math.min(256 / width, 144 / height, 1)
        owned.canvas.width = Math.max(1, Math.round(width * scale))
        owned.canvas.height = Math.max(1, Math.round(height * scale))
        const ctx = owned.canvas.getContext('2d')
        if (!ctx) throw new Error('Preview canvas unavailable')
        ctx.drawImage(video, 0, 0, owned.canvas.width, owned.canvas.height)
        thumbPreviewImg.value = owned.canvas.toDataURL('image/jpeg', .7)
        thumbPreviewVisible.value = !superseded || !!deps.keepPreviousFrame
        thumbPreviewState.value = superseded ? 'loading' : 'ready'
        capturedTime = timeSec
        thumbPreviewTime.value = timeSec
        if (!superseded) {
          if (loadingTimer !== null) clearTimeout(loadingTimer)
          loadingTimer = null
        }
      } catch { thumbPreviewVisible.value = false; thumbPreviewState.value = 'unavailable' }
      try { owned.hls?.stopLoad() } catch { /* A captured thumbnail needs no ongoing fetching. */ }
      if (superseded) scheduleThumbnail()
    }
    owned.cancelSeek = cancel
    video.addEventListener('seeked', capture)
    video.addEventListener('loadeddata', capture)
    timer = setTimeout(() => {
      cancel()
      try { owned.hls?.stopLoad() } catch { /* A failed preview never affects playback. */ }
      if (target !== null && Math.abs(target - timeSec) > .25) scheduleThumbnail()
      else if (target !== null) thumbPreviewState.value = 'unavailable'
    }, 2500)
    try {
      if (Math.abs(video.currentTime - requested) <= .05 && video.readyState >= 2 && !video.seeking) capture()
      else { owned.hls?.startLoad(requested); video.currentTime = requested }
    } catch { cancel(); thumbPreviewState.value = 'unavailable' }
  }
  function scheduleThumbnail(): void {
    if (requestTimer !== null || target === null || !armed) return
    const delay = Math.max(0, 150 - (performance.now() - lastSeekAt))
    requestTimer = setTimeout(() => {
      requestTimer = null
      lastSeekAt = performance.now()
      void ensurePreview().then(owned => {
        if (owned && target !== null) captureThumbnail(owned, target)
      }).catch(() => { if (target !== null) thumbPreviewState.value = 'unavailable' })
    }, delay)
  }
  function requestThumbnail(timeSec: number): void {
    const main = deps.getVideoEl()
    if (!armed || !main || !Number.isFinite(timeSec) || !Number.isFinite(main.duration) || main.duration <= 0) return
    clearIdle()
    const starting = target === null
    target = Math.max(0, Math.min(main.duration, timeSec))
    if (Math.abs(capturedTime - target) <= .25 && thumbPreviewImg.value) {
      thumbPreviewVisible.value = true
      thumbPreviewState.value = 'ready'
      if (loadingTimer !== null) clearTimeout(loadingTimer)
      loadingTimer = null
      return
    }
    // A moving preview can retain the last decoded frame with its own timestamp
    // while fetching the next one, instead of blinking on every pointer move.
    thumbPreviewVisible.value = !!deps.keepPreviousFrame && !starting && capturedTime >= 0
    thumbPreviewState.value = 'loading'
    if (starting) loadingTimer = setTimeout(() => {
      loadingTimer = null
      if (target !== null && !thumbPreviewVisible.value) thumbPreviewState.value = 'unavailable'
    }, 6000)
    scheduleThumbnail()
  }
  function onProgressHover(event: MouseEvent): void {
    const progress = deps.progressContainerRef.value, main = deps.getVideoEl()
    if (!progress || !main || !Number.isFinite(main.duration) || main.duration <= 0) return
    const rect = progress.getBoundingClientRect()
    const percent = Math.max(0, Math.min(100, (event.clientX - rect.left) / rect.width * 100))
    progressHoverPct.value = percent
    progressHoverTime.value = percent / 100 * main.duration
    requestThumbnail(progressHoverTime.value)
  }
  function onProgressLeave(): void {
    progressHoverPct.value = progressHoverTime.value = -1
    thumbPreviewVisible.value = false
    target = null
    thumbPreviewState.value = 'idle'
    if (requestTimer !== null) clearTimeout(requestTimer)
    if (loadingTimer !== null) clearTimeout(loadingTimer)
    requestTimer = loadingTimer = null
    preview?.cancelSeek?.()
    try { preview?.hls?.stopLoad() } catch { /* Idle teardown remains independent. */ }
    clearIdle()
    if (preview) idleTimer = setTimeout(releasePreview, deps.idleReleaseMs ?? 10000)
  }
  // One synchronous scoped watcher; it never retains an old decoder or
  // creates new resources after closing the player.
  watch(deps.getUrl, () => { releasePreview() })
  if (getCurrentScope()) onScopeDispose(destroyThumbSampler)
  function snapshot() {
    const ranges: number[][] = []
    const owned = preview
    try {
      if (owned) for (let index = 0; index < owned.video.buffered.length && index < 8; index++) ranges.push([owned.video.buffered.start(index), owned.video.buffered.end(index)])
    } catch { /* The decoder may be releasing its SourceBuffer. */ }
    return { active: !!owned, hls_active: !!owned?.hls, pending_seek: !!owned?.cancelSeek,
      ready_state: owned?.video.readyState ?? null, buffered_ranges: ranges }
  }
  return { progressHoverPct, progressHoverTime, thumbPreviewImg, thumbPreviewVisible, thumbPreviewState, thumbPreviewTime,
    initThumbSampler, destroyThumbSampler, requestThumbnail, onProgressHover, onProgressLeave, snapshot }
}
