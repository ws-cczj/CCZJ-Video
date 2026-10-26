import { afterEach, describe, expect, it, vi } from 'vitest'
import { createPlaybackSession } from '../src/player/playbackSession'
import { createPlaybackControl } from '../src/player/playbackControl'
import { useHlsEngine } from '../src/player/hls/useHlsEngine'
import { createEnhancementPipeline } from '../src/player/enhancementPipeline'
import { resumeStreaming } from '../src/player/hls/resumeStreaming'

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (error: unknown) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}

afterEach(() => vi.useRealTimers())

describe('media replacement and disposal', () => {
  it('invalidates old asynchronous loads, events and delayed fullscreen work', () => {
    vi.useFakeTimers()
    const sessions = createPlaybackSession()
    const old = sessions.capture()
    const callback = vi.fn()
    const listener = old.guard(callback)
    sessions.later(callback, 80)
    sessions.reset()
    listener()
    vi.advanceTimersByTime(100)
    expect(old.current()).toBe(false)
    expect(old.signal.aborted).toBe(true)
    expect(callback).not.toHaveBeenCalled()
    const current = sessions.capture()
    current.guard(callback)()
    expect(callback).toHaveBeenCalledOnce()
    sessions.reset()
  })

  it('destroys HLS when the element changes and leaves the replacement intact during late cleanup', () => {
    const engine = useHlsEngine()
    class Hls {
      destroy = vi.fn()
    }
    const first = {} as HTMLVideoElement
    const second = {} as HTMLVideoElement
    const ctor = Hls as unknown as typeof import('hls.js').default
    const old = engine.create(first, {}, ctor)
    const active = engine.create(second, {}, ctor)
    expect(old.destroy).toHaveBeenCalledOnce()
    expect((first as any).__hls).toBeUndefined()
    engine.dispose(first)
    expect(active.destroy).not.toHaveBeenCalled()
    engine.dispose()
    engine.dispose()
    expect(active.destroy).toHaveBeenCalledOnce()
    expect((second as any).__hls).toBeUndefined()
  })

  it('does not start an enhancement whose init completes after a switch', async () => {
    const pipeline = createEnhancementPipeline()
    const init = deferred<boolean>()
    const old = { init: vi.fn(() => init.promise), start: vi.fn(), stop: vi.fn(), destroy: vi.fn() }
    const next = { init: vi.fn(async () => true), start: vi.fn(), stop: vi.fn(), destroy: vi.fn() }
    const pending = pipeline.start(async () => old, {} as HTMLVideoElement)
    await Promise.resolve()
    await pipeline.start(async () => next, {} as HTMLVideoElement)
    init.resolve(true)
    expect(await pending).toBeNull()
    expect(old.start).not.toHaveBeenCalled()
    expect(old.destroy).toHaveBeenCalled()
    expect(pipeline.current()).toBe(next)
    pipeline.reset()
    expect(next.destroy).toHaveBeenCalledOnce()
  })

  it('releases instances returned by a late shader read without initializing them', async () => {
    const pipeline = createEnhancementPipeline()
    const read = deferred<any>()
    const instance = { init: vi.fn(), start: vi.fn(), stop: vi.fn(), destroy: vi.fn() }
    const pending = pipeline.start(() => read.promise, {} as HTMLVideoElement)
    pipeline.reset()
    read.resolve(instance)
    await pending
    expect(instance.init).not.toHaveBeenCalled()
    expect(instance.destroy).toHaveBeenCalledOnce()
  })
})

describe('play and pause', () => {
  it('restarts HLS at the current position after a long pause, without discarding a short pause buffer', () => {
    const startLoad = vi.fn()
    const video = { readyState: 2, currentTime: 42, __hls: { startLoad } }
    resumeStreaming(video as unknown as HTMLVideoElement)
    expect(startLoad).toHaveBeenCalledWith(42)
    video.readyState = 3
    resumeStreaming(video as unknown as HTMLVideoElement)
    expect(startLoad).toHaveBeenCalledOnce()
  })
  function setup(error: string) {
    const video = { muted: false, play: vi.fn().mockRejectedValueOnce({ name: error }).mockResolvedValue(undefined) }
    const onMuted = vi.fn()
    const onUnsupported = vi.fn()
    const playback = createPlaybackControl({ getVideo: () => video as unknown as HTMLVideoElement, onMuted, onUnsupported })
    return { video, playback, onMuted, onUnsupported }
  }

  it('retries a denied autoplay muted', async () => {
    const { video, playback, onMuted } = setup('NotAllowedError')
    await playback.play(true)
    expect(video.play).toHaveBeenCalledTimes(2)
    expect(video.muted).toBe(true)
    expect(onMuted).toHaveBeenCalledOnce()
  })

  it.each(['AbortError', 'NetworkError'])('does not mute or retry %s', async error => {
    const { video, playback } = setup(error)
    await playback.play(true)
    expect(video.play).toHaveBeenCalledOnce()
    expect(video.muted).toBe(false)
  })

  it('preserves volume on user-initiated play', async () => {
    const { video, playback } = setup('NotAllowedError')
    await playback.play(false)
    expect(video.muted).toBe(false)
  })

  it('a pause cancels an outstanding autoplay fallback', async () => {
    const request = deferred<void>()
    const { video, playback } = setup('NotAllowedError')
    video.play.mockReset().mockReturnValue(request.promise)
    const pending = playback.play(true)
    playback.cancel()
    request.reject({ name: 'NotAllowedError' })
    await pending
    expect(video.play).toHaveBeenCalledOnce()
    expect(video.muted).toBe(false)
  })

  it('reports unsupported media without retrying', async () => {
    const { video, playback, onUnsupported } = setup('NotSupportedError')
    await playback.play(true)
    expect(onUnsupported).toHaveBeenCalledOnce()
    expect(video.play).toHaveBeenCalledOnce()
  })

  it('distinguishes an interrupted automatic start so readiness can retry it', async () => {
    const { video, playback } = setup('AbortError')
    expect(await playback.play(true)).toBe('interrupted')
    expect(await playback.play(true)).toBe('playing')
    expect(video.muted).toBe(false)
  })
})
