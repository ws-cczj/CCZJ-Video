// 分片缓存预算与补货策略参数。
export const LOG_PREFIX = '[TsCache]'

// ====== 可调参数 ======
// 单集与全局预算共同约束 LRU，超限时淘汰最久未使用的条目。
//   - 单集最多 128 片（超出就淘汰该集最久没被读到的片段）
//   - 全局最多 2048 片 / 256 MiB（超出就淘汰全局最久没被读到的片段）
//   - 这样切换到已看过的集，缓存仍能命中；而长期不用的片段会自然被淘汰
export const DEFAULT_PREFETCH_SECONDS = 60
export const PREFETCH_TRIGGER_WHEN_LESS_THAN = 12
export const PREFETCH_AHEAD_NEXT_EPISODE = 5
export const PREFETCH_DEBOUNCE_MS = 300
export const MAX_QUEUE_PER_EPISODE = 60
export const MAX_CACHED_SEGMENTS = 2048
export const MAX_CACHED_BYTES = 256 * 1024 * 1024 // Retained segment bytes, excluding decoder/HLS buffers.
export const MAX_PER_EPISODE = 128
export const MAX_PLAYLIST_BYTES = 16 * 1024 * 1024
export const MAX_FRAGMENT_CAPTURE_BYTES = 64 * 1024 * 1024
export const PREFETCH_CONCURRENCY = 3
export const STANDARD_INVENTORY = 30
export const MIN_INVENTORY = Math.ceil(STANDARD_INVENTORY * .4)
export const MIN_REDUNDANCY = Math.ceil(STANDARD_INVENTORY * .2)
export const MAX_REDUNDANCY = Math.ceil(STANDARD_INVENTORY * .8)
export const DEFAULT_REDUNDANCY = Math.ceil(STANDARD_INVENTORY * .4)
export const MIN_PREFETCH_COUNT = DEFAULT_REDUNDANCY
// Include the first segment strictly above the standard plus redundancy.
export const MAX_PREFETCH_COUNT = Math.min(MAX_QUEUE_PER_EPISODE, STANDARD_INVENTORY + MAX_REDUNDANCY + 1)
export const SPEED_SAMPLE_COUNT = 8

// ====== 网络诊断阈值 ======
export const DIAG_CONGESTED_AVG_MS = 5000       // avg > 5s → server_congested
export const DIAG_SLOW_AVG_MS = 2000            // avg > 2s → local_network_slow
export const DIAG_FAST_THRESHOLD_MS = 1000      // avg < 1s 连续4次 → normal
export const DIAG_VARIANCE_THRESHOLD = 0.5      // stddev/avg > 0.5 → 高方差（服务器侧抖动）
export const HEDGE_STAGGER_MS = 1000            // 对冲请求延迟发射间隔
export const FRAGMENT_TIMEOUT_MS = 8000         // 普通分片超时
export const FRAGMENT_TIMEOUT_CONGESTED_MS = 12000  // 拥堵时放宽超时
export const FRAGMENT_TIMEOUT_SLOW_MS = 10000   // 慢网络超时
export const CONSECUTIVE_SLOW_FOR_ABR = 3       // 连续N个慢分片 → 触发 ABR 降级
export const COOLDOWN_SAMPLES = 6               // 切回 normal 后的冷却样本数
export const REENTRY_CONFIRM_COUNT = 3          // 冷却后需连续 N 次确认才允许重新切入 congested
export const EXTREME_CONGESTED_AVG_MS = 8000    // 极端拥塞阈值：bypass 冷却直接切入
