// 片段缓存子系统的可调参数：原本内联在 tsCache.ts 顶部，集中到这里后每个模块
// 只 import 自己关心的常量，改阈值不再需要翻 1900 行。
export const LOG_PREFIX = '[TsCache]'

// ====== 可调参数 ======
// ⭐ 用户建议：不要粗暴清空，用"单集上限 + 全局上限 + 最近最少使用"的 LRU 调度
//   - 单集最多 64 片（超出就淘汰该集最久没被读到的片段）
//   - 全局最多 1024 片 / 512 MB（超出就淘汰全局最久没被读到的片段）
//   - 这样切换到已看过的集，缓存仍能命中；而长期不用的片段会自然被淘汰
export const DEFAULT_PREFETCH_SECONDS = 60
export const MIN_PREFETCH_COUNT = 4
export const MAX_PREFETCH_COUNT = 20
export const PREFETCH_TRIGGER_WHEN_LESS_THAN = 12
export const PREFETCH_AHEAD_NEXT_EPISODE = 5
export const PREFETCH_DEBOUNCE_MS = 300
export const MAX_QUEUE_PER_EPISODE = 30
export const MAX_CACHED_SEGMENTS = 1024          // 全局 LRU 上限：1024 片
export const MAX_CACHED_BYTES = 512 * 1024 * 1024 // 全局上限：512 MB
export const MAX_PER_EPISODE = 64                 // ⭐ 单集上限：64 片（硬约束）
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
