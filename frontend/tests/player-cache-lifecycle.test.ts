import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { TsCache } from '../src/utils/tsCache'
import { cacheSet, cacheClear, cacheHas } from '../src/utils/ts-cache/memory-cache'
import { cache, prefetchState, session } from '../src/utils/ts-cache/store'
import { diskCache, diskLoad } from '../src/utils/ts-cache/disk-cache'
import { beginCacheSession, prefetchFromSegments } from '../src/utils/ts-cache/prefetch'

const releases: (() => void)[] = []
function retain() { const release = TsCache.retainPlayback(); releases.push(release); return release }
function seed() {
  for (let index = 0; index < 70; index++) cacheSet(`https://fixture/${index}.ts`, new ArrayBuffer(1024 * 1024), `episode-${Math.floor(index / 32)}`)
}

beforeEach(() => {
  vi.useFakeTimers()
  vi.stubGlobal('window', globalThis)
  beginCacheSession()
  cacheClear()
  session.enabled = true
  session.playbackBufferAhead = 60
  session.episodes = [{ source_key: 'fixture', vod_id: '1', ep_num: 1, ep_url: 'https://fixture/playlist' }]
  session.currentEpKey = 'ep_fixture_1_1'
  session.currentEpIdx = 0
})
afterEach(async () => {
  for (const release of releases.splice(0)) release()
  await vi.advanceTimersByTimeAsync(30_000)
  beginCacheSession()
  cacheClear()
  vi.restoreAllMocks()
  vi.clearAllTimers()
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('playback cache ownership and idle release', () => {
  it('preserves playing and quick-reopen bytes, then retains only the newest 8 MiB while idle', async () => {
    const release = retain()
    seed()
    await vi.advanceTimersByTimeAsync(120_000)
    expect(cache.totalCacheBytes).toBe(70 * 1024 ** 2)
    release()
    expect(session.currentEpKey).toBe('')
    expect(session.episodes).toEqual([])
    expect(session.enabled).toBe(false)
    await vi.advanceTimersByTimeAsync(29_999)
    expect(cache.totalCacheBytes).toBe(70 * 1024 ** 2)
    await vi.advanceTimersByTimeAsync(1)
    expect(cache.totalCacheBytes).toBeLessThanOrEqual(8 * 1024 ** 2)
    expect(cacheHas('https://fixture/69.ts')).toBe(true)
    expect(cacheHas('https://fixture/0.ts')).toBe(false)
    expect([...cache.epCacheKeys.values()].reduce((sum, keys) => sum + keys.size, 0)).toBe(cache.lruCache.size)
    // A late opportunistic preload must also obey the idle budget.
    cacheSet('https://fixture/oversized-idle.ts', new ArrayBuffer(10 * 1024 ** 2), 'idle')
    expect(cacheHas('https://fixture/69.ts')).toBe(true)
    for (let index = 0; index < 30; index++) cacheSet(`https://fixture/idle-${index}.ts`, new ArrayBuffer(1024 * 1024), 'idle')
    expect(cache.totalCacheBytes).toBeLessThanOrEqual(8 * 1024 ** 2)
  })

  it('cancels a prior close deadline when another player opens and releases idempotently', async () => {
    const first = retain()
    seed()
    first()
    await vi.advanceTimersByTimeAsync(10_000)
    const second = retain()
    first()
    await vi.advanceTimersByTimeAsync(40_000)
    expect(cache.totalCacheBytes).toBe(70 * 1024 ** 2)
    expect(TsCache.stats().activePlayers).toBe(1)
    second()
    await vi.advanceTimersByTimeAsync(30_000)
    expect(cache.totalCacheBytes).toBeLessThanOrEqual(8 * 1024 ** 2)
  })

  it('does not clear the shared session while another playback owner is still mounted', async () => {
    const first = retain(), second = retain()
    seed()
    const generation = prefetchState.cacheSession
    first()
    await vi.advanceTimersByTimeAsync(40_000)
    expect(prefetchState.cacheSession).toBe(generation)
    expect(session.currentEpKey).toBe('ep_fixture_1_1')
    expect(cache.totalCacheBytes).toBe(70 * 1024 ** 2)
    second()
    expect(prefetchState.cacheSession).toBeGreaterThan(generation)
    await vi.advanceTimersByTimeAsync(30_000)
    expect(cache.totalCacheBytes).toBeLessThanOrEqual(8 * 1024 ** 2)
  })

  it('aborts pending prefetch on last close and rejects a late response body', async () => {
    const release = retain()
    let finish!: (body: ArrayBuffer) => void
    let signal!: AbortSignal
    vi.stubGlobal('fetch', vi.fn(async (_url, options) => {
      signal = options.signal
      return { ok: true, arrayBuffer: () => new Promise<ArrayBuffer>(resolve => { finish = resolve }) }
    }))
    prefetchFromSegments(['https://fixture/late.ts'], 0, 0, 1)
    await vi.advanceTimersByTimeAsync(301)
    expect(finish).toBeTypeOf('function')
    release()
    expect(signal.aborted).toBe(true)
    finish(new ArrayBuffer(1024))
    await vi.advanceTimersByTimeAsync(0)
    expect(cache.totalCacheBytes).toBe(0)
    expect(prefetchState.queue).toHaveLength(0)
  })

  it('does not restore an old disk read after closing and reopening the same episode', async () => {
    const release = retain()
    let finish!: (buffer: ArrayBuffer) => void
    vi.spyOn(diskCache, 'loadSegment').mockImplementation(() => new Promise(resolve => { finish = resolve }))
    const loading = diskLoad('https://fixture/late-disk.ts')
    release()
    retain()
    session.currentEpKey = 'ep_fixture_1_1'
    finish(new ArrayBuffer(1024))
    expect(await loading).toBeNull()
    expect(cache.totalCacheBytes).toBe(0)
  })
})
