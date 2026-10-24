import type { HlsConfig, Loader, LoaderContext } from 'hls.js'
import { stripAdFromM3u8Text } from './ad-filter'
import { diskLoad, diskSave } from './disk-cache'
import { getM3u8FromCache, setM3u8Cache } from './m3u8'
import { cacheGet, cacheSet } from './memory-cache'
import { updateNetworkDiagnosis, recordNetworkFailure } from './network'
import { beginForegroundFetch } from './prefetch'
import { byteRangeOf, rangeLength } from './segment-key'
import { fireListeners, telemetry, prefetchState } from './store'
import { cacheHitShapingMs, recordFetchDuration } from './telemetry'

const MAX_PLAYLIST_BYTES = 8 << 20
const MAX_FRAGMENT_CAPTURE_BYTES = 32 << 20

/** Cache/playlist adaptation surrounds HLS's streaming FetchLoader. The SDK
 * owns deadlines, progress, retries and transport cancellation. Completed stats
 * survive destroy(): ABR samples them after buffering, rather than at download.
 */
export class TsCacheLoader {
  public context: LoaderContext | null = null
  public stats = {
    aborted: false, loaded: 0, retry: 0, total: 0, chunkCount: 0, bwEstimate: 0,
    loading: { start: 0, first: 0, end: 0 },
    parsing: { start: 0, end: 0 }, buffering: { start: 0, first: 0, end: 0 },
  }
  private network: Loader<LoaderContext> | null = null
  private generation = 0
  private destroyed = false
  private pending = false
  private onAbort: (() => void) | null = null
  private release: (() => void) | null = null

  constructor(private readonly hlsConfig: Partial<HlsConfig>) {}

  load(context: any, config: any, callbacks: any): void {
    if (this.destroyed) return
    this.abort()
    this.network?.destroy()
    this.network = null
    const generation = ++this.generation
    const cacheGeneration = prefetchState.cacheSession
    const current = () => !this.destroyed && this.pending && generation === this.generation
    this.context = context
    this.pending = true
    this.onAbort = () => callbacks.onAbort?.(this.stats, context, null)
    Object.assign(this.stats, { aborted: false, loaded: 0, total: 0, chunkCount: 0, bwEstimate: 0 })
    this.stats.loading = { start: performance.now(), first: 0, end: 0 }
    const url = context.url || context.frag?.url || ''
    const fragment = context.type === 'media-fragment' || /\.(ts|m4s|aac)(\?|$)/i.test(url)
    const playlist = ['manifest', 'level', 'audioTrack', 'subtitleTrack'].includes(context.type) || /\.m3u8?(\?|$)/i.test(url)
    const range = fragment ? byteRangeOf(context) : null
    const finish = () => {
      this.pending = false
      this.onAbort = null
      this.release?.(); this.release = null
    }
    const fail = (error: { code: number; text: string }, details: unknown = null) => {
      if (!current()) return
      finish()
      this.network?.destroy()
      if (fragment && cacheGeneration === prefetchState.cacheSession) recordNetworkFailure()
      callbacks.onError?.(error, context, details, this.stats)
    }
    const cached = (buffer: ArrayBuffer | string) => {
      Promise.resolve().then(() => {
        if (!current()) return
        const size = typeof buffer === 'string' ? new TextEncoder().encode(buffer).length : buffer.byteLength
        const now = performance.now()
        // Hits describe cache speed, not physical network capacity.
        const ms = fragment ? cacheHitShapingMs(size) || size * 8000 / (this.hlsConfig.abrEwmaDefaultEstimate || 500_000) : 1
        this.stats.loading = { start: now - ms, first: now - ms, end: now }
        this.stats.loaded = this.stats.total = size
        finish()
        callbacks.onSuccess?.({ url, data: typeof buffer === 'string' ? buffer : buffer.slice(0), code: 200 }, this.stats, context, null)
      })
    }
    const fetchNetwork = async () => {
      try {
        if (fragment) { telemetry.curEpStats.misses++; this.release = beginForegroundFetch(); fireListeners() }
        const runtime = await import('hls.js')
        if (!current()) return
        const native = new runtime.FetchLoader({ ...runtime.default.DefaultConfig, ...this.hlsConfig })
        this.network = native
        native.stats = this.stats
        this.stats.loading.start = 0
        const chunks: Uint8Array[] = []
        let captured = 0, capture = true
        let captureRetry = this.stats.retry
        const forward = fragment && !range && !!callbacks.onProgress
        const streaming = playlist || forward || !!range
        const nativeContext = playlist ? { ...context, responseType: 'arraybuffer' } : context
        const policy = config.loadPolicy || runtime.default.DefaultConfig[fragment ? 'fragLoadPolicy' : playlist ? 'manifestLoadPolicy' : 'keyLoadPolicy'].default
        const nativeConfig = { ...config, loadPolicy: policy, highWaterMark: streaming ? Math.min(config.highWaterMark || (64 << 10), 64 << 10) : Infinity }
        native.load(nativeContext, nativeConfig, {
          onProgress: (stats, _context, data, details) => {
            if (!current() || !(data instanceof ArrayBuffer)) return
            if (stats.retry !== captureRetry) { chunks.length = 0; captured = 0; capture = true; captureRetry = stats.retry }
            if (capture) {
              captured += data.byteLength
              if (captured > (playlist ? MAX_PLAYLIST_BYTES : MAX_FRAGMENT_CAPTURE_BYTES)) {
                chunks.length = 0; capture = false
                if (playlist || range) { fail({ code: 0, text: 'response exceeds bounded playlist/range size' }, details); return }
              } else chunks.push(new Uint8Array(data))
            }
            if (playlist && chunks.length === 1) {
              const prefix = new TextDecoder().decode(chunks[0]).replace(/^\uFEFF/, '').trimStart()
              if (prefix && !prefix.startsWith('#EXTM3U') && !'#EXTM3U'.startsWith(prefix)) {
                fail({ code: 0, text: 'not an HLS playlist' }, details); return
              }
            }
            if (forward) callbacks.onProgress(stats, context, data, details)
          },
          onSuccess: (response, stats, _context, details) => {
            if (!current()) return
            let buffer = response.data instanceof ArrayBuffer ? response.data : null
            if (chunks.length) {
              buffer = new ArrayBuffer(captured)
              const view = new Uint8Array(buffer)
              let offset = 0
              for (const chunk of chunks) { view.set(chunk, offset); offset += chunk.byteLength }
              chunks.length = 0
            }
            if (playlist) {
              const raw = buffer ? new TextDecoder().decode(buffer) : ''
              if (!raw.replace(/^\uFEFF/, '').trimStart().startsWith('#EXTM3U')) { fail({ code: 0, text: 'not an HLS playlist' }, details); return }
              setM3u8Cache(url, raw)
              finish()
              callbacks.onSuccess?.({ ...response, data: stripAdFromM3u8Text(raw, url) }, stats, context, details)
              return
            }
            if (range && buffer) {
              const want = rangeLength(range), start = Number(range.split('-')[0])
              if (response.code === 200 && buffer.byteLength >= start + want) buffer = buffer.slice(start, start + want)
              if (buffer.byteLength !== want) { fail({ code: response.code || 0, text: 'incomplete byte-range response' }, details); return }
            }
            if (fragment && cacheGeneration === prefetchState.cacheSession) {
              const ms = Math.max(1, stats.loading.end - stats.loading.start)
              recordFetchDuration(ms, stats.loaded, context.frag?.duration)
              updateNetworkDiagnosis()
              if (buffer && buffer.byteLength && capture) {
                cacheSet(url, buffer, undefined, range, ms)
                void diskSave(url, buffer, undefined, range)
              }
              fireListeners()
            }
            finish()
            // Already streamed bytes must not be appended a second time.
            callbacks.onSuccess?.({ ...response, data: forward ? new ArrayBuffer(0) : buffer || response.data }, stats, context, details)
          },
          onError: (error, _context, details) => fail(error, details),
          onTimeout: (stats, _context, details) => {
            if (!current()) return
            finish()
            if (fragment && cacheGeneration === prefetchState.cacheSession) recordNetworkFailure()
            callbacks.onTimeout?.(stats, context, details)
          },
        })
      } catch (error) { fail({ code: 0, text: error instanceof Error ? error.message : 'media request failed' }) }
    }
    if (!url) { queueMicrotask(() => fail({ code: 0, text: 'empty url' })); return }
    if (fragment) {
      const hit = cacheGet(url, range)
      if (hit) { telemetry.curEpStats.hits++; cached(hit.buffer); fireListeners(); return }
      void diskLoad(url, range).then(buffer => {
        if (!current()) return
        if (buffer) { telemetry.curEpStats.hits++; cacheSet(url, buffer, undefined, range); cached(buffer); fireListeners() }
        else void fetchNetwork()
      })
    } else if (playlist) {
      const text = getM3u8FromCache(url)
      if (text) cached(stripAdFromM3u8Text(text, url))
      else void fetchNetwork()
    } else void fetchNetwork()
  }

  getCacheAge(): number | null { return this.network?.getCacheAge?.() ?? null }
  getResponseHeader(name: string): string | null { return this.network?.getResponseHeader?.(name) ?? null }
  abort(): void {
    const notify = this.onAbort
    this.onAbort = null
    this.generation++
    if (this.pending) this.stats.aborted = true
    this.pending = false
    this.network?.abort()
    this.release?.(); this.release = null
    notify?.()
  }
  destroy(): void {
    this.onAbort = null
    this.abort()
    this.destroyed = true
    this.network?.destroy(); this.network = null
    this.context = null
  }
}
