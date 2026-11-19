import { nextTick, onBeforeUnmount, ref, shallowRef, watch, type Ref } from 'vue'
import { useMotionStore } from '../../stores/motion'
import { videoPictureBounds } from '../../utils/videoLayout'
import { recordDiagnosticEvent } from '../../diagnostics/recording'
import type { ShortDramaNeighbor } from './useShortDramaFeed'

type Direction = 'previous' | 'next'
const pullThreshold = 96
const easing = 'cubic-bezier(.2,.8,.2,1)'
const translate = (y: number) => `translate3d(0,${y}px,0)`

export function useShortDramaPaging(options: {
  surface: Ref<HTMLElement | undefined>
  canvas: Ref<HTMLCanvasElement | undefined>
  cover: Ref<HTMLElement | undefined>
  chrome: Ref<HTMLElement | undefined>
  mobile?: { layer: Ref<HTMLElement | undefined>; neighbor: (direction: Direction) => ShortDramaNeighbor | null }
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
  const preview = shallowRef<ShortDramaNeighbor | null>(null)
  const pullProgress = ref(0)
  let direction = 0, pending = false, animating = false, distance = 0, waitForMedia = false
  let previewWaiting = false, returning = false, previewSide = 0
  let frame: number | undefined, timeout: ReturnType<typeof setTimeout> | undefined
  let animations: Animation[] = [], revision = 0
  let outgoingOffset = 0, incomingOffset = 0, height = 0
  let pulled = 0
  const duration = () => motion.allows('pages') ? motion.tokenMs('--cczj-motion-slow') : 0

  function reset(): void {
    revision++; pending = false; animating = false; direction = 0; distance = 0; waitForMedia = false
    outgoingOffset = 0; incomingOffset = 0; height = 0; pulled = 0
    previewWaiting = false; returning = false; previewSide = 0
    preview.value = null; pullProgress.value = 0
    if (frame !== undefined) cancelAnimationFrame(frame)
    frame = undefined; clearTimeout(timeout); timeout = undefined
    animations.forEach(animation => animation.cancel()); animations = []
    options.mobile?.layer.value?.style.removeProperty('transform')
    options.surface.value?.style.removeProperty('transform')
    const canvas = options.canvas.value
    if (canvas) { canvas.style.removeProperty('transform'); canvas.width = 0; canvas.height = 0 }
    options.cover.value?.style.removeProperty('transform')
    options.chrome.value?.replaceChildren()
    frozen.value = false
  }
  function drag(value: number): void {
    if (!options.active() || pending || frozen.value || previewWaiting) return
    pulled = value
    const target = value < 0 ? 'next' : 'previous'
    const pulling = value > 0 && !options.canMove('previous') && options.canRefresh?.()
    pullProgress.value = pulling ? Math.min(value / pullThreshold, 1) : 0
    if (!motion.allows('pages')) return
    height ||= options.surface.value?.clientHeight || 0
    const neighbor = value ? options.mobile?.neighbor(target) ?? null : null
    const allowed = options.mobile ? !!neighbor : options.canMove(target)
    preview.value = neighbor
    previewSide = value < 0 ? 1 : -1
    const offset = value * (allowed ? 1 : pulling ? .42 : .15)
    distance = Math.max(-height * .9, Math.min(height * .9, offset))
    if (frame !== undefined) return
    frame = requestAnimationFrame(() => {
      frame = undefined
      if (pending) return
      const surface = options.surface.value, layer = options.mobile?.layer.value
      if (surface) surface.style.transform = translate(distance)
      if (layer) layer.style.transform = translate(distance + previewSide * height)
    })
  }
  function contact(): void {
    // Refresh keeps an outgoing still while buffering, but must never hide the
    // only interactive player from the next gesture. Input belongs to the
    // stable scene, not the translated video subtree.
    if (previewWaiting || returning || (frozen.value && !animating)) reset()
  }
  function cancelDrag(): void {
    if (pending || frozen.value || previewWaiting || returning) return
    const surface = options.surface.value, offset = distance
    const layer = preview.value ? options.mobile?.layer.value : undefined
    const edge = previewSide * height
    const ms = offset ? duration() : 0
    if (surface && layer && ms) {
      if (frame !== undefined) cancelAnimationFrame(frame)
      frame = undefined; pulled = 0; pullProgress.value = 0; returning = true
      const token = revision
      animations = [
        surface.animate([{ transform: translate(offset) }, { transform: translate(0) }], { duration: ms, easing, fill: 'both' }),
        layer.animate([{ transform: translate(edge + offset) }, { transform: translate(edge) }], { duration: ms, easing, fill: 'both' }),
      ]
      void Promise.allSettled(animations.map(animation => animation.finished)).then(() => { if (token === revision) reset() })
      return
    }
    reset()
    if (surface && ms) {
      const animation = surface.animate([{ transform: translate(offset) }, { transform: translate(0) }], { duration: ms, easing })
      animations = [animation]
    }
  }
  async function slideMobile(target: Direction): Promise<void> {
    const surface = options.surface.value, layer = options.mobile?.layer.value
    const neighbor = options.mobile?.neighbor(target)
    const ms = duration()
    if (!surface || !layer || !neighbor || !ms) { reset(); options.step(target); return }

    if (frame !== undefined) cancelAnimationFrame(frame)
    frame = undefined
    pending = true; pulled = 0; pullProgress.value = 0
    preview.value = neighbor
    const edge = (target === 'next' ? 1 : -1) * (height || surface.clientHeight)
    const offset = distance * edge < 0 ? distance : 0
    const token = revision
    surface.style.transform = translate(offset)
    layer.style.transform = translate(edge + offset)
    await nextTick()
    if (token !== revision) return

    const timing = { duration: ms * (1 - Math.min(.7, Math.abs(offset / (edge || 1)))), easing, fill: 'both' as const }
    animations = [
      surface.animate([{ transform: translate(offset) }, { transform: translate(-edge) }], timing),
      layer.animate([{ transform: translate(edge + offset) }, { transform: translate(0) }], timing),
    ]
    await Promise.allSettled(animations.map(animation => animation.finished))
    if (token !== revision || !options.active()) return
    if (options.mobile?.neighbor(target)?.key !== neighbor.key) { reset(); return }

    layer.style.transform = translate(0)
    surface.style.removeProperty('transform')
    animations.forEach(animation => animation.cancel()); animations = []
    pending = false; previewWaiting = true
    // Keep the landed cover over the same decoder until its new media is usable.
    timeout = setTimeout(reset, 15000)
    options.step(target)
  }
  function step(value: Direction): void {
    if (pending || frozen.value || returning || previewWaiting || !options.active()) return
    if (value === 'previous' && pullProgress.value === 1 && pulled >= pullThreshold && !options.canMove(value) && options.canRefresh?.()) {
      cancelDrag(); options.refresh?.(); return
    }
    if (!options.canMove(value)) {
      cancelDrag()
      return
    }
    if (options.mobile) { void slideMobile(value); return }
    cancelDrag()
    direction = value === 'next' ? 1 : -1; pending = true
    options.step(value)
  }
  function replace(work: () => void): void {
    if (pending || frozen.value || !options.active()) return
    cancelDrag(); direction = 1; pending = true; waitForMedia = true; work()
  }
  function capture(): void {
    if (options.mobile) return
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
    if (previewWaiting) { reset(); return }
    if (!options.active() || !frozen.value || animating) return
    const surface = options.surface.value, cover = options.cover.value
    if (!surface || !cover) { reset(); return }
    clearTimeout(timeout); timeout = undefined; animating = true
    const token = revision, timing = { duration: duration(), easing, fill: 'both' as const }
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
  return { frozen, preview, pullProgress, drag, cancelDrag, step, replace, capture, ready, reset, contact }
}
