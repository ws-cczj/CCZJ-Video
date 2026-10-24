import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cacheSet, cacheClear, cacheHas } from '../src/utils/ts-cache/memory-cache'
import { beginCacheSession, prefetchFromSegments } from '../src/utils/ts-cache/prefetch'
import { cache, prefetchState, session } from '../src/utils/ts-cache/store'
import { MAX_CACHED_BYTES } from '../src/utils/ts-cache/constants'
import { TsCacheLoader } from '../src/utils/ts-cache/loader'

vi.mock('../src/utils/ts-cache/disk-cache', () => ({ diskSave: vi.fn(async () => {}), diskLoad: vi.fn(async () => null) }))

beforeEach(() => {
  vi.useFakeTimers()
  vi.stubGlobal('window', globalThis)
  vi.stubGlobal('self', globalThis)
  cacheClear()
  beginCacheSession()
  session.enabled = true
  session.playbackBufferAhead = 60
  session.episodes = [{ source_key: 'test', vod_id: '1', ep_num: 1, ep_url: 'https://test/playlist' }]
  session.currentEpIdx = -1
  session.currentEpKey = ''
})
afterEach(async () => { beginCacheSession(); await vi.advanceTimersByTimeAsync(0); cacheClear(); vi.clearAllTimers(); prefetchState.debounceTimer = null; vi.useRealTimers(); vi.unstubAllGlobals() })

describe('segment memory bounds and asynchronous session release', () => {
  it('rejects a movie returned to the HLS manifest loader without reading it as text', async () => {
    const response = new Response('binary movie', { headers: { 'Content-Type': 'video/mp4' } })
    const text = vi.spyOn(response, 'text')
    vi.stubGlobal('fetch', vi.fn(async () => response))
    const loader = new TsCacheLoader({})
    const success = vi.fn(), error = vi.fn()
    loader.load({ url: 'https://test/movie.mp4', type: 'manifest' }, {}, { onSuccess: success, onError: error })
    await vi.dynamicImportSettled()
    await vi.advanceTimersByTimeAsync(0)
    expect(text).not.toHaveBeenCalled()
    expect(success).not.toHaveBeenCalled()
    expect(error).toHaveBeenCalledOnce()
    loader.destroy()
  })
  it('does not deliver a cached fragment after the loader is aborted', async () => {
    cacheSet('https://test/ready.ts', new ArrayBuffer(128), 'test')
    const loader = new TsCacheLoader({})
    const success = vi.fn()
    const aborted = vi.fn()
    const context = { url: 'https://test/ready.ts', type: 'media-fragment', responseType: 'arraybuffer' }
    loader.load(context, {}, { onSuccess: success, onAbort: aborted })
    expect(loader.context).toBe(context)
    loader.abort()
    await vi.advanceTimersByTimeAsync(0)
    expect(success).not.toHaveBeenCalled()
    expect(loader.stats.aborted).toBe(true)
    expect(aborted).toHaveBeenCalledOnce()
    loader.destroy()
    expect(loader.context).toBeNull()
  })
  it('delivers only the latest request when a loader is reused before cache delivery', async () => {
    cacheSet('https://test/first.ts', new ArrayBuffer(64), 'test')
    cacheSet('https://test/next.ts', new ArrayBuffer(96), 'test')
    const loader = new TsCacheLoader({})
    const first = vi.fn(), latest = vi.fn(), firstAbort = vi.fn(), latestAbort = vi.fn()
    loader.load({ url: 'https://test/first.ts', type: 'media-fragment' }, {}, { onSuccess: first, onAbort: firstAbort })
    loader.load({ url: 'https://test/next.ts', type: 'media-fragment' }, {}, { onSuccess: latest, onAbort: latestAbort })
    await vi.advanceTimersByTimeAsync(0)
    expect(first).not.toHaveBeenCalled()
    expect(firstAbort).toHaveBeenCalledOnce()
    expect(latest).toHaveBeenCalledOnce()
    expect(latest.mock.calls[0][0].data.byteLength).toBe(96)
    loader.abort()
    expect(latestAbort).not.toHaveBeenCalled()
    loader.destroy()
    // HLS consumes these stats after FragmentLoader destroys the loader.
    expect(loader.stats.aborted).toBe(false)
  })
  it('does not evict useful entries or retain a single segment larger than the entire budget', () => {
    cacheSet('https://test/small.ts', new ArrayBuffer(64), 'test')
    cacheSet('https://test/oversized.ts', { byteLength: MAX_CACHED_BYTES + 1 } as ArrayBuffer, 'test')
    expect(cache.totalCacheBytes).toBeLessThanOrEqual(MAX_CACHED_BYTES)
    expect(cacheHas('https://test/small.ts')).toBe(true)
    expect(cacheHas('https://test/oversized.ts')).toBe(false)
  })

  it('does not cache a response body that finishes after switching sessions', async () => {
    let finish!: (buffer: ArrayBuffer) => void
    const body = new Promise<ArrayBuffer>(resolve => { finish = resolve })
    vi.stubGlobal('fetch', vi.fn(async () => ({ ok: true, arrayBuffer: () => body })))
    prefetchFromSegments(['https://test/old.ts'], 0, 0, 1)
    await vi.advanceTimersByTimeAsync(301)
    beginCacheSession()
    finish(new ArrayBuffer(128))
    await vi.advanceTimersByTimeAsync(0)
    expect(cache.totalCacheBytes).toBe(0)
    expect(prefetchState.prefetchControllers.size).toBe(0)
  })

  it('keeps a timeout for bodies that stall after response headers', async () => {
    let aborted = false
    vi.stubGlobal('fetch', vi.fn(async (_url, options) => ({
      ok: true,
      arrayBuffer: () => new Promise((_resolve, reject) => {
        options.signal.addEventListener('abort', () => { aborted = true; reject(new Error('aborted')) }, { once: true })
      }),
    })))
    prefetchFromSegments(['https://test/stalled.ts'], 0, 0, 1)
    await vi.advanceTimersByTimeAsync(60_000)
    expect(aborted).toBe(true)
    expect(prefetchState.prefetchControllers.size).toBe(0)
    expect(prefetchState.inflightBySession.size).toBe(0)
  })
})
