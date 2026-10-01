import { SPEED_SAMPLE_COUNT } from './constants'
import { cacheHas } from './memory-cache'
import { fireListeners, session, telemetry } from './store'

/**
 * 命中率计数 + 最近若干次真实下载的速率样本。
 *
 * 计数按集分开（store.telemetry.epStats），速率样本是全局滑窗；两者都只在这里改，
 * 别处只读，避免「谁都能记一笔」导致统计口径漂移。
 */

export function recordFetchDuration(ms: number, bytes: number): void {
  telemetry.recentFetchDurations.push(ms)
  telemetry.recentFetchBytes.push(bytes)
  if (telemetry.recentFetchDurations.length > SPEED_SAMPLE_COUNT) {
    telemetry.recentFetchDurations.shift()
    telemetry.recentFetchBytes.shift()
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
