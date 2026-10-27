import { ref, onMounted, onBeforeUnmount } from 'vue'
import type { PlayerPlatform } from '../player/platform'
import { formatPlaybackTime } from '../player/videoInfoFormat'

/** One pointer owns one gesture. Scrubbing commits once on release; cancellation never seeks. */
export function useMobilePlayerGestures(options: {
  enabled: boolean; device: PlayerPlatform['device']; video: () => HTMLVideoElement | null; visible: () => void; onVolume: (value: number) => void
  onStart?: () => void
  holdSpeed?: () => number
  interaction?: 'portrait'
  onSwipe?: (direction: 'previous' | 'next') => void
}) {
  const locked = ref(false)
  const hud = ref('')
  const kind = ref<'seek' | 'brightness' | 'volume' | 'speed' | ''>('')
  const boosting = ref(false)
  const seekTarget = ref(-1)
  const level = ref(0)
  const compact = ref(false)
  const active = ref(false)
  let hide: ReturnType<typeof setTimeout> | undefined
  let collapse: ReturnType<typeof setTimeout> | undefined
  let suppressClick = false
  let hold: ReturnType<typeof setTimeout> | undefined
  let boosted: { video: HTMLVideoElement; rate: number } | null = null
  let swipe = 0
  function clearHold(): void { if (hold) clearTimeout(hold); hold = undefined }
  function restoreRate(): void {
    const saved = boosted
    boosted = null
    if (saved) {
      saved.video.removeEventListener('pause', cancel)
      saved.video.removeEventListener('ended', cancel)
      saved.video.removeEventListener('emptied', cancel)
      saved.video.playbackRate = saved.rate
    }
    boosting.value = false
  }
  let start: { id: number; x: number; y: number; width: number; height: number;
    time: number; duration: number; brightness: number; volume: number; target: number; side: boolean; native: boolean } | null = null
  function down(e: PointerEvent): void {
    if (options.enabled && !e.isPrimary) { cancel(); return }
    if (!options.enabled || !e.isPrimary || e.button !== 0) return
    cancel()
    suppressClick = false
    if (locked.value || (e.target as Element).closest('button,input,select,a,[role="dialog"],.progress-bar-wrapper,.ctrl-bar,.vp-modal-overlay,.ui-modal-overlay,.ai-warning-overlay,.cczj-mobile-cast')) return
    const video = options.video()
    if (!video || video.readyState < 1) return
    // DOM clears event.currentTarget after dispatch; the hold fires later.
    const root = e.currentTarget as HTMLElement
    const r = root.getBoundingClientRect(), device = options.device.read()
    if (hide) clearTimeout(hide)
    if (collapse) clearTimeout(collapse)
    kind.value = ''; hud.value = ''; seekTarget.value = -1; compact.value = false; active.value = false
    start = { id: e.pointerId, x: e.clientX, y: e.clientY, width: r.width, height: r.height,
      time: video.currentTime, duration: video.duration, target: video.currentTime,
      brightness: device?.brightness ?? .5, volume: device?.volume ?? video.volume,
      side: e.clientX - r.left < r.width / 2, native: !!device }
    hold = setTimeout(() => {
      hold = undefined
      if (!start || kind.value || locked.value || options.video() !== video || video.paused || video.ended) return
      boosted = { video, rate: video.playbackRate }
      boosting.value = true
      // Holding never slows down a user's already faster selected rate.
      const configured = options.holdSpeed?.() ?? 2
      video.playbackRate = Math.max(Number.isFinite(configured) ? Math.max(1, Math.min(4, configured)) : 2, video.playbackRate)
      kind.value = 'speed'; hud.value = String(video.playbackRate); active.value = true; suppressClick = true
      options.onStart?.()
      root.setPointerCapture(e.pointerId)
      video.addEventListener('pause', cancel)
      video.addEventListener('ended', cancel)
      video.addEventListener('emptied', cancel)
    }, 450)
  }
  function move(e: PointerEvent): void {
    const s = start
    if (!s || e.pointerId !== s.id || locked.value) return
    if (kind.value === 'speed') { e.preventDefault(); return }
    const dx = e.clientX - s.x, dy = e.clientY - s.y
    if (options.interaction === 'portrait') {
      if (Math.hypot(dx, dy) < 10) return
      clearHold()
      suppressClick = true
      swipe = Math.abs(dy) > Math.abs(dx) * 1.4 ? dy : 0
      ;(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId)
      e.preventDefault()
      return
    }
    if (!kind.value) {
      if (Math.hypot(dx, dy) < 10) return
      clearHold()
      // Only expose gestures backed by an actual device capability.
      if (Math.abs(dx) < Math.abs(dy) && (s.side
        ? !s.native || !options.device.brightnessSupported
        : !options.device.volumeSupported)) {
        suppressClick = true
        return
      }
      kind.value = Math.abs(dx) >= Math.abs(dy) ? 'seek' : s.side ? 'brightness' : 'volume'
      active.value = true
      options.onStart?.()
      if (kind.value !== 'seek') collapse = setTimeout(() => {
        if (start && active.value) compact.value = true
      }, 650)
      ;(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId)
    }
    e.preventDefault(); suppressClick = true
    if (kind.value === 'seek') {
      if (!Number.isFinite(s.duration) || s.duration <= 0) return
      s.target = Math.max(0, Math.min(s.duration, s.time + dx / s.width * Math.min(180, s.duration)))
      seekTarget.value = s.target
      const diff = Math.round(s.target - s.time)
      hud.value = `${formatPlaybackTime(s.target)}  ${diff >= 0 ? '+' : '−'}${Math.abs(diff)}s`
    } else {
      const value = Math.max(kind.value === 'brightness' ? .02 : 0, Math.min(1, (kind.value === 'brightness' ? s.brightness : s.volume) - dy / s.height * 1.4))
      if (kind.value === 'brightness' && !options.device.setBrightness(value)) { cancel(); return }
      level.value = value
      hud.value = `${Math.round(value * 100)}%`
      if (kind.value === 'volume') {
        if (s.native) options.onVolume(value)
        else { const video = options.video(); if (video) { video.volume = value; video.muted = value === 0 } }
      }
    }
  }
  function end(e: PointerEvent): void {
    const s = start
    if (!s || e.pointerId !== s.id) return
    clearHold()
    start = null
    active.value = false
    const direction = swipe
    swipe = 0
    if (options.interaction === 'portrait' && Math.abs(direction) >= 50 && e.type === 'pointerup') {
      options.onSwipe?.(direction < 0 ? 'next' : 'previous')
      return
    }
    if (boosting.value) {
      restoreRate(); kind.value = ''; hud.value = ''; options.visible(); return
    }
    if (collapse) clearTimeout(collapse)
    if (e.type === 'pointercancel') suppressClick = true
    if (!kind.value) return
    if (e.type === 'pointerup' && kind.value === 'seek') {
      const video = options.video()
      if (video && Number.isFinite(s.duration) && s.duration > 0) video.currentTime = s.target
    }
    options.visible()
    hide = setTimeout(() => { hud.value = ''; kind.value = ''; seekTarget.value = -1 }, 650)
  }
  function cancel(): void {
    clearHold(); restoreRate()
    start = null; swipe = 0; active.value = false; hud.value = ''; kind.value = ''; seekTarget.value = -1; compact.value = false
    if (hide) clearTimeout(hide)
    if (collapse) clearTimeout(collapse)
  }
  function consumeClick(): boolean { return locked.value || suppressClick }
  function toggleLock(): void { cancel(); locked.value = !locked.value; options.visible() }
  function visibility(): void { if (document.hidden) cancel() }
  onMounted(() => {
    if (!options.enabled) return
    window.addEventListener('pointerup', end, true)
    window.addEventListener('pointercancel', end, true)
    window.addEventListener('blur', cancel)
    window.addEventListener('pagehide', cancel)
    document.addEventListener('visibilitychange', visibility)
  })
  onBeforeUnmount(() => {
    cancel()
    if (!options.enabled) return
    options.device.restoreBrightness()
    window.removeEventListener('pointerup', end, true)
    window.removeEventListener('pointercancel', end, true)
    window.removeEventListener('blur', cancel)
    window.removeEventListener('pagehide', cancel)
    document.removeEventListener('visibilitychange', visibility)
  })
  return { locked, hud, kind, boosting, seekTarget, level, compact, active, down, move, end, cancel, consumeClick, toggleLock }
}
