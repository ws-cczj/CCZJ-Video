import type { HlsConfig, Loader, LoaderCallbacks, LoaderConfiguration, LoaderContext } from 'hls.js'
import { stripAdFromM3u8Text } from './ad-filter'
import { MAX_PLAYLIST_BYTES } from './constants'
import { getM3u8FromCache, setM3u8Cache } from './m3u8'
import { cacheDelete } from './memory-cache'
import { beginForegroundFetch, beginPreviewFetch } from './prefetch'
import { byteRangeOf } from './segment-key'
import { fireListeners, telemetry } from './store'
import { cacheHitShapingMs } from './telemetry'
import { acquireSegment, newLoaderStats, SegmentFailure } from './transfer'

/** HLS transport adapter. Fragments share the scheduler's transfer owner;
 * manifests and keys keep the SDK's deadline and response handling. */
export class TsCacheLoader implements Loader<LoaderContext> {
  public context: LoaderContext | null = null
  public stats = newLoaderStats()
  private network: Loader<LoaderContext> | null = null
  private generation = 0
  private destroyed = false
  private pending = false
  private onAbort: (() => void) | null = null
  private release: (() => void) | null = null
  private releaseSegment: (() => void) | null = null

  constructor(private readonly hlsConfig: Partial<HlsConfig>, private readonly purpose: 'playback' | 'preview' = 'playback') {}

  load(context: LoaderContext & { frag?: { url: string; duration: number } }, config: LoaderConfiguration, callbacks: LoaderCallbacks<LoaderContext>): void {
    if (this.destroyed) return
    this.abort()
    this.network?.destroy(); this.network = null
    const generation = ++this.generation
    const current = () => !this.destroyed && this.pending && generation === this.generation
    this.context = context; this.pending = true
    Object.assign(this.stats, newLoaderStats())
    this.stats.loading.start = performance.now()
    this.onAbort = () => callbacks.onAbort?.(this.stats, context, null)
    const url = context.url || context.frag?.url || ''
    const fragment = context.type === 'media-fragment' || /\.(ts|m4s|aac)(\?|$)/i.test(url)
    const playlist = ['manifest', 'level', 'audioTrack', 'subtitleTrack'].includes(context.type) || /\.m3u8?(\?|$)/i.test(url)
    const range = fragment ? byteRangeOf(context) : null
    const playback = this.purpose === 'playback'
    const finish = () => {
      this.pending = false; this.onAbort = null
      this.release?.(); this.release = null
      this.releaseSegment?.(); this.releaseSegment = null
    }
    const fail = (error: { code: number; text: string }, details: Parameters<LoaderCallbacks<LoaderContext>['onError']>[2] = null) => {
      if (!current()) return
      finish(); this.network?.destroy()
      callbacks.onError?.(error, context, details, this.stats)
    }
    const cached = (buffer: ArrayBuffer | string) => {
      queueMicrotask(() => {
        if (!current()) return
        let data: ArrayBuffer | string
        try {
          if (buffer instanceof ArrayBuffer && buffer.byteLength === 0) throw new Error('empty cache buffer')
          data = typeof buffer === 'string' ? buffer : buffer.slice(0)
        } catch {
          cacheDelete(url, range)
          fail({ code: 0, text: 'media cache could not be read' })
          return
        }
        const size = typeof data === 'string' ? new TextEncoder().encode(data).length : data.byteLength
        const now = performance.now()
        const ms = fragment ? cacheHitShapingMs(size) || size * 8000 / (this.hlsConfig.abrEwmaDefaultEstimate || 500_000) : 1
        this.stats.loading = { start: now - ms, first: now - ms, end: now }
        this.stats.loaded = this.stats.total = size
        finish()
        callbacks.onSuccess?.({ url, data, code: 200 }, this.stats, context, null)
      })
    }
    const fetchNetwork = async () => {
      try {
        const runtime = await import('hls.js')
        if (!current()) return
        if (!playback) {
          this.release = beginPreviewFetch(() => fail({ code: 0, text: 'preview yielded to playback' }))
          if (!this.release) { fail({ code: 0, text: 'playback has priority' }); return }
        }
        const native = new runtime.FetchLoader({ ...runtime.default.DefaultConfig, ...this.hlsConfig })
        this.network = native; native.stats = this.stats
        this.stats.loading.start = 0
        const chunks: Uint8Array[] = []
        let captured = 0
        const nativeContext = playlist ? { ...context, responseType: 'arraybuffer' } : context
        const policy = config.loadPolicy || runtime.default.DefaultConfig[playlist ? 'manifestLoadPolicy' : 'keyLoadPolicy'].default
        native.load(nativeContext, { ...config, loadPolicy: policy, highWaterMark: playlist ? 64 << 10 : Infinity }, {
          onProgress: (_stats, _context, data, details) => {
            if (!current() || !playlist || !(data instanceof ArrayBuffer)) return
            captured += data.byteLength
            if (captured > MAX_PLAYLIST_BYTES) { fail({ code: 0, text: 'playlist exceeds 16 MiB' }, details); return }
            chunks.push(new Uint8Array(data))
            if (chunks.length === 1) {
              const prefix = new TextDecoder().decode(chunks[0]).replace(/^\uFEFF/, '').trimStart()
              if (prefix && !prefix.startsWith('#EXTM3U') && !'#EXTM3U'.startsWith(prefix)) fail({ code: 0, text: 'not an HLS playlist' }, details)
            }
          },
          onSuccess: (response, stats, _context, details) => {
            if (!current()) return
            if (playlist) {
              let buffer = response.data instanceof ArrayBuffer ? response.data : new ArrayBuffer(0)
              if (chunks.length) {
                buffer = new ArrayBuffer(captured)
                const view = new Uint8Array(buffer)
                let offset = 0
                for (const chunk of chunks) { view.set(chunk, offset); offset += chunk.byteLength }
              }
              chunks.length = 0
              if (buffer.byteLength > MAX_PLAYLIST_BYTES) { fail({ code: 0, text: 'playlist exceeds 16 MiB' }, details); return }
              const raw = new TextDecoder().decode(buffer)
              if (!raw.replace(/^\uFEFF/, '').trimStart().startsWith('#EXTM3U')) { fail({ code: 0, text: 'not an HLS playlist' }, details); return }
              setM3u8Cache(url, raw, details instanceof Response ? details.headers : undefined); finish()
              callbacks.onSuccess?.({ ...response, data: stripAdFromM3u8Text(raw, url) }, stats, context, details)
            } else { finish(); callbacks.onSuccess?.(response, stats, context, details) }
          },
          onError: (error, _context, details) => fail(error, details),
          onTimeout: (stats, _context, details) => { if (!current()) return; finish(); callbacks.onTimeout?.(stats, context, details) },
        })
      } catch { fail({ code: 0, text: 'media request failed' }) }
    }
    if (!url) { queueMicrotask(() => fail({ code: 0, text: 'empty url' })); return }
    if (fragment) {
      if (!playback) {
        this.release = beginPreviewFetch(() => fail({ code: 0, text: 'preview yielded to playback' }))
        if (!this.release) { queueMicrotask(() => fail({ code: 0, text: 'playback has priority' })); return }
      } else this.release = beginForegroundFetch()
      const request = acquireSegment({ url, range, purpose: playback ? 'playback' : 'preview', duration: context.frag?.duration,
        headers: context.headers, timeoutMs: config.loadPolicy?.maxLoadTimeMs,
        progress: !range && callbacks.onProgress ? (stats, data) => {
          if (!current()) return
          this.stats.loading = { ...stats.loading }; this.stats.loaded = stats.loaded; this.stats.total = stats.total; this.stats.chunkCount = stats.chunkCount; this.stats.retry = stats.retry
          callbacks.onProgress?.(this.stats, context, data, null)
        } : undefined })
      if (!current()) { request.release(); return }
      this.releaseSegment = request.release
      void request.promise.then(result => {
        if (!current()) return
        if (result.cached) {
          if (playback) telemetry.curEpStats.hits++
          cached(result.buffer)
        } else {
          if (playback) telemetry.curEpStats.misses++
          this.stats.loading = { ...result.stats.loading }; this.stats.loaded = result.stats.loaded; this.stats.total = result.stats.total
          this.stats.chunkCount = result.stats.chunkCount; this.stats.retry = result.stats.retry
          const data = result.streamed ? new ArrayBuffer(0) : result.buffer.slice(0)
          finish()
          callbacks.onSuccess?.({ url, data, code: 200 }, this.stats, context, null)
        }
        fireListeners()
      }).catch(error => {
        if (!current()) return
        if (error?.name === 'AbortError') { this.abort(); return }
        if (playback) telemetry.curEpStats.misses++
        fail({ code: error instanceof SegmentFailure ? error.code : 0, text: 'media segment request failed' })
        fireListeners()
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
    this.onAbort = null; this.generation++
    if (this.pending) this.stats.aborted = true
    this.pending = false; this.network?.abort()
    this.release?.(); this.release = null
    this.releaseSegment?.(); this.releaseSegment = null
    notify?.()
  }
  destroy(): void {
    this.onAbort = null; this.abort(); this.destroyed = true
    this.network?.destroy(); this.network = null; this.context = null
  }
}
export class TsCachePreviewLoader extends TsCacheLoader {
  constructor(config: Partial<HlsConfig>) { super(config, 'preview') }
}
