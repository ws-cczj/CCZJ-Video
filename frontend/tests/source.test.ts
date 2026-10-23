import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPlaybackSource } from '../src/player/hls/playbackSource'
import { createPlaybackSession } from '../src/player/playbackSession'

const fakes = vi.hoisted(() => ({ instances: [] as any[], cache: {
  enable: vi.fn(), fetchAndParseM3u8: vi.fn(), setSegments: vi.fn(), setTargetDuration: vi.fn(),
  setAbrSwitchCallback: vi.fn(), notifyCurrentTs: vi.fn(), TsCacheLoader: class {},
} }))
vi.mock('../src/utils/tsCache', () => ({ TsCache: fakes.cache }))
vi.mock('hls.js', () => ({ default: class {
  static isSupported() { return true }
  static Events = { LEVEL_LOADED: 'level', FRAG_CHANGED: 'fragment', ERROR: 'error' }
  static ErrorTypes = { NETWORK_ERROR: 'network', MEDIA_ERROR: 'media' }
  listeners = new Map<string, Function>()
  levels = [{ bitrate: 500000 }, { bitrate: 1000000 }]
  currentLevel = 1
  nextAutoLevel = 1
  destroy = vi.fn()
  loadSource = vi.fn()
  attachMedia = vi.fn()
  startLoad = vi.fn()
  recoverMediaError = vi.fn()
  constructor() { fakes.instances.push(this) }
  on(name: string, listener: Function) { this.listeners.set(name, listener) }
  emit(name: string, data: unknown) { this.listeners.get(name)?.(name, data) }
} }))

beforeEach(() => {
  vi.useFakeTimers()
  fakes.instances.length = 0
  fakes.cache.fetchAndParseM3u8.mockResolvedValue({ urls: ['https://media/1.ts'], targetduration: 6, isMaster: false })
})
afterEach(() => vi.useRealTimers())

function setup() {
  const video = { paused: true, ended: false, src: '', onerror: null } as unknown as HTMLVideoElement
  const lease = createPlaybackSession()
  const events = { streams: vi.fn(), error: vi.fn(), fatal: vi.fn(), unrecoverable: vi.fn(), bufferChanged: vi.fn() }
  const source = createPlaybackSource(events)
  return { video, lease, source, events }
}

describe('stream loading and recovery', () => {
  it('does not attach media or mutate the segment table after a stale playlist read', async () => {
    const { video, lease, source, events } = setup()
    let resolve!: (value: unknown) => void
    const reading = new Promise(yes => { resolve = yes })
    fakes.cache.fetchAndParseM3u8.mockReturnValueOnce(reading)
    const load = source.load(video, 'https://media/1.m3u8', lease.capture())
    // Wait for the dynamic HLS import to reach the pending playlist read.
    await vi.waitFor(() => expect(fakes.cache.fetchAndParseM3u8).toHaveBeenCalled())
    lease.reset()
    resolve({ urls: ['https://media/old.ts'], targetduration: 6, isMaster: false })
    expect(await load).toBe(false)
    expect(fakes.instances).toHaveLength(0)
    expect(events.streams).not.toHaveBeenCalled()
    source.dispose()
  })

  it('ignores late HLS fragment and error callbacks after disposal', async () => {
    const { video, lease, source, events } = setup()
    await source.load(video, 'https://media/1.m3u8', lease.capture())
    const old = fakes.instances[0]
    lease.reset()
    source.dispose(video)
    old.emit('fragment', { frag: { url: 'old.ts' } })
    old.emit('error', { fatal: true, type: 'media', details: 'old-error' })
    expect(fakes.cache.notifyCurrentTs).not.toHaveBeenCalled()
    expect(events.fatal).not.toHaveBeenCalled()
    expect(old.recoverMediaError).not.toHaveBeenCalled()
    expect(old.destroy).toHaveBeenCalledOnce()
  })

  it('recovers network loading without treating pause as a timeout', async () => {
    const { video, lease, source, events } = setup()
    await source.load(video, 'https://media/1.m3u8', lease.capture())
    const hls = fakes.instances[0]
    hls.emit('error', { fatal: true, type: 'network', details: 'fragmentLoadError' })
    expect(vi.getTimerCount()).toBe(0)
    Object.defineProperty(video, 'paused', { value: false, writable: true })
    hls.emit('error', { fatal: true, type: 'network', details: 'fragmentLoadError' })
    Object.defineProperty(video, 'paused', { value: true, writable: true })
    vi.advanceTimersByTime(10000)
    expect(events.error).not.toHaveBeenCalled()
    source.dispose(video)
  })

  it('falls back to direct video for a non-HLS manifest and releases the HLS instance', async () => {
    const { video, lease, source } = setup()
    await source.load(video, 'https://media/video.webm', lease.capture())
    const hls = fakes.instances[0]
    hls.emit('error', { fatal: true, details: 'manifestParsingError' })
    expect(hls.destroy).toHaveBeenCalledOnce()
    expect(video.src).toMatch(/^\/__cczj\/hls\?u=/)
    source.dispose(video)
  })
})
