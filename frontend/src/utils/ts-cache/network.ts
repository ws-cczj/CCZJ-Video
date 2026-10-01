/* eslint-disable no-console */
import {
  CONSECUTIVE_SLOW_FOR_ABR,
  COOLDOWN_SAMPLES,
  DEFAULT_PREFETCH_SECONDS,
  DIAG_CONGESTED_AVG_MS,
  DIAG_FAST_THRESHOLD_MS,
  DIAG_SLOW_AVG_MS,
  DIAG_VARIANCE_THRESHOLD,
  EXTREME_CONGESTED_AVG_MS,
  FRAGMENT_TIMEOUT_CONGESTED_MS,
  FRAGMENT_TIMEOUT_MS,
  FRAGMENT_TIMEOUT_SLOW_MS,
  LOG_PREFIX,
  MAX_PREFETCH_COUNT,
  MIN_PREFETCH_COUNT,
  REENTRY_CONFIRM_COUNT,
} from './constants'
import { netDiag, session, telemetry } from './store'
import type { NetworkMode } from './store'

// ====== 网络诊断引擎 ======
//
// 初始化 → 对比测速诊断
//   ├─ server_congested → 多连接+降码率+对冲
//   ├─ local_network_slow → 降码率+缓存储备+低并发
//   └─ normal → 白天默认策略

export function updateNetworkDiagnosis(): void {
  const durations = telemetry.recentFetchDurations
  const n = durations.length
  if (n < 3) return
  const avg = durations.reduce((a, b) => a + b, 0) / n
  const variance = durations.reduce((a, b) => a + (b - avg) ** 2, 0) / n
  const stddev = Math.sqrt(variance)
  const cv = avg > 0 ? stddev / avg : 0
  const slowCount = durations.filter((d) => d > 3000).length

  netDiag.downloadAvgMs = avg
  netDiag.downloadVariance = variance
  netDiag.slowFetchCount = slowCount

  // 连续快请求 → 恢复正常
  const lastDur = durations[n - 1]
  if (lastDur < DIAG_FAST_THRESHOLD_MS) {
    netDiag.fastFetchStreak++
  } else {
    netDiag.fastFetchStreak = 0
  }

  const prevMode = netDiag.mode

  // ⭐ 冷却递减
  if (netDiag.normalCooldown > 0) netDiag.normalCooldown--

  // ── 判定目标模式 ──
  let targetMode: NetworkMode = netDiag.mode  // 默认保持当前

  if (netDiag.fastFetchStreak >= 4 && avg < DIAG_FAST_THRESHOLD_MS) {
    targetMode = 'normal'
  } else if (avg > DIAG_CONGESTED_AVG_MS || (slowCount >= 3 && cv > DIAG_VARIANCE_THRESHOLD)) {
    // 目标为 congested → 需过冷却检查
    if (netDiag.normalCooldown > 0 && avg <= EXTREME_CONGESTED_AVG_MS) {
      // 冷却中且非极端拥塞 → 保持 normal，计数不累加
      netDiag.congestedReentryCount = 0
      targetMode = netDiag.mode === 'server_congested' ? 'normal' : netDiag.mode
    } else if (netDiag.normalCooldown > 0 && avg > EXTREME_CONGESTED_AVG_MS) {
      // 极端拥塞 → bypass 冷却
      netDiag.congestedReentryCount = 0
      targetMode = 'server_congested'
      console.log(`${LOG_PREFIX} 🔥 极端拥塞 (avg=${Math.round(avg)}ms)，bypass 冷却切入 congested`)
    } else {
      // 冷却已结束 → 需要连续 REENTRY_CONFIRM_COUNT 次确认
      netDiag.congestedReentryCount++
      if (netDiag.congestedReentryCount >= REENTRY_CONFIRM_COUNT) {
        targetMode = 'server_congested'
      }
      // 未达确认次数 → 保持当前模式
    }
  } else if (avg > DIAG_SLOW_AVG_MS && cv <= DIAG_VARIANCE_THRESHOLD) {
    // 目标为 local_network_slow → 需过冷却检查
    if (netDiag.normalCooldown > 0) {
      // 冷却中 → 阻止切入 slow
      netDiag.slowReentryCount = 0
      targetMode = netDiag.mode === 'local_network_slow' ? 'normal' : netDiag.mode
    } else {
      // 冷却已结束 → 需要连续 REENTRY_CONFIRM_COUNT 次确认
      netDiag.slowReentryCount++
      if (netDiag.slowReentryCount >= REENTRY_CONFIRM_COUNT) {
        targetMode = 'local_network_slow'
      }
    }
  } else {
    // 不满足任何切换条件 → 重置所有确认计数
    netDiag.congestedReentryCount = 0
    netDiag.slowReentryCount = 0
  }

  netDiag.mode = targetMode

  // ⭐ 从非 normal 切回 normal → 启动冷却
  if (prevMode !== 'normal' && netDiag.mode === 'normal') {
    netDiag.normalCooldown = COOLDOWN_SAMPLES
    netDiag.congestedReentryCount = 0
    netDiag.slowReentryCount = 0
    console.log(`${LOG_PREFIX} 🧊 进入冷却期 (${COOLDOWN_SAMPLES} 样本内不允许切入 congested/slow)`)
  }

  // 连续慢分片 → ABR 降级
  if (lastDur > DIAG_SLOW_AVG_MS) {
    netDiag.consecutiveSlowSegs++
    if (netDiag.consecutiveSlowSegs >= CONSECUTIVE_SLOW_FOR_ABR) {
      netDiag.abrSwitchCallback?.(-1)  // -1 = 降一级
      console.log(`${LOG_PREFIX} ⚠️ 连续 ${netDiag.consecutiveSlowSegs} 片慢 (avg=${Math.round(avg)}ms)，建议降码率`)
    }
  } else {
    netDiag.consecutiveSlowSegs = 0
  }
  if (prevMode !== netDiag.mode) {
    console.log(`${LOG_PREFIX} 🔍 网络诊断: ${prevMode} → ${netDiag.mode} (avg=${Math.round(avg)}ms, cv=${cv.toFixed(2)}, slow=${slowCount}/${n}, cooldown=${netDiag.normalCooldown}, congRe=${netDiag.congestedReentryCount}, slowRe=${netDiag.slowReentryCount})`)
  }
}

export function getNetworkMode(): NetworkMode { return netDiag.mode }

/**
 * ⭐ v3: 重置网络诊断状态 —— 新视频 = 新网络环境。
 * setEpisodes 与 clear 共用同一份重置，字段顺序保持与原实现一致。
 */
export function resetNetworkDiagnosis(): void {
  netDiag.mode = 'normal'
  netDiag.downloadAvgMs = 0
  netDiag.downloadVariance = 0
  netDiag.slowFetchCount = 0
  netDiag.fastFetchStreak = 0
  netDiag.consecutiveSlowSegs = 0
}

// ====== 自适应预取策略（v3 — 聪明地缓存）======
//
// 分片选择策略：不只"多缓存"，而要"聪明地缓存"
// 预存128个ts，但晚上播放位置附近的 ts 都下载慢，光缓存多也没用（远水不解近渴）
//
// 三级区域权重：
//   紧急区 (pos+1 ~ pos+2): 最高优先，对冲请求双并发取最快
//   温热区 (pos+3 ~ pos+15): 中等优先，密集预取
//   冷区   (pos+16+):        低优先，稀疏预取
//
// 网络模式适配：
//   server_congested → 全力紧急区 + 对冲，减少冷区
//   local_network_slow → 均匀分配，低并发
//   normal → 标准分布

function avgFetchMs(): number {
  return netDiag.downloadAvgMs || (telemetry.recentFetchDurations.reduce((a, b) => a + b, 0) / telemetry.recentFetchDurations.length)
}

export function adaptivePrefetchCount(): number {
  const base = Math.ceil(DEFAULT_PREFETCH_SECONDS / Math.max(session.targetDuration, 1))
  if (telemetry.recentFetchDurations.length < 3) return clamp(base, MIN_PREFETCH_COUNT, MAX_PREFETCH_COUNT)
  const avg = avgFetchMs()
  if (netDiag.mode === 'server_congested') return clamp(base + 10, MIN_PREFETCH_COUNT, MAX_PREFETCH_COUNT)
  if (netDiag.mode === 'local_network_slow') return clamp(base + 4, MIN_PREFETCH_COUNT, MAX_PREFETCH_COUNT)
  if (avg < 500) return clamp(base - 2, MIN_PREFETCH_COUNT, MAX_PREFETCH_COUNT)
  return clamp(base, MIN_PREFETCH_COUNT, MAX_PREFETCH_COUNT)
}

export function adaptiveBufferOffset(): number {
  if (telemetry.recentFetchDurations.length < 3) return 8
  const avg = avgFetchMs()
  const ratio = avg / (session.targetDuration * 1000)
  // 拥堵时：偏移小 → 优先缓存近处（对冲+密集）
  if (netDiag.mode === 'server_congested') return 3
  if (avg > 5000 || ratio > 1.5) return 20
  if (avg > 3000 || ratio > 1.0) return 15
  if (avg > 1500 || ratio > 0.5) return 12
  if (avg < 500) return 8
  return 10
}

export function adaptiveSpreadStep(): number {
  if (telemetry.recentFetchDurations.length < 3) return 2
  const avg = avgFetchMs()
  const ratio = avg / (session.targetDuration * 1000)
  if (netDiag.mode === 'server_congested') return 3
  if (avg > 5000 || ratio > 1.5) return 4
  if (avg > 3000 || ratio > 1.0) return 3
  if (avg > 1500 || ratio > 0.5) return 2
  if (avg < 300) return 1
  return 2
}

/** 动态并发数：根据网络状况调整同时拉取的分片数 */
export function adaptiveConcurrency(): number {
  switch (netDiag.mode) {
    case 'server_congested': return 1
    case 'local_network_slow': return 1
    default: return 1
  }
}

/** 分片超时：根据网络状况动态调整 */
export function adaptiveFragmentTimeout(): number {
  switch (netDiag.mode) {
    case 'server_congested': return FRAGMENT_TIMEOUT_CONGESTED_MS
    case 'local_network_slow': return FRAGMENT_TIMEOUT_SLOW_MS
    default: return FRAGMENT_TIMEOUT_MS
  }
}

function clamp(v: number, min: number, max: number): number { return Math.max(min, Math.min(max, v)) }
