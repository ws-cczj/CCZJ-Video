import { SPEED_SAMPLE_COUNT } from './constants'
import { cacheHas } from './memory-cache'
import { fireListeners, session, telemetry, netDiag } from './store'

/**
 * 命中率计数 + 最近若干次真实下载的速率样本。
 *
 * 读取计数按集分开，当前 loader 在明确读取结果后记账；真实传输样本
 * 来自共享传输所有者，预览不进入主播放样本。
 */

export function recordFetchDuration(ms: number, bytes: number, mediaSeconds = session.targetDuration, started = performance.now() - ms): void {
  if (!(ms > 0) || !(bytes > 0)) return
  telemetry.recentFetchDurations.push(ms)
  telemetry.recentFetchBytes.push(bytes)
  telemetry.recentMediaSeconds.push(mediaSeconds > 0 ? mediaSeconds : session.targetDuration)
  telemetry.recentFetchStarts.push(started)
  telemetry.recentFetchEnds.push(started + ms)
  if (telemetry.recentFetchDurations.length > SPEED_SAMPLE_COUNT) {
    telemetry.recentFetchDurations.shift()
    telemetry.recentFetchBytes.shift()
    telemetry.recentMediaSeconds.shift()
    telemetry.recentFetchStarts.shift()
    telemetry.recentFetchEnds.shift()
  }
}

/**
 * 最近若干次「真实网络下载」折算出的速率（bits/s）；一次都没有时返回 0。
 *
 * 缓存命中必须拿它做参照：hls.js 用 (parsing.end - loading.start) 给 ABR 采样，
 * 并把时长下限兜到 50ms，所以一个 1 MB 的命中片段如果报「0 ms 下完」，就会被算成
 * ~160 Mbps 写进 EWMA。命中越多、估计值越高，最后自适应码率一路钉在最高档，
 * 而实际网络根本没那么快 —— 表现为换个集就开始卡。
 */
function measuredNetworkBps(): number {
  if (netDiag.throughputBps > 0) return netDiag.throughputBps * .8
  const n = telemetry.recentFetchDurations.length
  if (n === 0) return 0
  let bytes = 0
  let ms = 0
  for (let i = 0; i < n; i++) {
    bytes += telemetry.recentFetchBytes[i] || 0
    ms += telemetry.recentFetchDurations[i]
  }
  if (bytes <= 0 || ms <= 0) return 0
  return (bytes * 8) / (ms / 1000)
}

/**
 * 把「从缓存拿到」折算成一段假想的网络耗时，让 ABR 采样结果与这次真的去下载
 * 基本一致 —— 也就是对码率决策保持中性，既不因为命中而升档，也不伪造一条带宽记录。
 * 还没有任何真实样本时返回 0（调用方保持原样上报）。
 */
export function cacheHitShapingMs(bytes: number): number {
  const bps = measuredNetworkBps()
  if (bps <= 0) return 0
  return (bytes * 8 * 1000) / bps
}

export function notifyFragmentRequested(absUrl: string): void {
  if (!session.enabled || !absUrl) return
  if (cacheHas(absUrl)) telemetry.curEpStats.hits++
  else telemetry.curEpStats.misses++
  fireListeners()
}
