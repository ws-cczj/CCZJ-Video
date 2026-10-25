import { afterEach, expect, it, vi } from 'vitest'

const host = vi.hoisted(() => ({ ios: true }))
const hls = vi.hoisted(() => ({ loadRuntime: vi.fn() }))
vi.mock('../src/player/hls/useHlsEngine', () => ({ useHlsEngine: () => ({ loadRuntime: hls.loadRuntime }) }))
vi.mock('../src/api/runtime', () => ({ isIOSRuntime: () => host.ios }))
vi.mock('../src/api/app', () => ({ WindowIsFs: vi.fn(), WindowIsMax: vi.fn(), WindowSetFullscreen: vi.fn(), WindowToggleMax: vi.fn(), OpenFileInExplorer: vi.fn(), OpenFolder: vi.fn() }))
vi.mock('../src/platform/presentation', () => ({ presentationProfile: () => ({ ui: 'mobile', host: 'native-mobile' }) }))
vi.mock('../src/locales', () => ({ tr: (key: string) => key }))

afterEach(() => { vi.unstubAllGlobals(); vi.restoreAllMocks(); host.ios = true })

it('routes iOS media to a validated loopback capability and does not cache aborted discovery', async () => {
  vi.resetModules()
  const { playbackTransportURL } = await import('../src/platform/mediaTransport')
  const fetcher = vi.fn(async () => ({ ok: true, json: async () => ({ endpoint: 'http://127.0.0.1:1234/' + 'a'.repeat(64) }) }))
  vi.stubGlobal('fetch', fetcher)
  const cancelled = new AbortController(); cancelled.abort()
  await expect(playbackTransportURL('/__cczj/hls?u=first', cancelled.signal)).rejects.toBeDefined()
  const signal = new AbortController().signal
  expect(await playbackTransportURL('/__cczj/hls?u=second', signal)).toBe('http://127.0.0.1:1234/' + 'a'.repeat(64) + '/__cczj/ios-hls?u=second')
  expect(fetcher).toHaveBeenCalledTimes(2)
  host.ios = false
  expect(await playbackTransportURL('/__cczj/hls?u=desktop', signal)).toBe('/__cczj/hls?u=desktop')
})

it.each(['https://example.com/private', 'http://localhost:1234/' + 'a'.repeat(64), 'http://127.0.0.1:1234/not-a-capability'])('rejects an invalid native media endpoint %s', async endpoint => {
  vi.resetModules()
  const { playbackTransportURL } = await import('../src/platform/mediaTransport')
  vi.stubGlobal('fetch', vi.fn(async () => ({ ok: true, json: async () => ({ endpoint }) })))
  await expect(playbackTransportURL('/__cczj/hls?u=x', new AbortController().signal)).rejects.toThrow('Invalid mobile media')
})

it('provides iOS viewport immersion without calling unsupported element/window fullscreen', async () => {
  const { createFullscreenPlatform } = await import('../src/platform/fullscreen')
  const requestFullscreen = vi.fn()
  const root = { requestFullscreen } as unknown as HTMLElement
  const platform = createFullscreenPlatform()
  expect(platform.kind).toBe('viewport')
  expect(await platform.enter(root, () => false)).toBe(false)
  expect(await platform.enter(root, () => true)).toBe(true)
  expect(platform.browserActive(root)).toBe(true)
  expect(requestFullscreen).not.toHaveBeenCalled()
  await platform.release(root, () => true)
  expect(platform.browserActive(root)).toBe(false)
})

it('uses WebKit video PiP on iOS without requiring the Android player bridge', async () => {
  const { createPlayerPlatform } = await import('../src/player/platform')
  const change = vi.fn()
  const video = { webkitSupportsPresentationMode: () => true, webkitSetPresentationMode: change, webkitPresentationMode: 'picture-in-picture' } as unknown as HTMLVideoElement
  const platform = createPlayerPlatform()
  expect(platform.pip.kind).toBe('element')
  expect(platform.pip.active(video)).toBe(true)
  expect(platform.device.volumeSupported).toBe(false)
  expect(platform.cast.supported).toBe(false)
  expect(platform.pip.supported(video)).toBe(true)
  expect(await platform.pip.enter(video)).toBe(true)
  expect(change).toHaveBeenCalledWith('picture-in-picture')
  await platform.pip.exit(video)
  expect(change).toHaveBeenCalledWith('inline')
})

it('attaches native iOS HLS without creating a second MSE player', async () => {
  vi.resetModules()
  const { createPlaybackSource } = await import('../src/player/hls/playbackSource')
  const { createPlaybackSession } = await import('../src/player/playbackSession')
  vi.stubGlobal('fetch', vi.fn(async () => ({ ok: true, json: async () => ({ endpoint: 'http://127.0.0.1:1234/' + 'a'.repeat(64) }) })))
  const video = { canPlayType: () => 'probably', src: '' } as unknown as HTMLVideoElement
  const source = createPlaybackSource({ streams: vi.fn(), error: vi.fn(), fatal: vi.fn(), unrecoverable: vi.fn(), bufferChanged: vi.fn() })
  expect(await source.load(video, 'https://media.example/episode.m3u8', createPlaybackSession().capture())).toBe(true)
  expect(video.src).toContain('/__cczj/ios-hls?u=')
  expect(hls.loadRuntime).not.toHaveBeenCalled()
})

it('shares an iOS export using the validated Go file endpoint and reports rejection', async () => {
  const { OpenFileInExplorer } = await import('../src/api/app')
  const { revealDiagnosticExport } = await import('../src/platform/diagnosticFiles')
  vi.mocked(OpenFileInExplorer).mockResolvedValueOnce(true).mockResolvedValueOnce(false)
  await revealDiagnosticExport('/data/backup.br')
  expect(OpenFileInExplorer).toHaveBeenCalledWith('/data/backup.br')
  await expect(revealDiagnosticExport('/data/missing.br')).rejects.toThrow('mobile.shareFailed')
})
