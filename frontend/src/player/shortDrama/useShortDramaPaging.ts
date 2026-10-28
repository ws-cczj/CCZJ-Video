import { nextTick, onBeforeUnmount, ref, watch, type Ref } from 'vue'
import { useMotionStore } from '../../stores/motion'
import { videoPictureBounds } from '../../utils/videoLayout'
import { recordDiagnosticEvent } from '../../diagnostics/recording'

type Direction = 'previous' | 'next'

/** One live decoder and one temporary frame own a vertical handoff. Swipes
 * settle independently of buffering; only silent refresh waits for a frame.
 * No second player or exported image/cache is needed. */
export function useShortDramaPaging(options: {
  surface: Ref<HTMLElement | undefined>
  canvas: Ref<HTMLCanvasElement | undefined>
  cover: Ref<HTMLElement | undefined>
  chrome: Ref<HTMLElement | undefined>
  video: () => HTMLVideoElement | null
  active: () => boolean
  visible: () => boolean
  canMove: (direction: Direction) => boolean
  step: (direction: Direction) => void
  canRefresh?: () => boolean
  refresh?: () => void
  timeout?: () => void
}) {
  const motion = useMotionStore(), frozen = ref(false)
  let direction = 0, pending = false, animating = false, distance = 0, waitForMedia = false
  let frame: number | undefined, timeout: ReturnType<typeof setTimeout> | undefined
  let animations: Animation[] = [], revision = 0
  let outgoingOffset = 0, incomingOffset = 0, height = 0
  let pulled = 0
  const duration = () => motion.reduced || document.documentElement.dataset.motionPages === 'off' || document.documentElement.dataset.cczjMotion === 'off' ? 0 : motion.tokenMs('--cczj-motion-slow')

  function reset(): void {
    revision++; pending = false; animating = false; direction = 0; distance = 0; waitForMedia = false
    outgoingOffset = 0; incomingOffset = 0; height = 0; pulled = 0
    if (frame !== undefined) cancelAnimationFrame(frame)
    frame = undefined; clearTimeout(timeout); timeout = undefined
    animations.forEach(animation => animation.cancel()); animations = []
    options.surface.value?.style.removeProperty('transform')
    const canvas = options.canvas.value
    if (canvas) { canvas.style.removeProperty('transform'); canvas.width = 0; canvas.height = 0 }
    options.cover.value?.style.removeProperty('transform')
    options.chrome.value?.replaceChildren()
    frozen.value = false
  }
  function drag(value: number): void {
    if (!options.active() || frozen.value) return
    pulled = value
    if (pending || !duration()) return
    const allowed = options.canMove(value < 0 ? 'next' : 'previous')
    distance = Math.max(-innerHeight * .8, Math.min(innerHeight * .8, value * (allowed ? 1 : .15)))
    if (frame !== undefined) return
    frame = requestAnimationFrame(() => {
      frame = undefined
      if (!pending && options.surface.value) options.surface.value.style.transform = `translate3d(0,${distance}px,0)`
    })
  }
  function contact(): void {
    // Refresh keeps an outgoing still while buffering, but must never hide the
    // only interactive player from the next gesture. Input belongs to the
    // stable scene, not the translated video subtree.
    if (frozen.value && !animating) reset()
  }
  function cancelDrag(): void {
    if (pending || frozen.value) return
    const surface = options.surface.value, offset = distance
    reset()
    if (surface && offset && duration()) {
      const animation = surface.animate([{ transform: `translate3d(0,${offset}px,0)` }, { transform: 'translate3d(0,0,0)' }], { duration: duration(), easing: 'cubic-bezier(.2,.8,.2,1)' })
      animations = [animation]
    }
  }
  function step(value: Direction): void {
    if (options.active() && !animating && value === 'previous' && pulled >= Math.max(100, innerHeight * .18) && options.canRefresh?.()) {
      reset(); options.refresh?.(); return
    }
    if (pending || frozen.value || !options.active()) return
    if (!options.canMove(value)) {
      cancelDrag()
      return
    }
    cancelDrag()
    direction = value === 'next' ? 1 : -1; pending = true
    options.step(value)
  }
  function replace(work: () => void): void {
    if (pending || frozen.value || !options.active()) return
    cancelDrag(); direction = 1; pending = true; waitForMedia = true; work()
  }
  function capture(): void {
    const started = performance.now()
    const video = options.video(), canvas = options.canvas.value, surface = options.surface.value
    if (!options.active() || !pending || !direction || !duration() || !video || video.readyState < 2 || !canvas || !surface) { reset(); return }
    const bounds = surface.getBoundingClientRect()
    if (!bounds.width || !bounds.height || !video.videoWidth || !video.videoHeight) { reset(); return }
    // Continue from the finger/rebound position instead of snapping the
    // outgoing picture back to zero at the instant the detail commits.
    const transform = getComputedStyle(surface).transform
    outgoingOffset = transform === 'none' ? 0 : new DOMMatrixReadOnly(transform).m42
    animations.forEach(animation => animation.cancel()); animations = []
    height = bounds.height; incomingOffset = direction * height + outgoingOffset
    const density = Math.min(devicePixelRatio, 720 / bounds.width, 1280 / bounds.height)
    canvas.width = Math.max(1, Math.round(bounds.width * density))
    canvas.height = Math.max(1, Math.round(bounds.height * density))
    const context = canvas.getContext('2d', { alpha: false })
    if (!context) { reset(); return }
    const picture = videoPictureBounds(video, surface)
    if (!picture) { reset(); return }
    try {
      context.fillStyle = getComputedStyle(surface).backgroundColor || '#18191c'
      context.fillRect(0, 0, canvas.width, canvas.height)
      context.drawImage(video, picture.left * density, picture.top * density, picture.width * density, picture.height * density)
    } catch { reset(); return }
    // Decorative controls belong to the same outgoing frame. cloneNode copies
    // their current DOM/style without Vue listeners; the inert cover owns and
    // releases this one clone together with its bounded canvas.
    const chrome = surface.querySelector('.portrait-controls')
    options.chrome.value?.replaceChildren(...(chrome ? [chrome.cloneNode(true)] : []))
    void recordDiagnosticEvent('short_drama_frame_capture', { elapsed_ms: Math.round(performance.now() - started), width: canvas.width, height: canvas.height })
    frozen.value = true
    if (options.cover.value) options.cover.value.style.transform = `translate3d(0,${outgoingOffset}px,0)`
    surface.style.transform = `translate3d(0,${incomingOffset}px,0)`
    if (waitForMedia) timeout = setTimeout(() => { reset(); options.timeout?.() }, 15000)
    else {
      const token = revision
      // A user swipe settles with the page duration, independent of provider
      // buffering. Waiting for loadeddata here leaves a screen-sized still and
      // an offscreen input target for up to fifteen seconds on slow streams.
      void nextTick(() => { if (token === revision) void ready() })
    }
  }
  async function ready(): Promise<void> {
    if (!options.active() || !frozen.value || animating) return
    const surface = options.surface.value, cover = options.cover.value
    if (!surface || !cover) { reset(); return }
    clearTimeout(timeout); timeout = undefined; animating = true
    const token = revision, timing = { duration: duration(), easing: 'cubic-bezier(.2,.8,.2,1)', fill: 'both' as const }
    animations = [
      surface.animate([{ transform: `translate3d(0,${incomingOffset}px,0)` }, { transform: 'translate3d(0,0,0)' }], timing),
      cover.animate([{ transform: `translate3d(0,${outgoingOffset}px,0)` }, { transform: `translate3d(0,${-direction * height}px,0)` }], timing),
    ]
    await Promise.allSettled(animations.map(animation => animation.finished))
    if (token === revision) reset()
  }
  watch([options.active, options.visible], ([active, visible]) => {
    if (!visible) { reset(); return }
    if (!active) {
      if (!frozen.value) { reset(); return }
      // The route can stop work before its outgoing pixels leave the screen.
      // Retain this one frame through that leave instead of exposing a new
      // decoder which may still be preparing underneath it.
      clearTimeout(timeout); timeout = undefined
      animations.forEach(animation => animation.pause())
      return
    }
    if (animating) animations.forEach(animation => animation.play())
    else if (frozen.value && (!waitForMedia || (options.video()?.readyState ?? 0) >= 2)) void ready()
    else if (frozen.value) timeout = setTimeout(() => { reset(); options.timeout?.() }, 15000)
  })
  onBeforeUnmount(reset)
  return { frozen, drag, cancelDrag, step, replace, capture, ready, reset, contact }
}
