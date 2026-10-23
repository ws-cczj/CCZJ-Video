import { TsCache } from '../../utils/tsCache'
import { extractBitrateFromUrl, type StreamVariantInfo } from '../useVideoInfo'
import { createPlaybackSession } from '../playbackSession'
import { proxyHlsURL } from './proxy'
import { useHlsEngine } from './useHlsEngine'

type Lease = ReturnType<ReturnType<typeof createPlaybackSession>['capture']>
type SourceError = 'unsupported' | 'load_failed' | 'fatal' | 'timeout'

/** Transport owns HLS/cache integration and recovery. The view receives stream
 * metadata and error outcomes; it never needs HLS event names or loader config. */
export function createPlaybackSource(deps: {
  streams: (streams: StreamVariantInfo[]) => void
  error: (code: SourceError, detail?: string) => void
  fatal: (detail: string) => void
  recovering?: (detail: string) => void
  unrecoverable: () => void
  bufferChanged: () => void
}) {
  const engine = useHlsEngine()
  let networkTimer: ReturnType<typeof setTimeout> | null = null

  function cancelNetworkTimeout(): void {
    if (networkTimer !== null) clearTimeout(networkTimer)
    networkTimer = null
  }

  function dispose(video?: HTMLVideoElement): void {
    cancelNetworkTimeout()
    TsCache.setAbrSwitchCallback(null)
    engine.dispose(video)
  }

  async function load(video: HTMLVideoElement, url: string, lease: Lease): Promise<boolean> {
    const playbackURL = proxyHlsURL(url)
    TsCache.enable()
    const Hls = await engine.loadRuntime()
    if (!lease.current()) return false
    if (!Hls.isSupported()) {
      if (video.canPlayType('application/vnd.apple.mpegurl')) {
        video.src = playbackURL
        return true
      }
      deps.error('unsupported')
      return false
    }

    let parsed: { urls: string[]; targetduration: number; isMaster: boolean; streamInfo?: StreamVariantInfo[] }
    try { parsed = await TsCache.fetchAndParseM3u8(playbackURL, lease.signal) }
    catch { parsed = { urls: [], targetduration: 6, isMaster: false } }
    if (!lease.current()) return false
    let streams = parsed.streamInfo ?? []
    if (!parsed.isMaster && parsed.urls.length && /\.(ts|aac|mp4|m4s)(\?|$)/i.test(parsed.urls[0])) {
      TsCache.setSegments(parsed.urls)
      TsCache.setTargetDuration(parsed.targetduration)
      if (!streams.length) {
        const bandwidth = extractBitrateFromUrl(parsed.urls[0])
        if (bandwidth) streams = [{ bandwidth, resolution: '', codecs: '', url: parsed.urls[0] }]
      }
    }
    deps.streams(streams)

    const hls = engine.create(video, {
      enableWorker: false, lowLatencyMode: false,
      maxBufferLength: 30, maxMaxBufferLength: 60, backBufferLength: 30,
      maxBufferSize: 60 * 1000 * 1000,
      fragLoadingTimeOut: 15000, fragLoadingMaxRetry: 8, fragLoadingRetryDelay: 500,
      manifestLoadingTimeOut: 10000, manifestLoadingMaxRetry: 3, manifestLoadingRetryDelay: 700,
      loader: TsCache.TsCacheLoader,
    }, Hls)

    TsCache.setAbrSwitchCallback(lease.guard((target: number) => {
      if (!hls.levels || hls.levels.length <= 1) return
      const current = hls.currentLevel >= 0 ? hls.currentLevel : hls.nextAutoLevel
      const next = target === -1 ? Math.max(0, current - 1) : target
      if (next >= 0 && next < hls.levels.length) hls.nextAutoLevel = next
    }))
    hls.on(Hls.Events.LEVEL_LOADED, lease.guard((_event: unknown, data: any) => {
      const fragments = data?.details?.fragments ?? []
      if (fragments.length) {
        TsCache.setSegments(fragments.map((fragment: any) => absolute(fragment.url, playbackURL)))
        TsCache.setTargetDuration(data.details.targetduration || parsed.targetduration || 6)
        deps.bufferChanged()
      }
    }))
    hls.on(Hls.Events.FRAG_CHANGED, lease.guard((_event: unknown, data: any) => {
      if (data?.frag?.url) TsCache.notifyCurrentTs(absolute(data.frag.url, playbackURL))
    }))
    hls.on(Hls.Events.ERROR, lease.guard((_event: unknown, data: any) => {
      if (!data?.fatal) return
      const detail = String(data.details || data.type || '')
      if (['manifestLoadError', 'manifestParsingError', 'manifestIncompatibleCodecsError', 'manifestLoadTimeOut'].includes(detail)) {
        deps.recovering?.(detail)
        dispose(video)
        video.src = playbackURL
        video.onerror = lease.guard(() => deps.error('load_failed'))
        return
      }
      if (data.type === Hls.ErrorTypes.NETWORK_ERROR) {
        deps.recovering?.(detail)
        try { hls.startLoad() } catch (error) { console.warn('[Player] Network recovery failed:', error) }
        if (!video.paused && !video.ended && networkTimer === null) {
          networkTimer = setTimeout(lease.guard(() => {
            networkTimer = null
            if (!video.paused && !video.ended) deps.error('timeout')
          }), 10000)
        }
      } else if (data.type === Hls.ErrorTypes.MEDIA_ERROR) {
        deps.recovering?.(detail)
        try { hls.recoverMediaError() } catch (error) { console.warn('[Player] Media recovery failed:', error) }
      } else {
        deps.fatal(detail)
        deps.error('fatal', detail)
        deps.unrecoverable()
      }
    }))
    hls.loadSource(playbackURL)
    hls.attachMedia(video)
    return true
  }

  return { load, dispose, cancelNetworkTimeout, snapshot: engine.snapshot }
}

function absolute(url: string, base: string): string {
  try { return new URL(url, base).href } catch { return url }
}
