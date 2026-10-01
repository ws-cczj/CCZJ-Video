/* eslint-disable no-console */
/**
 * hls.js v1.7.0-beta.1 统一 loader（TsCacheLoader）
 *
 * 接口完全匹配 hls.js v1.7.0-beta.1 BaseLoader/FetchLoader：
 *   - 构造器: new TsCacheLoader(config)
 *   - this.stats 必须有完整的 LoadStats 结构（hls.js 会直接读写）
 *   - load(context, config, callbacks)  入口
 *   - abort() / destroy()
 *
 * callbacks 结构（hls.js 内部 FragmentLoader 传入）:
 *   onSuccess(response, stats, context, networkDetails)
 *     response = { url: string, data: ArrayBuffer|string|object, code: number }
 *   onError(error, context, networkDetails, stats)
 *   onAbort(stats, context, networkDetails)
 *   onTimeout(stats, context, networkDetails)
 *   onProgress(stats, context, data, networkDetails) ← 【可选】，不调用就不会崩
 *
 * context.type 取值:
 *   "manifest" | "level" | "audioTrack" | "subtitleTrack" | "media-fragment" | "key" | ...
 *
 * 策略：
 *   - "media-fragment" (TS 片段) → LRU 缓存 + fetch 后缓存
 *   - "manifest"/"level" (m3u8) → 文本缓存 + 正常 fetch
 *   - 其他类型 → 正常 fetch（交给浏览器/hls.js 默认逻辑）
 */
import { stripAdFromM3u8Text } from './ad-filter'
import { LOG_PREFIX } from './constants'
import { diskSave } from './disk-cache'
import { getM3u8FromCache, setM3u8Cache } from './m3u8'
import { cacheGet, cacheSet } from './memory-cache'
import { updateNetworkDiagnosis } from './network'
import { hedgeFetch } from './prefetch'
import { byteRangeOf, rangeLength } from './segment-key'
import { fireListeners, netDiag, telemetry } from './store'
import { cacheHitShapingMs, recordFetchDuration } from './telemetry'

export class TsCacheLoader {
  private _abort: AbortController | null = null
  private _destroyed = false
  private _hedgeAbort: AbortController | null = null  // ⭐ v3: 对冲请求的 abort 控制器

  // hls.js v1.7.0-beta.1 的 LoadStats 完整结构（必须在构造器中初始化）
  public stats = {
    aborted: false,
    loaded: 0,
    retry: 0,
    total: 0,
    chunkCount: 0,
    bwEstimate: 0,
    loading: { start: 0, first: 0, end: 0 },
    parsing: { start: 0, end: 0 },
    buffering: { start: 0, first: 0, end: 0 },
  }

  constructor(_config: any) {
    // _config 是 hls.js 全局 config（含 fetchSetup 等），我们不需要
  }

  load(context: any, config: any, callbacks: any) {
    if (this._destroyed) return
    const url = context?.url || (context?.frag && context.frag.url) || ''
    if (!url) {
      // 无效 URL —— 异步报 error，不同步触发 hls.js 脆弱路径
      Promise.resolve().then(() => {
        if (this._destroyed || !callbacks?.onError) return
        callbacks.onError({ code: 0, text: 'empty url' }, context, null, this.stats)
      })
      return
    }

    // ⭐ 重置 stats（每次 load 前必须重置，hls.js 内部 FragLoader 也这么做）
    this.stats.aborted = false
    this.stats.loaded = 0
    this.stats.retry = 0
    this.stats.total = 0
    this.stats.chunkCount = 0
    this.stats.bwEstimate = 0
    this.stats.loading = { start: performance.now(), first: 0, end: 0 }

    // 判断是否为 TS 片段请求（按 context.type 或 URL 后缀）
    const type: string = context?.type || ''
    const isFragment = type === 'media-fragment' || /\.(ts|m4s)(\?|$)/i.test(url)
    const isPlaylist = type === 'manifest' || type === 'level' || /\.m3u8?/i.test(url)

    // === 1) TS 片段：先走 LRU 缓存 ===
    if (isFragment) {
      const range = byteRangeOf(context)
      const hit = cacheGet(url, range)
      if (hit) {
        telemetry.curEpStats.hits++
        fireListeners()
        const size = hit.size
        // 这批字节当初真下载用了多久：优先取写入时记录的时长，其次按实测速率折算
        const loadMs = hit.fetchMs > 0 ? hit.fetchMs : cacheHitShapingMs(size)
        // 异步回调 onSuccess（模拟 fetch 的 async 行为）
        Promise.resolve().then(() => {
          if (this._destroyed) return
          const now = performance.now()
          // hls.js 用 (parsing.end - loading.start) 给 ABR 采样，时长还被兜到最少 50ms。
          // 命中若按「0 ms 拿到」上报，1 MB 就会被当成 ~160 Mbps：命中越多估计值越高，
          // 码率一路钉在最高档，换个集就开始卡。所以把起点回拨成真实下载用时 —— 数据
          // 照样即时返回，只是这次采样和「真的去下一遍」基本等价，对码率决策保持中性。
          const start = now - loadMs
          this.stats.loading = { start, first: start + 1, end: now }
          this.stats.loaded = this.stats.total = size
          this.stats.chunkCount = 1
          if (callbacks?.onSuccess) {
            callbacks.onSuccess(
              { url, data: hit.buffer.slice(0), code: 200 },
              this.stats,
              context,
              null
            )
          }
        })
        return
      }

      // 未命中 → 用原生 fetch 拉数据（不走拦截器，避免双重计数）
      telemetry.curEpStats.misses++
      fireListeners()
      const ctrl = new AbortController()
      this._abort = ctrl
      this._hedgeAbort = null
      const t0 = this.stats.loading.start

      // BYTERANGE 片段必须按区间取：整个文件当成其中一段返回会直接解出花屏。
      // 预取路径没有区间概念，所以带区间的片段不参与对冲。
      const init: RequestInit = { signal: ctrl.signal }
      if (range) init.headers = { Range: `bytes=${range}` }
      const doFetch = () => fetch(url, init)

      // ⭐ v3: 根据网络诊断决定使用对冲请求还是普通请求
      const useHedge = !range && netDiag.mode === 'server_congested' && netDiag.hedgeInFlight.size < 2
      const fetchPromise = useHedge
        ? hedgeFetch(url).then((buf) => buf ? { buf, hedged: true as const, resp: null as Response | null } : Promise.reject(new Error('hedge timeout')))
        : doFetch().then(async (resp) => {
          if (this._destroyed || ctrl.signal.aborted) throw new Error('aborted')
          if (!resp.ok) throw new Error('HTTP ' + resp.status)
          let buf = await resp.arrayBuffer()
          if (range) {
            const want = rangeLength(range)
            if (want > 0 && buf.byteLength !== want) {
              // 上游忽略了 Range 头：整份文件当一片交给 hls.js 只会解出花屏，
              // 能自己截就截，截不出来说明响应本身就不完整。
              const from = Number(range.slice(0, range.indexOf('-')))
              if (buf.byteLength >= from + want) {
                console.warn(`${LOG_PREFIX} ⚠ 未按 Range bytes=${range} 返回（收到 ${buf.byteLength} 字节），已本地截取`)
                buf = buf.slice(from, from + want)
              } else {
                console.warn(`${LOG_PREFIX} ⚠ Range bytes=${range} 只拿到 ${buf.byteLength} 字节（期望 ${want}）`)
              }
            }
          }
          return { buf, hedged: false as const, resp: resp as Response | null }
        })

      fetchPromise
        .then(({ buf, hedged, resp }) => {
          if (this._destroyed) return
          const elapsed = performance.now() - t0
          // 写入缓存
          cacheSet(url, buf, undefined, range, elapsed)
          diskSave(url, buf, undefined, range).catch(() => { })
          recordFetchDuration(elapsed, buf.byteLength)
          updateNetworkDiagnosis()
          fireListeners()
          // 统计
          const now = performance.now()
          this.stats.loading.first = Math.max(t0 + 1, t0)
          this.stats.loading.end = Math.max(this.stats.loading.first, now)
          this.stats.loaded = this.stats.total = buf.byteLength
          this.stats.bwEstimate = Math.round((buf.byteLength * 8 * 1000) / Math.max(1, now - t0))
          this.stats.chunkCount = 1
          if (hedged) console.log(`${LOG_PREFIX} ⚡ 对冲命中: ${url.slice(-60)} (${Math.round(elapsed)}ms)`)
          if (callbacks?.onSuccess) {
            callbacks.onSuccess({ url, data: buf, code: 200 }, this.stats, context, resp || null)
          }
        })
        .catch((_err) => {
          if (this._destroyed || ctrl.signal.aborted) return
          if (callbacks?.onError) {
            callbacks.onError({ code: 0, text: 'fetch failed' }, context, null, this.stats)
          }
        })
      return
    }

    // === 2) m3u8 播放列表：走文本缓存（避免重复请求同一个 m3u8）===
    if (isPlaylist) {
      const cachedText = getM3u8FromCache(url)
      if (cachedText) {
        // ⭐ 剔除广告后返回给 hls.js
        const cleanText = stripAdFromM3u8Text(cachedText, url)
        // 异步回传
        Promise.resolve().then(() => {
          if (this._destroyed) return
          const now = performance.now()
          this.stats.loading.first = Math.max(now, this.stats.loading.start)
          this.stats.loading.end = Math.max(this.stats.loading.first, now)
          this.stats.loaded = this.stats.total = cleanText.length
          this.stats.chunkCount = 1
          if (callbacks?.onSuccess) {
            callbacks.onSuccess(
              { url, data: cleanText, code: 200 },
              this.stats,
              context,
              null
            )
          }
        })
        return
      }

      // 未命中 → 原生 fetch 并缓存文本（缓存原始文本，剔除广告后返回给 hls.js）
      const ctrl = new AbortController()
      this._abort = ctrl
      const doFetch2 = () => fetch(url, { signal: ctrl.signal })
      doFetch2()
        .then(async (resp) => {
          if (this._destroyed || ctrl.signal.aborted) return
          if (!resp.ok) throw new Error('HTTP ' + resp.status)
          const rawText = await resp.text()
          if (this._destroyed) return
          setM3u8Cache(url, rawText) // 缓存原始文本
          const cleanText = stripAdFromM3u8Text(rawText, url) // ⭐ 剔除广告
          const now = performance.now()
          this.stats.loading.first = Math.max(this.stats.loading.start + 1, this.stats.loading.start)
          this.stats.loading.end = Math.max(this.stats.loading.first, now)
          this.stats.loaded = this.stats.total = cleanText.length
          this.stats.bwEstimate = Math.round((cleanText.length * 8 * 1000) / Math.max(1, now - this.stats.loading.start))
          this.stats.chunkCount = 1
          if (callbacks?.onSuccess) {
            callbacks.onSuccess({ url, data: cleanText, code: 200 }, this.stats, context, resp)
          }
        })
        .catch((_err) => {
          if (this._destroyed || ctrl.signal.aborted) return
          if (callbacks?.onError) {
            callbacks.onError({ code: 0, text: 'fetch failed' }, context, null, this.stats)
          }
        })
      return
    }

    // === 3) 其他请求（key、证书等）：直接走原生 fetch ===
    const ctrl = new AbortController()
    this._abort = ctrl
    const doFetch3 = () => fetch(url, { signal: ctrl.signal })
    doFetch3()
      .then(async (resp) => {
        if (this._destroyed || ctrl.signal.aborted) return
        if (!resp.ok) throw new Error('HTTP ' + resp.status)
        let data: any
        if (context.responseType === 'arraybuffer') data = await resp.arrayBuffer()
        else if (context.responseType === 'json') data = await resp.json()
        else data = await resp.text()
        if (this._destroyed) return
        const now = performance.now()
        this.stats.loading.first = Math.max(this.stats.loading.start + 1, this.stats.loading.start)
        this.stats.loading.end = Math.max(this.stats.loading.first, now)
        const size = (typeof data === 'string') ? data.length : (data?.byteLength || 0)
        this.stats.loaded = this.stats.total = size
        this.stats.bwEstimate = Math.round((size * 8 * 1000) / Math.max(1, now - this.stats.loading.start))
        this.stats.chunkCount = 1
        if (callbacks?.onSuccess) {
          callbacks.onSuccess({ url, data, code: 200 }, this.stats, context, resp)
        }
      })
      .catch((_err) => {
        if (this._destroyed || ctrl.signal.aborted) return
        if (callbacks?.onError) {
          callbacks.onError({ code: 0, text: 'fetch failed' }, context, null, this.stats)
        }
      })
  }

  abort() {
    try { this._abort?.abort() } catch { }
    try { this._hedgeAbort?.abort() } catch { }
  }

  destroy() {
    this._destroyed = true
    try { this._abort?.abort() } catch { }
    try { this._hedgeAbort?.abort() } catch { }
  }
}
