import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { TsCacheLoader } from '../src/utils/ts-cache/loader'
import { diskCache } from '../src/utils/ts-cache/disk-cache'
import { cacheClear, cacheGet, setMemoryCacheBudget } from '../src/utils/ts-cache/memory-cache'
import { MAX_CACHED_BYTES, MAX_CACHED_SEGMENTS } from '../src/utils/ts-cache/constants'
import { beginCacheSession, prefetchFromSegments } from '../src/utils/ts-cache/prefetch'
import { session } from '../src/utils/ts-cache/store'

beforeEach(() => {
  vi.useFakeTimers()
  vi.stubGlobal('window', globalThis)
  // HLS FetchLoader uses the browser global even when fetch itself is mocked.
  vi.stubGlobal('self', globalThis)
  beginCacheSession()
  cacheClear()
  setMemoryCacheBudget(MAX_CACHED_BYTES, MAX_CACHED_SEGMENTS)
  session.currentEpKey = 'ep_fixture_1_1'
  session.enabled = true
  session.playbackBufferAhead = 60
  session.episodes = [{ source_key: 'fixture', vod_id: '1', ep_num: 1, ep_url: 'https://fixture/playlist' }]
  session.currentEpIdx = 0
  vi.spyOn(diskCache, 'save').mockResolvedValue()
})
afterEach(() => {
  beginCacheSession()
  cacheClear()
  vi.clearAllTimers()
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

function request(context: object = {}) {
  const loader = new TsCacheLoader({})
  let finish!: () => void
  const done = new Promise<void>(resolve => { finish = resolve })
  const success = vi.fn(finish), error = vi.fn(finish), abort = vi.fn(finish)
  loader.load({ url: 'https://fixture/17.ts', type: 'media-fragment', responseType: 'arraybuffer', ...context }, {}, {
    onSuccess: success, onError: error, onAbort: abort,
  })
  return { loader, success, error, abort, done }
}

describe('fragment disk reads through the real playback loader', () => {
  it('also reuses a persisted segment during scheduled prefetch without downloading it again', async () => {
    vi.spyOn(diskCache, 'loadSegment').mockResolvedValue(new Uint8Array([20, 21]).buffer)
    const network = vi.fn()
    vi.stubGlobal('fetch', network)
    prefetchFromSegments(['https://fixture/17.ts'], 0, 0, 1)
    await vi.advanceTimersByTimeAsync(301)
    expect(network).not.toHaveBeenCalled()
    expect(cacheGet('https://fixture/17.ts')?.size).toBe(2)
  })

  it('drops a disk prefetch result when its episode is replaced', async () => {
    let finish!: (data: ArrayBuffer | null) => void
    vi.spyOn(diskCache, 'loadSegment').mockImplementation(() => new Promise(resolve => { finish = resolve }))
    const network = vi.fn()
    vi.stubGlobal('fetch', network)
    prefetchFromSegments(['https://fixture/17.ts'], 0, 0, 1)
    await vi.advanceTimersByTimeAsync(301)
    beginCacheSession()
    finish(new ArrayBuffer(16))
    await vi.advanceTimersByTimeAsync(0)
    expect(network).not.toHaveBeenCalled()
    expect(cacheGet('https://fixture/17.ts')).toBeNull()
  })

  it('plays a disk hit offline, then serves the next read from memory without corrupting the cached binary', async () => {
    const bytes = new Uint8Array([17, 18, 19])
    const disk = vi.spyOn(diskCache, 'loadSegment').mockResolvedValue(bytes.buffer)
    const network = vi.fn().mockRejectedValue(new Error('offline'))
    vi.stubGlobal('fetch', network)
    const first = request()
    await vi.advanceTimersByTimeAsync(0)
    expect(first.success).toHaveBeenCalledOnce()
    // The loader returns an independent ArrayBuffer.
    new Uint8Array(first.success.mock.calls[0][0].data)[0] = 99
    const second = request()
    await vi.advanceTimersByTimeAsync(0)
    expect(new Uint8Array(second.success.mock.calls[0][0].data)).toEqual(bytes)
    expect(disk).toHaveBeenCalledOnce()
    expect(network).not.toHaveBeenCalled()
    first.loader.destroy(); second.loader.destroy()
  })

  it.each(['missing', 'unavailable'])('falls back to the network when disk data is %s', async (mode) => {
    const disk = vi.spyOn(diskCache, 'loadSegment')
    if (mode === 'missing') disk.mockResolvedValue(null)
    else disk.mockRejectedValue(new Error('IndexedDB unavailable'))
    const network = vi.fn(async () => new Response(new Uint8Array([3, 4])))
    vi.stubGlobal('fetch', network)
    const result = request()
    await result.done
    expect(network).toHaveBeenCalledOnce()
    expect(new Uint8Array(result.success.mock.calls[0][0].data)).toEqual(new Uint8Array([3, 4]))
    expect(result.error).not.toHaveBeenCalled()
    result.loader.destroy()
  })

  it('falls back when a disk read stalls, and ignores its later binary', async () => {
    let finish!: (data: ArrayBuffer | null) => void
    vi.spyOn(diskCache, 'loadSegment').mockImplementation(() => new Promise(resolve => { finish = resolve }))
    const network = vi.fn(async () => new Response(new Uint8Array([3, 4])))
    vi.stubGlobal('fetch', network)
    const result = request()
    await vi.advanceTimersByTimeAsync(249)
    expect(network).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(1)
    await result.done
    expect(network).toHaveBeenCalledOnce()
    expect(result.success).toHaveBeenCalledOnce()
    finish(new Uint8Array([99]).buffer)
    await vi.advanceTimersByTimeAsync(0)
    expect(result.success).toHaveBeenCalledOnce()
    expect(new Uint8Array(cacheGet('https://fixture/17.ts')!.buffer)).toEqual(new Uint8Array([3, 4]))
    result.loader.destroy()
  })

  it.each([null, new ArrayBuffer(16)])('does not deliver or start a network request after aborting a pending disk read (%s)', async (buffer) => {
    let finish!: (data: ArrayBuffer | null) => void
    vi.spyOn(diskCache, 'loadSegment').mockImplementation(() => new Promise(resolve => { finish = resolve }))
    const network = vi.fn()
    vi.stubGlobal('fetch', network)
    const result = request()
    result.loader.abort()
    finish(buffer)
    await vi.advanceTimersByTimeAsync(0)
    expect(result.success).not.toHaveBeenCalled()
    expect(network).not.toHaveBeenCalled()
    expect(result.abort).toHaveBeenCalledOnce()
    expect(cacheGet('https://fixture/17.ts')).toBeNull()
    result.loader.destroy()
  })

  it('keeps byte ranges separate on disk and in memory', async () => {
    const disk = vi.spyOn(diskCache, 'loadSegment').mockResolvedValue(new Uint8Array([8, 9]).buffer)
    const network = vi.fn()
    vi.stubGlobal('fetch', network)
    const result = request({ rangeStart: 8, rangeEnd: 10 })
    await vi.advanceTimersByTimeAsync(0)
    expect(disk.mock.calls[0][0]).toContain('8-9')
    expect(new Uint8Array(result.success.mock.calls[0][0].data)).toEqual(new Uint8Array([8, 9]))
    expect(cacheGet('https://fixture/17.ts')).toBeNull()
    expect(cacheGet('https://fixture/17.ts', '8-9')?.size).toBe(2)
    expect(network).not.toHaveBeenCalled()
    result.loader.destroy()
  })
})
