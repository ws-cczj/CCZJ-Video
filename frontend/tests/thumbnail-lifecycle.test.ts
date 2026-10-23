import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { effectScope, nextTick, ref, shallowRef } from 'vue'
import { useThumbnailScrubber } from '../src/player/useThumbnailScrubber'

const hls = vi.hoisted(() => ({ instances: [] as any[] }))
vi.mock('../src/utils/tsCache', () => ({ TsCache: { TsCacheLoader: class {} } }))
vi.mock('hls.js', () => ({ default: class {
  static isSupported() { return true }
  destroy = vi.fn()
  loadSource = vi.fn()
  attachMedia = vi.fn()
  constructor() { hls.instances.push(this) }
} }))

class MediaElement {
  style: Record<string, string> = {}
  src = ''
  currentTime = 0
  duration = 100
  readyState = 2
  videoWidth = 160
  videoHeight = 90
  width = 160
  height = 90
  muted = false
  preload = ''
  playsInline = false
  listeners = new Map<string, Set<Function>>()
  pause = vi.fn()
  load = vi.fn()
  parent: MediaElement[] | null = null
  constructor(public tagName: string) {}
  setAttribute() {}
  removeAttribute(name: string) { if (name === 'src') this.src = '' }
  remove() { const at = this.parent?.indexOf(this) ?? -1; if (at >= 0) this.parent?.splice(at, 1); this.parent = null }
  getContext() { return { drawImage() {} } }
  toDataURL() { return 'data:image/jpeg;base64,x' }
  addEventListener(name: string, callback: Function) { if (!this.listeners.has(name)) this.listeners.set(name, new Set()); this.listeners.get(name)!.add(callback) }
  removeEventListener(name: string, callback: Function) { this.listeners.get(name)?.delete(callback) }
}

let scopes: ReturnType<typeof effectScope>[] = []
beforeEach(() => { vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout', 'setInterval', 'clearInterval', 'Date', 'performance'] }); vi.stubGlobal('window', globalThis); hls.instances.length = 0 })
afterEach(() => { scopes.forEach(scope => scope.stop()); scopes = []; vi.clearAllTimers(); vi.useRealTimers(); vi.unstubAllGlobals() })

function setup(isHls = false) {
  const children: MediaElement[] = []
  const created: MediaElement[] = []
  vi.stubGlobal('document', { createElement: (tag: string) => { const element = new MediaElement(tag); created.push(element); return element } })
  const url = ref('https://test/video.mp4')
  let main: MediaElement | null = new MediaElement('video')
  main.src = url.value
  const wrapper = { appendChild: (element: MediaElement) => { children.push(element); element.parent = children } }
  const scope = effectScope()
  scopes.push(scope)
  const scrubber = scope.run(() => useThumbnailScrubber({
    wrapperRef: shallowRef(wrapper as unknown as HTMLDivElement),
    progressContainerRef: shallowRef({ getBoundingClientRect: () => ({ left: 0, width: 100 }) } as HTMLDivElement),
    thumbCanvasRef: shallowRef<HTMLCanvasElement | null>(null),
    getVideoEl: () => main as unknown as HTMLVideoElement,
    getUrl: () => url.value,
    isHlsUrl: () => isHls,
  }))!
  return { scrubber, children, created, url, loseMain: () => { main = null } }
}

describe('thumbnail media resource lifetime', () => {
  it('does not create a second decoder before the progress bar is used', async () => {
    const { scrubber, children } = setup(true)
    await scrubber.initThumbSampler()
    expect(children).toHaveLength(0)
    expect(hls.instances).toHaveLength(0)
  })
  it('owns at most one preview video across repeated initialization', async () => {
    const { scrubber, children } = setup()
    await scrubber.initThumbSampler()
    scrubber.onProgressHover({ clientX: 50 } as MouseEvent)
    await vi.advanceTimersByTimeAsync(0)
    await scrubber.initThumbSampler()
    await scrubber.initThumbSampler()
    expect(children.filter(child => child.tagName === 'video')).toHaveLength(1)
    scrubber.destroyThumbSampler()
    expect(children).toHaveLength(0)
  })

  it('cancels a pending HLS import when the main element disappears before cleanup', async () => {
    const { scrubber, loseMain, children } = setup(true)
    await scrubber.initThumbSampler()
    scrubber.onProgressHover({ clientX: 50 } as MouseEvent)
    loseMain()
    scrubber.destroyThumbSampler()
    await vi.advanceTimersByTimeAsync(0)
    expect(children).toHaveLength(0)
    expect(hls.instances.filter(instance => instance.destroy.mock.calls.length === 0)).toHaveLength(0)
  })

  it('releases media source and URL watcher even after losing the main ref', async () => {
    const { scrubber, created, loseMain, url } = setup()
    await scrubber.initThumbSampler()
    scrubber.onProgressHover({ clientX: 50 } as MouseEvent)
    await vi.advanceTimersByTimeAsync(0)
    const preview = created.find(element => element.tagName === 'video')!
    loseMain()
    scrubber.destroyThumbSampler()
    expect(preview.src).toBe('')
    expect(preview.pause).toHaveBeenCalled()
    expect(preview.load).toHaveBeenCalled()
    url.value = 'https://test/new.mp4'
    await nextTick()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('removes timed-out seek handlers instead of accumulating them during playback', async () => {
    const { scrubber, created } = setup()
    await scrubber.initThumbSampler()
    scrubber.onProgressHover({ clientX: 50 } as MouseEvent)
    await vi.advanceTimersByTimeAsync(600)
    const preview = created.find(element => element.tagName === 'video')!
    for (let index = 0; index < 20; index++) {
      await vi.advanceTimersByTimeAsync(600)
      scrubber.onProgressHover({ clientX: 50 + index % 10 } as MouseEvent)
      await vi.advanceTimersByTimeAsync(600)
      expect(preview.listeners.get('seeked')?.size ?? 0).toBeLessThanOrEqual(1)
    }
    // One active seek may still hand off to the latest queued target. Let both
    // bounded seeks (2.5s each) and the scheduler finish without more input.
    await vi.advanceTimersByTimeAsync(5200)
    expect(preview.listeners.get('seeked')?.size ?? 0).toBe(0)
    scrubber.destroyThumbSampler()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('releases an idle preview and does not recreate it on a URL change after closing', async () => {
    const { scrubber, children, url } = setup()
    await scrubber.initThumbSampler()
    scrubber.onProgressHover({ clientX: 50 } as MouseEvent)
    await vi.advanceTimersByTimeAsync(0)
    expect(children).toHaveLength(2)
    scrubber.onProgressLeave()
    await vi.advanceTimersByTimeAsync(10000)
    expect(children).toHaveLength(0)
    scrubber.onProgressHover({ clientX: 60 } as MouseEvent)
    await vi.advanceTimersByTimeAsync(0)
    expect(children).toHaveLength(2)
    scrubber.destroyThumbSampler()
    url.value = 'https://test/replaced.mp4'
    await nextTick()
    scrubber.onProgressHover({ clientX: 70 } as MouseEvent)
    await vi.advanceTimersByTimeAsync(1000)
    expect(children).toHaveLength(0)
    expect(vi.getTimerCount()).toBe(0)
  })
})
