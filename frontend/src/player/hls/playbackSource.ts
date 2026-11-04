import { TsCache } from '../../utils/tsCache'
import { extractBitrateFromUrl, type StreamVariantInfo } from '../useVideoInfo'
import { createPlaybackSession } from '../playbackSession'
import { proxyHlsURL } from './proxy'
import { useHlsEngine } from './useHlsEngine'
import { adaptiveForwardBuffer } from '../../utils/ts-cache/network'
import { playbackTransportURL } from '../../platform/mediaTransport'
import { isIOSRuntime } from '../../api/runtime'
import { hlsWorkerConfig } from './worker'
import type { BlockedSegment } from '../../utils/ts-cache/prefetch'
import { byteRangeOf } from '../../utils/ts-cache/segment-key'
import { confirmSegmentDecoded, rejectSegmentContent, transferFailure } from '../../utils/ts-cache/transfer'
import { clearM3u8TextCache } from '../../utils/ts-cache/m3u8'

type Lease = ReturnType<ReturnType<typeof createPlaybackSession>['capture']>
type SourceError = 'unsupported' | 'load_failed' | 'fatal' | 'timeout'

/** Transport owns HLS/cache integration and recovery. The view receives stream
 * metadata and error outcomes; it never needs HLS event names or loader config. */
export function createPlaybackSource(deps: {
  streams: (streams: StreamVariantInfo[]) => void
  error: (code: SourceError, detail?: string) => void
  fatal: (detail: string) => void
  recovering?: (detail: string) => void
  recovered?: () => void
  resume?: () => void
  unrecoverable: () => void
  bufferChanged: () => void
  networkPolicy?: (detail: Record<string, unknown>) => void
  networkIssue?: (detail: Record<string, unknown>) => void
  segmentIssue?: (issue: BlockedSegment | null) => void
}) {
  const engine = useHlsEngine()
  const transportOwner = Symbol('hls-playback')
  let networkTimer: ReturnType<typeof setTimeout> | null = null
  let releaseRecovery: (() => void) | undefined
  let resumeRecovery: (() => boolean) | undefined
  let suspended = false
  let retrySegment: (() => void) | undefined
  let skipSegment: (() => boolean) | undefined

  function cancelNetworkTimeout(): void {
    if (networkTimer !== null) clearTimeout(networkTimer)
    networkTimer = null
  }

  function dispose(video?: HTMLVideoElement): void {
    suspended = false
    releaseRecovery?.(); releaseRecovery = undefined
    resumeRecovery = undefined
    retrySegment = undefined; skipSegment = undefined; deps.segmentIssue?.(null)
    TsCache.setTransportActive(false, transportOwner)
    cancelNetworkTimeout()
    TsCache.setAbrSwitchCallback(null)
    engine.dispose(video)
  }

  async function load(video: HTMLVideoElement, url: string, lease: Lease): Promise<boolean> {
    TsCache.setTransportActive(false, transportOwner)
    const playbackURL = await playbackTransportURL(proxyHlsURL(url), lease.signal)
    if (!lease.current()) return false
    // Native HLS keeps iPhone playback and PiP in AVPlayer. MSE/Hls.js is still
    // available for hosts without native HLS support.
    if (isIOSRuntime() && video.canPlayType('application/vnd.apple.mpegurl')) {
      video.src = playbackURL
      return true
    }
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

    const fragPolicy = Hls.DefaultConfig?.fragLoadPolicy?.default
    const BaseGapController = Hls.DefaultConfig.gapController
    // Gap recovery must not silently seek over a required fragment while its
    // transport is retrying. Normal timestamp-hole recovery remains SDK-owned.
    const gapController = BaseGapController ? class extends BaseGapController {
      poll(currentTime: number, lastCurrentTime: number) {
        if (!TsCache.hasPlaybackGap()) super.poll(currentTime, lastCurrentTime)
      }
    } : undefined
    const forward = adaptiveForwardBuffer()
    const hls = engine.create(video, {
      autoStartLoad: !suspended,
      ...hlsWorkerConfig, lowLatencyMode: false,
      maxBufferLength: forward, maxMaxBufferLength: forward, backBufferLength: 30,
      ...(gapController ? { gapController } : {}),
      maxBufferSize: 60 * 1000 * 1000,
      ...(fragPolicy ? { fragLoadPolicy: { default: { ...fragPolicy,
        maxTimeToFirstByteMs: 10000, maxLoadTimeMs: TsCache.adaptiveFragmentTimeout(),
        timeoutRetry: { maxNumRetry: 0, retryDelayMs: 1000, maxRetryDelayMs: 4000 },
        errorRetry: { maxNumRetry: 0, retryDelayMs: 1000, maxRetryDelayMs: 4000 },
      } } } : {}),
      loader: TsCache.TsCacheLoader,
    }, Hls)

    let policySignature = ''
    let networkRecoveries = 0, mediaRecoveries = 0, recoveryPending = false, exhausted = false
    let managedFragment = false
    let retryTarget: { url: string; range: string | null } | undefined
    let fragments: any[] = []
    let authRefreshAt = -Infinity, authRefreshes = 0
    let loadedKey: { uri: string; method: string } | undefined
    const finishRecovery = () => {
      if (retryTarget && transferFailure(retryTarget.url, retryTarget.range)) return
      cancelNetworkTimeout()
      networkRecoveries = 0; mediaRecoveries = 0; recoveryPending = false; exhausted = false
      retryTarget = undefined; managedFragment = false
      deps.recovered?.()
    }
    const recoverNetwork = () => {
      if (!lease.current() || suspended || !recoveryPending || exhausted || (video.paused && video.readyState >= 2) || video.ended) return
      if (networkTimer !== null) return
      if (managedFragment) {
        deps.recovering?.('fragment_retry')
        networkTimer = setTimeout(lease.guard(() => {
          networkTimer = null
          if ((video.paused && video.readyState >= 2) || suspended || !recoveryPending) return
          try { hls.startLoad(video.currentTime) } catch { deps.error('load_failed') }
        }), TsCache.segmentRetryDelay(retryTarget?.url, retryTarget?.range))
        return
      }
      if (++networkRecoveries > 2) { exhausted = true; hls.stopLoad(); deps.error('timeout'); return }
      deps.recovering?.('network')
      try { hls.startLoad(video.currentTime) } catch { exhausted = true; deps.error('load_failed'); return }
      networkTimer = setTimeout(lease.guard(() => {
        networkTimer = null
        if (!recoveryPending || video.paused || video.ended) return
        if (navigator.onLine === false) { exhausted = true; hls.stopLoad(); deps.error('timeout'); return }
        recoverNetwork()
      }), 8000)
    }
    const online = () => { if (recoveryPending && !exhausted) recoverNetwork() }
    resumeRecovery = () => {if(!recoveryPending)return false;if(!exhausted)recoverNetwork();return true}
    const pause = () => cancelNetworkTimeout()
    const seek = () => {cancelNetworkTimeout();recoveryPending=false;exhausted=false;networkRecoveries=0;retryTarget=undefined;deps.recovered?.()}
    const clearSegmentIssue = () => deps.segmentIssue?.(null)
    const activateTransport = () => { if (!suspended && lease.current()) TsCache.setTransportActive(true, transportOwner) }
    const finishTransport = () => { if (lease.current()) TsCache.setTransportActive(false, transportOwner) }
    let offCache: () => void = () => {}
    window.addEventListener('online', online)
    video.addEventListener('pause', pause)
    video.addEventListener('seeking', seek)
    const releaseOwned = () => {
      window.removeEventListener('online', online)
      video.removeEventListener('pause', pause)
      video.removeEventListener('seeking', seek)
      video.removeEventListener('playing', finishRecovery)
      video.removeEventListener('waiting', refreshIssue)
      video.removeEventListener('playing', refreshIssue)
      video.removeEventListener('playing', activateTransport)
      video.removeEventListener('ended', finishTransport)
      video.removeEventListener('seeking', clearSegmentIssue)
      lease.signal.removeEventListener('abort', onOwnedAbort)
      offCache(); deps.segmentIssue?.(null)
    }
    const onOwnedAbort = () => {
      if (releaseRecovery === releaseOwned) { releaseRecovery = undefined; cancelNetworkTimeout() }
      releaseOwned()
    }
    releaseRecovery = releaseOwned
    lease.signal.addEventListener('abort', onOwnedAbort, {once:true})
    const applyNetworkPolicy = () => {
      if (!hls.config) return
      const stats = TsCache.stats()
      const forward = adaptiveForwardBuffer()
      hls.config.maxMaxBufferLength = forward
      hls.config.maxBufferLength = forward
      if (hls.config.fragLoadPolicy?.default) hls.config.fragLoadPolicy.default.maxLoadTimeMs = stats.fragmentTimeout
      const signature = `${stats.networkMode}:${hls.currentLevel}:${forward}:${stats.fragmentTimeout}:${stats.concurrency}`
      if (signature === policySignature) return
      policySignature = signature
      deps.networkPolicy?.({ network_mode: stats.networkMode, bandwidth_bps: Math.round(hls.bandwidthEstimate || 0),
        measured_bps: stats.measuredBps, download_ratio: stats.downloadRatio, buffer_ahead_seconds: stats.playbackBufferAhead,
        level: hls.currentLevel, levels: hls.levels?.length || 0, forward_seconds: forward,
        background_connections: stats.concurrency, fragment_timeout_ms: stats.fragmentTimeout })
    }
    // HLS's ABR controller owns level changes, including in-flight abandonment.
    // A second fixed-duration downswitch rule would conflict with its samples.
    video.addEventListener('playing', finishRecovery, {signal:lease.signal})
    if (Hls.Events.FRAG_BUFFERED) hls.on(Hls.Events.FRAG_BUFFERED, lease.guard((_event: unknown, data: any) => {
      if (data?.frag?.decryptdata?.key) loadedKey = { uri: data.frag.decryptdata.uri, method: data.frag.decryptdata.method }
      if (data?.frag?.url) confirmSegmentDecoded(data.frag.url, byteRangeOf({ rangeStart: data.frag.byteRangeStartOffset, rangeEnd: data.frag.byteRangeEndOffset }))
      deps.bufferChanged(); applyNetworkPolicy()
      if (recoveryPending && video.readyState >= 3) finishRecovery()
    }))
    if (Hls.Events.LEVEL_SWITCHED) hls.on(Hls.Events.LEVEL_SWITCHED, lease.guard(applyNetworkPolicy))
    const publishTimeline = () => {
      if (fragments.length) {
        TsCache.setSegments(fragments.map((fragment: any) => absolute(fragment.url, playbackURL)), undefined, fragments.map((fragment: any) => ({
          url: absolute(fragment.url, playbackURL), start: fragment.start, duration: fragment.duration,
          range: byteRangeOf({ rangeStart: fragment.byteRangeStartOffset, rangeEnd: fragment.byteRangeEndOffset }),
          demandOnly: !!fragment.gap || (!!fragment.encrypted && !!fragment.byteRange?.length),
        })))
        deps.bufferChanged()
      }
    }
    hls.on(Hls.Events.LEVEL_LOADED, lease.guard((_event: unknown, data: any) => {
      fragments = data?.details?.fragments ?? []
      TsCache.setTargetDuration(data.details.targetduration || parsed.targetduration || 6)
      publishTimeline()
    }))
    const currentIssue = () => {
      let issue = TsCache.blockedSegment()
      const next = issue && fragments.find(fragment => Math.abs(fragment.start - issue!.next) < .05)
      const keyReady = !next?.decryptdata || !!next.decryptdata.key || (loadedKey?.uri === next.decryptdata.uri && loadedKey?.method === next.decryptdata.method)
      if (issue && (!next || next.gap || (next.initSegment && !next.initSegment.data) || !keyReady)) issue = null
      if (issue && next) {
        // Select inside the next fragment, beyond the SDK's lookup tolerance,
        // so an explicit skip cannot select the failed predecessor again.
        const target = next.start + Math.min(next.duration / 2, (hls.config.maxFragLookUpTolerance || 0) + .05)
        issue = { ...issue, next: target, seconds: Math.max(0, target - video.currentTime) }
      }
      return issue
    }
    const refreshIssue = () => {
      deps.bufferChanged()
      if (!TsCache.stats().totalSegments && fragments.length) publishTimeline()
      const issue = currentIssue()
      deps.segmentIssue?.(issue)
    }
    offCache = TsCache.onStateChange(lease.guard(refreshIssue))
    video.addEventListener('waiting', refreshIssue, { signal: lease.signal })
    video.addEventListener('playing', refreshIssue, { signal: lease.signal })
    video.addEventListener('playing', activateTransport, { signal: lease.signal })
    video.addEventListener('ended', finishTransport, { signal: lease.signal })
    video.addEventListener('seeking', clearSegmentIssue, { signal: lease.signal })
    retrySegment = () => { TsCache.retryBlockedSegment(); deps.segmentIssue?.(null); cancelNetworkTimeout(); recoveryPending = true; managedFragment = true; exhausted = false; recoverNetwork() }
    skipSegment = () => {
      const issue = currentIssue()
      if (!issue || !TsCache.acceptSkippedSegment(issue.key)) return false
      deps.segmentIssue?.(null); cancelNetworkTimeout(); recoveryPending = false
      video.currentTime = issue.next
      hls.startLoad(issue.next)
      deps.networkIssue?.({ reason: 'fragment_skip_requested', skipped_seconds: issue.seconds, retry: issue.retries })
      return true
    }
    hls.on(Hls.Events.FRAG_CHANGED, lease.guard((_event: unknown, data: any) => {
      deps.bufferChanged()
      if (data?.frag?.url) TsCache.notifyCurrentTs(absolute(data.frag.url, playbackURL))
    }))
    hls.on(Hls.Events.ERROR, lease.guard((_event: unknown, data: any) => {
      if(suspended)return
      const status = Number(data.response?.code ?? data.networkDetails?.status)
      if (data.type === Hls.ErrorTypes.NETWORK_ERROR && (status === 401 || status === 403)) {
        const now = performance.now()
        if (now - authRefreshAt > 60_000) authRefreshes = 0
        if (authRefreshes < 2 && now - authRefreshAt > 5_000) {
          authRefreshes++; authRefreshAt = now
          clearM3u8TextCache()
          cancelNetworkTimeout()
          recoveryPending = true; exhausted = false
          deps.recovering?.('playlist_refresh')
          hls.stopLoad()
          hls.loadSource(playbackURL)
          hls.startLoad(video.currentTime)
          return
        }
      }
      if (data?.type === Hls.ErrorTypes.NETWORK_ERROR) {
        applyNetworkPolicy()
        deps.networkIssue?.({ reason: String(data.details || ''), fatal: !!data.fatal,
          fragment_number: data.frag?.sn, level: data.frag?.level ?? hls.currentLevel,
          fragment_seconds: data.frag?.duration, loaded_bytes: data.frag?.stats?.loaded,
          retry: data.frag?.stats?.retry, position_seconds: video.currentTime,
          buffer_ahead_seconds: TsCache.stats().playbackBufferAhead })
      }
      const detail = String(data.details || data.type || '')
      if (detail === 'fragParsingError' && data.frag?.url && data.frag.sn !== 'initSegment') {
        retryTarget = { url: data.frag.url, range: byteRangeOf({ rangeStart: data.frag.byteRangeStartOffset, rangeEnd: data.frag.byteRangeEndOffset }) }
        rejectSegmentContent(retryTarget.url, retryTarget.range)
        if (data.fatal) { managedFragment = true; recoveryPending = true; recoverNetwork() }
        return
      }
      if (!data?.fatal) return
      if (['manifestLoadError', 'manifestParsingError', 'manifestIncompatibleCodecsError', 'manifestLoadTimeOut'].includes(detail)) {
        const position=video.currentTime, rate=video.playbackRate, paused=video.paused, muted=video.muted
        deps.recovering?.(detail)
        dispose(video)
        video.addEventListener('loadedmetadata',lease.guard(()=>{
          if(position>0&&Number.isFinite(video.duration))video.currentTime=Math.min(position,Math.max(0,video.duration-1))
          video.playbackRate=rate;video.muted=muted
          if(!paused)deps.resume?.()
        }),{once:true,signal:lease.signal})
        video.src = playbackURL
        video.onerror = lease.guard(() => deps.error('load_failed'))
        return
      }
      if (data.type === Hls.ErrorTypes.NETWORK_ERROR) {
        managedFragment = !!data.frag && data.frag.sn !== 'initSegment'
        retryTarget = managedFragment ? { url: data.frag.url, range: byteRangeOf({ rangeStart: data.frag.byteRangeStartOffset, rangeEnd: data.frag.byteRangeEndOffset }) } : undefined
        recoveryPending = true
        if (video.paused && video.readyState >= 2) { if (!managedFragment) { exhausted = true; deps.error('load_failed') }; return }
        recoverNetwork()
      } else if (data.type === Hls.ErrorTypes.MEDIA_ERROR) {
        if (++mediaRecoveries > 2) { deps.error('fatal', detail); return }
        deps.recovering?.(detail)
        try { hls.recoverMediaError() } catch (error) { console.warn('[Player] Media recovery failed:', error) }
      } else {
        deps.fatal(detail)
        deps.error('fatal', detail)
        deps.unrecoverable()
      }
    }))
    TsCache.setTransportActive(!suspended, transportOwner)
    hls.loadSource(playbackURL)
    hls.attachMedia(video)
    if(suspended)engine.suspend()
    return true
  }

  function suspend(): void {
    suspended = true
    cancelNetworkTimeout()
    engine.suspend()
    TsCache.setTransportActive(false, transportOwner)
    TsCache.setPlaybackBufferAhead(0)
  }
  function resume(): boolean {
    if (suspended) { suspended = false; TsCache.setTransportActive(true, transportOwner); engine.resume(); return true }
    return resumeRecovery?.() ?? false
  }
  return { load, dispose, cancelNetworkTimeout, suspend, resume, snapshot: engine.snapshot,
    retrySegment: () => retrySegment?.(), skipSegment: () => skipSegment?.() ?? false }
}

function absolute(url: string, base: string): string {
  try { return new URL(url, base).href } catch { return url }
}
