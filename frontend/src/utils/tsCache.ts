/**
 * m3u8 TS 分片预取 + 内存缓存 + IndexedDB 持久化
 *
 * 核心机制：
 *   1. fetch 拦截：透明拦截 hls.js 的 .ts 请求，命中 LRU 直接返回 ArrayBuffer
 *   2. 自适应预取：根据网速动态调整预取窗口大小和起始偏移
 *   3. 详情页预取：自行 fetch + 解析 m3u8，不等播放器
 *   4. IndexedDB 持久化：磁盘 LRU + 2 天 TTL
 */
/* eslint-disable no-console */
import { readStorage, writeStorage } from '../platform/storage'
import { SegmentDiskCache, type DiskCacheInfo } from './tsCacheDisk'

const LOG_PREFIX = '[TsCache]'

// ====== 可调参数 ======
// ⭐ 用户建议：不要粗暴清空，用"单集上限 + 全局上限 + 最近最少使用"的 LRU 调度
//   - 单集最多 64 片（超出就淘汰该集最久没被读到的片段）
//   - 全局最多 1024 片 / 512 MB（超出就淘汰全局最久没被读到的片段）
//   - 这样切换到已看过的集，缓存仍能命中；而长期不用的片段会自然被淘汰
const DEFAULT_PREFETCH_SECONDS = 60
const MIN_PREFETCH_COUNT = 4
const MAX_PREFETCH_COUNT = 20
const PREFETCH_TRIGGER_WHEN_LESS_THAN = 12
const PREFETCH_AHEAD_NEXT_EPISODE = 5
const PREFETCH_DEBOUNCE_MS = 300
const MAX_QUEUE_PER_EPISODE = 30
const MAX_CACHED_SEGMENTS = 1024          // 全局 LRU 上限：1024 片
const MAX_CACHED_BYTES = 512 * 1024 * 1024 // 全局上限：512 MB
const MAX_PER_EPISODE = 64                 // ⭐ 单集上限：64 片（硬约束）
const SPEED_SAMPLE_COUNT = 8

// ====== 网络诊断阈值 ======
const DIAG_CONGESTED_AVG_MS = 5000       // avg > 5s → server_congested
const DIAG_SLOW_AVG_MS = 2000            // avg > 2s → local_network_slow
const DIAG_FAST_THRESHOLD_MS = 1000      // avg < 1s 连续4次 → normal
const DIAG_VARIANCE_THRESHOLD = 0.5      // stddev/avg > 0.5 → 高方差（服务器侧抖动）
const HEDGE_STAGGER_MS = 1000            // 对冲请求延迟发射间隔
const FRAGMENT_TIMEOUT_MS = 8000         // 普通分片超时
const FRAGMENT_TIMEOUT_CONGESTED_MS = 12000  // 拥堵时放宽超时
const FRAGMENT_TIMEOUT_SLOW_MS = 10000   // 慢网络超时
const CONSECUTIVE_SLOW_FOR_ABR = 3       // 连续N个慢分片 → 触发 ABR 降级
const COOLDOWN_SAMPLES = 6               // 切回 normal 后的冷却样本数
const REENTRY_CONFIRM_COUNT = 3          // 冷却后需连续 N 次确认才允许重新切入 congested
const EXTREME_CONGESTED_AVG_MS = 8000    // 极端拥塞阈值：bypass 冷却直接切入

// ====== 类型 ======

interface EpisodeLite {
  source_key?: string
  vod_id: string | number
  ep_url: string
  ep_name?: string
  ep_num?: number
}

let currentSourceKey = ''

/**
 * 生成"来源+视频+集数"稳定唯一 key ——
 *   - 包含 source_key，切换采集源时不会误复用缓存
 *   - 跨视频即使 ep_num 相同也不同
 */
function episodeKey(sourceKey: unknown, vodId: unknown, epNum: unknown): string {
  return `ep_${String(sourceKey ?? '')}_${String(vodId ?? '')}_${String(epNum ?? '')}`
}

function epKeyFrom(ep: EpisodeLite | undefined | null, fallbackIdx?: number): string {
  if (!ep) return ''
  const sk = ep.source_key ?? currentSourceKey
  return episodeKey(sk, ep.vod_id, ep.ep_num ?? fallbackIdx ?? 0)
}

interface FetchJob {
  url: string
  episodeKey: string      // 之前是 episodeIdx (number)，改字符串可跨视频区分
  priority: number
  generation?: number
}

interface CacheEntry {
  buffer: ArrayBuffer
  size: number
  cacheKey: string        // 在 lruCache 中的键（= segmentCacheKey(原始 URL)，不是原始 URL）
  ts: number              // 最后一次访问时间（Date.now），淘汰只看这一个字段
  episodeKey: string      // 所属集稳定 key (空字符串 = 未知)
  fetchMs: number         // 当初真下载这些字节用了多久（0 = 未知），命中时据此回报耗时
}

// ====== 缓存键归一化 ======
//
// CDN 给同一个分片每次下发的 URL 都换一个时效签名（?sign=…&t=…&token=…）。
// 用原始 URL 当缓存键的话，播放列表每刷新一次就集体未命中：同一份字节在内存里
// 存 N 份、在 IndexedDB 里存 N 份，彼此把对方挤出上限——缓存越大命中率反而越低。
//
// 因此缓存键只保留「资源身份」：协议+主机+路径+那些无法证明与内容无关的参数。
// 拿不准的参数一律留在键里（宁可漏命中，也不能把两个不同分片当成同一个返回内容）。
//
// 播放链路上 hls.js 看到的其实是本机代理地址（/__cczj/hls?u=<base64 上游 URL>），
// 时效签名藏在 base64 里面，所以必须先把上游 URL 解出来再归一化，否则归一化对
// 真实播放流量完全不起作用。解不出来就退回原始 URL —— 只是没有命中收益，不会错命中。
const VOLATILE_SEGMENT_PARAMS: string[] = [
  'sign', 'signature', 'token', 'access_token', 'expires', 'expire', 'expiry',
  'timestamp', 'time', 't', 'ts', 'auth_key', 'wssecret', 'wstime', 'deadline',
  'userticket',
]

const HLS_PROXY_PATH = '/__cczj/hls'

/** 从本机 HLS 代理地址里取出真正的上游 URL；不是代理地址则返回 null。 */
function decodeHlsProxyUpstream(url: string): string | null {
  const pathAt = url.indexOf(HLS_PROXY_PATH + '?')
  if (pathAt < 0) return null
  const query = url.slice(pathAt + HLS_PROXY_PATH.length + 1)
  const encoded = new URLSearchParams(query).get('u')
  if (!encoded) return null
  try {
    const b64 = encoded.replace(/-/g, '+').replace(/_/g, '/')
    const binary = atob(b64.padEnd(Math.ceil(b64.length / 4) * 4, '='))
    const bytes = Uint8Array.from(binary, (ch) => ch.charCodeAt(0))
    const upstream = new TextDecoder().decode(bytes)
    return /^https?:\/\//i.test(upstream) ? upstream : null
  } catch {
    return null
  }
}

/**
 * 分片缓存键。`range` 来自 hls.js 的 #EXT-X-BYTERANGE 片段：同一个文件的
 * 不同字节段必须分开缓存，否则第二段会拿到第一段的内容。
 */
export function segmentCacheKey(url: string, range?: string | null): string {
  const upstream = decodeHlsProxyUpstream(url) ?? url
  let parsed: URL
  try {
    parsed = new URL(upstream)
  } catch {
    return range ? `${upstream}#r=${range}` : upstream
  }
  for (const param of VOLATILE_SEGMENT_PARAMS) parsed.searchParams.delete(param)
  const query = parsed.searchParams.toString()
  const base = `${parsed.origin}${parsed.pathname}` + (query ? `?${query}` : '')
  return range ? `${base}#r=${range}` : base
}

/**
 * hls.js 把 #EXT-X-BYTERANGE 的区间放在 `context.rangeStart / rangeEnd`（end 是开区间），
 * 同一个文件的多个字节段共用一个 URL。这里既用来分开缓存键，也用来发 Range 头，
 * 语义与 hls.js 自带 loader 一致：`bytes=start-(end-1)`。
 */
function byteRangeOf(context: any): string | null {
  const start: number = context?.rangeStart || 0
  const end: number = context?.rangeEnd || 0
  if (end <= start) return null
  return `${start}-${end - 1}`
}

/** `bytes=start-end`（闭区间）的长度；解析不出来时返回 0。 */
function rangeLength(range: string): number {
  const [start, end] = range.split('-').map(Number)
  if (!Number.isFinite(start) || !Number.isFinite(end) || end < start) return 0
  return end - start + 1
}

// ====== LRU 内存缓存 ======

const lruCache = new Map<string, CacheEntry>()
let totalCacheBytes = 0

// 每集缓存条数：单集上限是硬约束，靠它把「该集是否超限」从 O(n) 遍历降成一次查表
const epCacheCount = new Map<string, number>()

function _incEpCount(epKey: string): void {
  epCacheCount.set(epKey, (epCacheCount.get(epKey) || 0) + 1)
}
function _decEpCount(epKey: string): void {
  const c = epCacheCount.get(epKey)
  if (c !== undefined) {
    if (c <= 1) epCacheCount.delete(epKey)
    else epCacheCount.set(epKey, c - 1)
  }
}

function cacheGet(url: string, range?: string | null): CacheEntry | null {
  const entry = lruCache.get(segmentCacheKey(url, range))
  if (!entry) return null
  entry.ts = Date.now()
  return entry
}

/**
 * 把片段写入内存缓存。
 *
 * `episodeKey` 由调用方给出（预取任务知道自己取的是哪一集），不再靠「拿 URL 去
 * segmentsByEpisode 里逐条比对」反推 —— 那个反推一旦因为签名轮换而比不中，条目就
 * 变成无主条目，既不受单集上限约束，也就永远不会被该上限淘汰。
 *
 * `fetchMs` 是这次下载真实花掉的时间，命中时要原样回报给 hls.js（见 loader）。
 */
function cacheSet(
  url: string,
  buf: ArrayBuffer,
  episodeKey?: string,
  range?: string | null,
  fetchMs?: number
): void {
  const key = segmentCacheKey(url, range)
  const size = buf.byteLength
  const epKey = episodeKey || currentEpKey || ''

  // 同一份内容重写（时效签名换了、缓存键没换）：先把旧条目摘掉，否则同一份字节
  // 会被 totalCacheBytes 计两次，上限越管越松。
  const prevEntry = lruCache.get(key)
  if (prevEntry) evict(prevEntry)

  // ---- 1) 硬约束：单集最多 "min(MAX_PER_EPISODE, 实际片段数)" 片 ----
  if (epKey) {
    const totalSegsForEp = segmentsByEpisode.get(epKey)?.length ?? 0
    const hardLimit = Math.min(MAX_PER_EPISODE, totalSegsForEp > 0 ? totalSegsForEp : MAX_PER_EPISODE)
    if ((epCacheCount.get(epKey) || 0) >= hardLimit) {
      const oldestOfEp = findLeastUsedEntry(epKey)
      if (oldestOfEp) evict(oldestOfEp)
    }
  }

  // ---- 2) 全局条数 / 字节上限 ----
  while (lruCache.size >= MAX_CACHED_SEGMENTS || totalCacheBytes + size > MAX_CACHED_BYTES) {
    const oldest = findLeastUsedEntry()
    if (!oldest) break
    evict(oldest)
  }

  lruCache.set(key, {
    buffer: buf,
    size,
    cacheKey: key,
    ts: Date.now(),
    episodeKey: epKey,
    fetchMs: fetchMs && fetchMs > 0 ? Math.round(fetchMs) : 0,
  })
  totalCacheBytes += size
  if (epKey) _incEpCount(epKey)
}

function evict(entry: CacheEntry): void {
  totalCacheBytes -= entry.size
  if (entry.episodeKey) _decEpCount(entry.episodeKey)
  lruCache.delete(entry.cacheKey)
}

/**
 * 真正的 LRU：谁最久没被读到就淘汰谁。
 *
 * 读一次就刷新 ts（见 cacheGet），所以「刚播过的、正在播的、刚预取完等着播的」
 * 天然排在尾部。这里不再用按距离/陈旧度加权的手工分数——那种权重会互相反超，
 * 结果是把刚插入的片段判得比一小时没人碰的片段更该淘汰。
 */
function findLeastUsedEntry(onlyEpisodeKey?: string): CacheEntry | null {
  let worst: CacheEntry | null = null
  for (const entry of lruCache.values()) {
    if (onlyEpisodeKey !== undefined && entry.episodeKey !== onlyEpisodeKey) continue
    if (!worst || entry.ts < worst.ts) worst = entry
  }
  return worst
}

function cacheHas(url: string, range?: string | null): boolean { return lruCache.has(segmentCacheKey(url, range)) }
function cacheClear(): void { lruCache.clear(); totalCacheBytes = 0; epCacheCount.clear() }

// ====== 全局状态 ======

let enabled = false
let episodes: EpisodeLite[] = []
let currentEpIdx = -1                    // 保留：当前播放"第几集（列表索引）"
let currentEpKey = ''                    // ⭐ 新增：当前播放集的稳定 key（vod_id+ep_num）
let targetDuration = 6
// 当前正在播放的 vod_id（字符串化）
let currentVodId: string = ''

// ⭐ segmentsByEpisode 改以稳定 episodeKey 做 key，跨视频也不冲突
const segmentsByEpisode = new Map<string, string[]>()
const epQueueCount = new Map<string, number>()
const pendingUrls = new Set<string>()
const queue: FetchJob[] = []
let cacheSession = 0
const inflightBySession = new Map<number, number>()
const prefetchControllers = new Set<AbortController>()
let debounceTimer: number | null = null
// hls.js owns playback buffering. Background fetches start only once there is
// sufficient media ahead, so they cannot compete with first play or recovery.
let playbackBufferAhead = 0

// 去重键也走归一化：签名换了但内容没换的片段，不该被当成两个不同的下载任务。
function pendingKey(url: string, generation = cacheSession): string {
  return `${generation}:${segmentCacheKey(url)}`
}

function hedgeKey(url: string, generation = cacheSession): string {
  return `${generation}:${segmentCacheKey(url)}`
}

function activeInflight(): number {
  return inflightBySession.get(cacheSession) || 0
}

function finishInflight(generation: number): void {
  const next = (inflightBySession.get(generation) || 0) - 1
  if (next > 0) inflightBySession.set(generation, next)
  else inflightBySession.delete(generation)
}

function beginCacheSession(): void {
  cacheSession++
  for (const controller of prefetchControllers) controller.abort()
  prefetchControllers.clear()
  for (const job of queue) pendingUrls.delete(pendingKey(job.url, job.generation))
  queue.length = 0
  epQueueCount.clear()
}

// ⭐ 优化：按集数级统计（避免跨集数污染），替代单一全局 hits/misses
interface EpisodeCounter { hits: number; misses: number }
const epStats = new Map<string, EpisodeCounter>()
const recentFetchDurations: number[] = []
// 与 recentFetchDurations 一一对应的字节数：只有耗时算不出速率，也就无法判断命中该折算成多久
const recentFetchBytes: number[] = []
// 为当前集使用一个可变引用，减少每次查找
let _curEpStats: EpisodeCounter = { hits: 0, misses: 0 }

// 已播放片段追踪：key = episodeKey，value = 已播放 segment 索引集合
const playedSegmentsByEpisode = new Map<string, Set<number>>()

// ====== 网络诊断状态 ======
type NetworkMode = 'normal' | 'server_congested' | 'local_network_slow'
let _networkMode: NetworkMode = 'normal'
let _downloadAvgMs = 0        // 滚动平均下载耗时 (ms)
let _downloadVariance = 0     // 下载耗时方差
let _slowFetchCount = 0       // 近期慢请求计数（>3s 计一次）
let _fastFetchStreak = 0      // 连续快请求计数（<1s 计一次）
let _consecutiveSlowSegs = 0  // 连续慢分片计数（触发 ABR 降级）
let _abrSwitchCallback: ((targetLevel: number) => void) | null = null
let _normalCooldown = 0         // 冷却倒计时：>0 时阻止切入 congested / local_network_slow
let _congestedReentryCount = 0  // 冷却结束后，连续确认 congested 的计数
let _slowReentryCount = 0       // 冷却结束后，连续确认 local_network_slow 的计数
const hedgeInFlight = new Set<string>()  // 正在进行对冲请求的 URL

// ====== fetch 透明拦截 ======

// ====== 网络诊断引擎 ======
//
// 初始化 → 对比测速诊断
//   ├─ server_congested → 多连接+降码率+对冲
//   ├─ local_network_slow → 降码率+缓存储备+低并发
//   └─ normal → 白天默认策略

function updateNetworkDiagnosis(): void {
  const n = recentFetchDurations.length
  if (n < 3) return
  const avg = recentFetchDurations.reduce((a, b) => a + b, 0) / n
  const variance = recentFetchDurations.reduce((a, b) => a + (b - avg) ** 2, 0) / n
  const stddev = Math.sqrt(variance)
  const cv = avg > 0 ? stddev / avg : 0
  const slowCount = recentFetchDurations.filter((d) => d > 3000).length

  _downloadAvgMs = avg
  _downloadVariance = variance
  _slowFetchCount = slowCount

  // 连续快请求 → 恢复正常
  const lastDur = recentFetchDurations[n - 1]
  if (lastDur < DIAG_FAST_THRESHOLD_MS) {
    _fastFetchStreak++
  } else {
    _fastFetchStreak = 0
  }

  const prevMode = _networkMode

  // ⭐ 冷却递减
  if (_normalCooldown > 0) _normalCooldown--

  // ── 判定目标模式 ──
  let targetMode: NetworkMode = _networkMode  // 默认保持当前

  if (_fastFetchStreak >= 4 && avg < DIAG_FAST_THRESHOLD_MS) {
    targetMode = 'normal'
  } else if (avg > DIAG_CONGESTED_AVG_MS || (slowCount >= 3 && cv > DIAG_VARIANCE_THRESHOLD)) {
    // 目标为 congested → 需过冷却检查
    if (_normalCooldown > 0 && avg <= EXTREME_CONGESTED_AVG_MS) {
      // 冷却中且非极端拥塞 → 保持 normal，计数不累加
      _congestedReentryCount = 0
      targetMode = _networkMode === 'server_congested' ? 'normal' : _networkMode
    } else if (_normalCooldown > 0 && avg > EXTREME_CONGESTED_AVG_MS) {
      // 极端拥塞 → bypass 冷却
      _congestedReentryCount = 0
      targetMode = 'server_congested'
      console.log(`${LOG_PREFIX} 🔥 极端拥塞 (avg=${Math.round(avg)}ms)，bypass 冷却切入 congested`)
    } else {
      // 冷却已结束 → 需要连续 REENTRY_CONFIRM_COUNT 次确认
      _congestedReentryCount++
      if (_congestedReentryCount >= REENTRY_CONFIRM_COUNT) {
        targetMode = 'server_congested'
      }
      // 未达确认次数 → 保持当前模式
    }
  } else if (avg > DIAG_SLOW_AVG_MS && cv <= DIAG_VARIANCE_THRESHOLD) {
    // 目标为 local_network_slow → 需过冷却检查
    if (_normalCooldown > 0) {
      // 冷却中 → 阻止切入 slow
      _slowReentryCount = 0
      targetMode = _networkMode === 'local_network_slow' ? 'normal' : _networkMode
    } else {
      // 冷却已结束 → 需要连续 REENTRY_CONFIRM_COUNT 次确认
      _slowReentryCount++
      if (_slowReentryCount >= REENTRY_CONFIRM_COUNT) {
        targetMode = 'local_network_slow'
      }
    }
  } else {
    // 不满足任何切换条件 → 重置所有确认计数
    _congestedReentryCount = 0
    _slowReentryCount = 0
  }

  _networkMode = targetMode

  // ⭐ 从非 normal 切回 normal → 启动冷却
  if (prevMode !== 'normal' && _networkMode === 'normal') {
    _normalCooldown = COOLDOWN_SAMPLES
    _congestedReentryCount = 0
    _slowReentryCount = 0
    console.log(`${LOG_PREFIX} 🧊 进入冷却期 (${COOLDOWN_SAMPLES} 样本内不允许切入 congested/slow)`)
  }

  // 连续慢分片 → ABR 降级
  if (lastDur > DIAG_SLOW_AVG_MS) {
    _consecutiveSlowSegs++
    if (_consecutiveSlowSegs >= CONSECUTIVE_SLOW_FOR_ABR) {
      _abrSwitchCallback?.(-1)  // -1 = 降一级
      console.log(`${LOG_PREFIX} ⚠️ 连续 ${_consecutiveSlowSegs} 片慢 (avg=${Math.round(avg)}ms)，建议降码率`)
    }
  } else {
    _consecutiveSlowSegs = 0
  }
  if (prevMode !== _networkMode) {
    console.log(`${LOG_PREFIX} 🔍 网络诊断: ${prevMode} → ${_networkMode} (avg=${Math.round(avg)}ms, cv=${cv.toFixed(2)}, slow=${slowCount}/${n}, cooldown=${_normalCooldown}, congRe=${_congestedReentryCount}, slowRe=${_slowReentryCount})`)
  }
}

function getNetworkMode(): NetworkMode { return _networkMode }

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

function adaptivePrefetchCount(): number {
  const base = Math.ceil(DEFAULT_PREFETCH_SECONDS / Math.max(targetDuration, 1))
  if (recentFetchDurations.length < 3) return clamp(base, MIN_PREFETCH_COUNT, MAX_PREFETCH_COUNT)
  const avg = _downloadAvgMs || (recentFetchDurations.reduce((a, b) => a + b, 0) / recentFetchDurations.length)
  if (_networkMode === 'server_congested') return clamp(base + 10, MIN_PREFETCH_COUNT, MAX_PREFETCH_COUNT)
  if (_networkMode === 'local_network_slow') return clamp(base + 4, MIN_PREFETCH_COUNT, MAX_PREFETCH_COUNT)
  if (avg < 500) return clamp(base - 2, MIN_PREFETCH_COUNT, MAX_PREFETCH_COUNT)
  return clamp(base, MIN_PREFETCH_COUNT, MAX_PREFETCH_COUNT)
}

function adaptiveBufferOffset(): number {
  if (recentFetchDurations.length < 3) return 8
  const avg = _downloadAvgMs || (recentFetchDurations.reduce((a, b) => a + b, 0) / recentFetchDurations.length)
  const ratio = avg / (targetDuration * 1000)
  // 拥堵时：偏移小 → 优先缓存近处（对冲+密集）
  if (_networkMode === 'server_congested') return 3
  if (avg > 5000 || ratio > 1.5) return 20
  if (avg > 3000 || ratio > 1.0) return 15
  if (avg > 1500 || ratio > 0.5) return 12
  if (avg < 500) return 8
  return 10
}

function adaptiveSpreadStep(): number {
  if (recentFetchDurations.length < 3) return 2
  const avg = _downloadAvgMs || (recentFetchDurations.reduce((a, b) => a + b, 0) / recentFetchDurations.length)
  const ratio = avg / (targetDuration * 1000)
  if (_networkMode === 'server_congested') return 3
  if (avg > 5000 || ratio > 1.5) return 4
  if (avg > 3000 || ratio > 1.0) return 3
  if (avg > 1500 || ratio > 0.5) return 2
  if (avg < 300) return 1
  return 2
}

/** 动态并发数：根据网络状况调整同时拉取的分片数 */
function adaptiveConcurrency(): number {
  switch (_networkMode) {
    case 'server_congested': return 1
    case 'local_network_slow': return 1
    default: return 1
  }
}

/** 分片超时：根据网络状况动态调整 */
function adaptiveFragmentTimeout(): number {
  switch (_networkMode) {
    case 'server_congested': return FRAGMENT_TIMEOUT_CONGESTED_MS
    case 'local_network_slow': return FRAGMENT_TIMEOUT_SLOW_MS
    default: return FRAGMENT_TIMEOUT_MS
  }
}

function clamp(v: number, min: number, max: number): number { return Math.max(min, Math.min(max, v)) }

// ====== 公共 API ======

export function setEpisodes(list: EpisodeLite[]): void {
  episodes = Array.isArray(list) ? list.slice() : []

  const newVodId = (episodes[0]?.vod_id != null) ? String(episodes[0].vod_id) : ''

  // ⭐ 不再清空 LRU — 只清空"相对集索引"的映射
  segmentsByEpisode.clear()
  playedSegmentsByEpisode.clear()
  beginCacheSession()
  pendingUrls.clear()
  epStats.clear()  // ⭐ 集数级统计重置
  _curEpStats = { hits: 0, misses: 0 }
  recentFetchDurations.length = 0
  recentFetchBytes.length = 0
  // ⭐ v3: 重置网络诊断状态（新视频 = 新网络环境）
  _networkMode = 'normal'
  _downloadAvgMs = 0
  _downloadVariance = 0
  _slowFetchCount = 0
  _fastFetchStreak = 0
  _consecutiveSlowSegs = 0
  hedgeInFlight.clear()

  currentVodId = newVodId
  currentSourceKey = episodes[0]?.source_key ? String(episodes[0].source_key) : ''
  currentEpIdx = -1
  currentEpKey = ''
  playbackBufferAhead = 0
  fireListeners()
  // Cache restoration is scoped to the current episode in setCurrentEpisode.
}

export function setCurrentEpisode(idx: number): void {
  if (idx < 0 || idx >= episodes.length) {
    currentEpIdx = -1
    currentEpKey = ''
    return
  }
  if (currentEpIdx === idx) return
  currentEpIdx = idx
  const ep = episodes[idx]
  const newKey = epKeyFrom(ep, idx)
  if (currentEpKey === newKey) return
  currentEpKey = newKey
  // The queued URLs belong to the old episode. Completed cache entries are
  // still useful under LRU, but queued work must not survive an episode jump.
  beginCacheSession()
  playbackBufferAhead = 0
  if (currentEpKey) {
    if (!playedSegmentsByEpisode.has(currentEpKey)) {
      playedSegmentsByEpisode.set(currentEpKey, new Set<number>())
    }
    // ⭐ 从 epStats 中拿出该集的计数器（或新建）
    let cs = epStats.get(currentEpKey)
    if (!cs) { cs = { hits: 0, misses: 0 }; epStats.set(currentEpKey, cs) }
    _curEpStats = cs

    // ⭐ 按需从磁盘加载该集的缓存（只有当前集的片段才恢复到内存）
    diskLoadForEpisode(currentEpKey).catch(() => { })
  } else {
    _curEpStats = { hits: 0, misses: 0 }
  }
  fireListeners()
}

export function setTargetDuration(sec: number): void { if (sec > 0 && sec <= 30) targetDuration = sec }

export function setPlaybackBufferAhead(seconds: number): void {
  playbackBufferAhead = Number.isFinite(seconds) ? Math.max(0, seconds) : 0
  if (canPrefetch()) scheduleDrain()
}

/** 把解析好的片段列表绑定到"当前集"或 episodes[epIdx]，避免越界/无 key 的情况 */
export function setSegments(segments: string[], epIdx?: number): void {
  let key = currentEpKey
  // 未显式指定 currentEpKey，但传入了 epIdx → 用 episodes[epIdx] 生成
  if (!key && typeof epIdx === 'number' && epIdx >= 0 && episodes[epIdx]) {
    const ep = episodes[epIdx]
    key = epKeyFrom(ep, epIdx)
  }
  // 都没有，但 episodes 至少有一集 → 用第 0 集
  if (!key && episodes.length > 0) {
    key = epKeyFrom(episodes[0], 0)
  }
  if (!key) return

  const seen = new Set<string>()
  const list: string[] = []
  for (const s of segments) { if (s && !seen.has(s)) { seen.add(s); list.push(s) } }
  segmentsByEpisode.set(key, list)

  // 如果此刻 currentEpKey 为空，也把它设为这集的 key（后续 notifyCurrentTs 能匹配）
  if (!currentEpKey) {
    currentEpKey = key
    if (typeof epIdx === 'number' && epIdx >= 0) currentEpIdx = epIdx
    let cs = epStats.get(currentEpKey)
    if (!cs) { cs = { hits: 0, misses: 0 }; epStats.set(currentEpKey, cs) }
    _curEpStats = cs
  }

  // Records in the v2 cache are indexed by episode key. Reload after the playlist is parsed.
  if (key === currentEpKey && list.length > 0) {
    diskLoadForEpisode(key).catch(() => { })
  }
  fireListeners()
}

export async function prefetchFirst(count: number): Promise<number> {
  if (!enabled || !currentEpKey) return 0
  const segs = segmentsByEpisode.get(currentEpKey) || []
  if (segs.length === 0) return 0
  const want = Math.min(count, Math.min(adaptivePrefetchCount(), segs.length))
  const end = Math.min(want, segs.length)
  let added = 0
  for (let i = end - 1; i >= 0; i--) {
    if (enqueue({ url: segs[i], episodeKey: currentEpKey, priority: 1 })) added++
  }
  if (added > 0) scheduleDrain()
  return added
}

export async function prefetchNextEpisode(count: number): Promise<number> {
  if (!enabled) return 0
  const nextIdx = currentEpIdx + 1
  if (nextIdx < 0 || nextIdx >= episodes.length) return 0
  const nextEp = episodes[nextIdx]
  const nextEpKey = epKeyFrom(nextEp, nextIdx)
  if (!nextEpKey) return 0
  const segs = segmentsByEpisode.get(nextEpKey) || []
  if (segs.length === 0) return 0
  const end = Math.min(count, segs.length)
  let added = 0
  for (let i = end - 1; i >= 0; i--) {
    if (enqueue({ url: segs[i], episodeKey: nextEpKey, priority: 2 })) added++
  }
  if (added > 0) { console.log(`${LOG_PREFIX} ⏭ 预取 #${nextIdx} 集`); scheduleDrain() }
  return added
}

/** 详情页自行 fetch m3u8 并解析 TS URL，不等播放器；使用 episodes[epIdx] 的 vod_id+ep_num 做稳定 key */
export async function prefetchFromM3u8(m3u8Url: string, epIdx: number): Promise<number> {
  if (!enabled) return 0
  try {
    const ep = episodes[epIdx]
    if (!ep) return 0
    const epKey = epKeyFrom(ep, epIdx)

    const parsed = await fetchAndParseM3u8(m3u8Url)
    if (parsed.isMaster || parsed.urls.length === 0) return 0

    const segs = parsed.urls
    segmentsByEpisode.set(epKey, segs)
    if (parsed.targetduration > 0 && parsed.targetduration <= 30) targetDuration = parsed.targetduration

    const played = playedSegmentsByEpisode.get(epKey)
    const totalActual = segs.length
    const count = Math.min(adaptivePrefetchCount(), totalActual)
    console.log(`${LOG_PREFIX} 📄 解析 #${epIdx} 集: ${totalActual} 片, 预取 ${count} 片`)

    const contiguousEnd = Math.ceil(count * 0.4)
    let added = 0
    let i = 0
    while (added < contiguousEnd && i < segs.length) {
      if (played && played.has(i)) { i++; continue }
      if (enqueue({ url: segs[i], episodeKey: epKey, priority: 1 })) added++
      i++
    }
    const step = Math.max(2, adaptiveSpreadStep())
    while (added < count && i < segs.length) {
      if (!played || !played.has(i)) {
        if (enqueue({ url: segs[i], episodeKey: epKey, priority: 1 })) added++
      }
      i += step
    }
    if (added > 0) scheduleDrain()
    return added
  } catch { return 0 }
}

export function notifyCurrentTs(absUrl: string): void {
  if (!enabled || !absUrl || !currentEpKey) return
  const segs = segmentsByEpisode.get(currentEpKey) || []
  if (segs.length === 0) return
  const pos = findSegmentIndex(segs, absUrl)
  if (pos < 0) return

  // 1. 标记当前及之前的片段为"已播放"
  let playedSet = playedSegmentsByEpisode.get(currentEpKey)
  if (!playedSet) { playedSet = new Set<number>(); playedSegmentsByEpisode.set(currentEpKey, playedSet) }
  for (let i = 0; i <= pos; i++) playedSet.add(i)

  const totalActual = segs.length
  const remaining = totalActual - pos - 1
  if (remaining <= 0) return

  // 2. 三级区域权重预取
  //   紧急区 (pos+1 ~ pos+2): 对冲请求，双并发取最快
  //   温热区 (pos+3 ~ pos+15): 密集预取，高优先
  //   冷区   (pos+16+):        稀疏预取，广覆盖
  const totalCount = Math.min(adaptivePrefetchCount(), remaining)
  const spreadStep = adaptiveSpreadStep()
  let addedCount = 0

  // --- 紧急区：对冲请求（仅拥堵时启用）---
  if (_networkMode === 'server_congested') {
    let hedgeAdded = 0
    for (let offset = 1; offset <= 2 && pos + offset < segs.length; offset++) {
      const idx = pos + offset
      if (playedSet.has(idx)) continue
      const u = segs[idx]
      if (cacheHas(u) || pendingUrls.has(pendingKey(u)) || hedgeInFlight.has(hedgeKey(u))) continue
      // 对冲：双请求取最快
      hedgeAdded++
      addedCount++
      hedgeInFlight.add(hedgeKey(u))
      const t0 = performance.now()
      const generation = cacheSession
      const episode = currentEpKey
      hedgeFetch(u).then((buf) => {
        if (buf && generation === cacheSession) {
          const elapsed = performance.now() - t0
          cacheSet(u, buf, episode, null, elapsed)
          diskSave(u, buf, episode).catch(() => { })
          recordFetchDuration(elapsed, buf.byteLength)
          fireListeners()
        }
      }).finally(() => { hedgeInFlight.delete(hedgeKey(u, generation)) })
    }
    if (hedgeAdded > 0) {
      console.log(`${LOG_PREFIX} 🚨 紧急对冲: ${hedgeAdded} 片 (pos=${pos}, mode=${_networkMode})`)
    }
  }

  // --- 温热区：密集预取 (pos+3 ~ pos+15) ---
  const warmStart = 3
  const warmEnd = Math.min(15, remaining)
  for (let offset = warmStart; offset <= warmEnd && addedCount < totalCount; offset++) {
    const idx = pos + offset
    if (idx >= segs.length) break
    if (playedSet.has(idx)) continue
    const u = segs[idx]
    if (cacheHas(u) || pendingUrls.has(pendingKey(u)) || hedgeInFlight.has(hedgeKey(u))) continue
    if (enqueue({ url: u, episodeKey: currentEpKey, priority: 1 })) addedCount++
  }

  // --- 冷区：稀疏预取 (pos+16+, 按 spreadStep 间距) ---
  const coldStart = Math.max(16, warmEnd + 1)
  for (let offset = coldStart; addedCount < totalCount && pos + offset < segs.length; offset += spreadStep) {
    const idx = pos + offset
    if (playedSet.has(idx)) continue
    const u = segs[idx]
    if (cacheHas(u) || pendingUrls.has(pendingKey(u)) || hedgeInFlight.has(hedgeKey(u))) continue
    if (enqueue({ url: u, episodeKey: currentEpKey, priority: 2 })) addedCount++
  }

  if (pos % 5 === 0) {
    const h = _curEpStats.hits, m = _curEpStats.misses
    const total = h + m
    const rate = total === 0 ? 0 : h / total
    console.log(
      `${LOG_PREFIX} pos=${pos}/${totalActual}, prefetch=${addedCount}, ` +
      `命中率=${(rate * 100).toFixed(1)}%, mode=${_networkMode}, conc=${adaptiveConcurrency()}`
    )
  }

  // 3. 下一集预取：进度到 30% 或剩余 ≤12 片时触发
  if (currentEpIdx + 1 < episodes.length && (pos / Math.max(totalActual, 1) >= 0.3 || remaining <= PREFETCH_TRIGGER_WHEN_LESS_THAN)) {
    const nextEp = episodes[currentEpIdx + 1]
    if (nextEp) {
      const nextEpKey = epKeyFrom(nextEp, currentEpIdx + 1)
      const nextSegs = segmentsByEpisode.get(nextEpKey) || []
      if (nextSegs.length > 0) {
        const nextPlayed = playedSegmentsByEpisode.get(nextEpKey)
        let nextAdded = 0
        const nextCount = Math.min(PREFETCH_AHEAD_NEXT_EPISODE, nextSegs.length)
        for (let i = 0; i < nextSegs.length && nextAdded < nextCount; i++) {
          if (nextPlayed && nextPlayed.has(i)) continue
          if (cacheHas(nextSegs[i]) || pendingUrls.has(pendingKey(nextSegs[i]))) continue
          if (enqueue({ url: nextSegs[i], episodeKey: nextEpKey, priority: 2 })) nextAdded++
        }
        if (nextAdded > 0) console.log(`${LOG_PREFIX} ⏭ 自动预取 #${currentEpIdx + 1} 集 (${nextAdded} 片)`)
      }
    }
  }

  scheduleDrain()
}

export function notifyFragmentRequested(absUrl: string): void {
  if (!enabled || !absUrl) return
  if (cacheHas(absUrl)) _curEpStats.hits++
  else _curEpStats.misses++
  fireListeners()
}

export function stats() {
  const segs = currentEpKey ? (segmentsByEpisode.get(currentEpKey) || []) : []
  let cachedForCurEp = 0
  if (currentEpKey) {
    for (const u of segs) if (cacheHas(u)) cachedForCurEp++
  }
  const h = _curEpStats.hits, m = _curEpStats.misses
  const total = h + m
  const played = currentEpKey ? (playedSegmentsByEpisode.get(currentEpKey)?.size || 0) : 0
  const avgMs = recentFetchDurations.length > 0
    ? Math.round(recentFetchDurations.reduce((a, b) => a + b, 0) / recentFetchDurations.length) : 0
  return {
    hits: h, misses: m, entries: cachedForCurEp, totalEntries: lruCache.size,
    bytes: totalCacheBytes, totalSegments: segs.length,
    playedSegments: played,
    hitRate: total === 0 ? 0 : h / total,
    avgFetchMs: avgMs,
    prefetchCount: adaptivePrefetchCount(),
    // 预取只维持播放窗口而非下载整集；将目标单独提供给 UI，避免把
    // “24/813 个全片段”误显示成缓存一直未完成。
    prefetchTarget: Math.min(adaptivePrefetchCount(), segs.length),
    queued: queue.filter((job) => job.episodeKey === currentEpKey).length,
    inflight: activeInflight(),
    bufferOffset: adaptiveBufferOffset(),
    spreadStep: adaptiveSpreadStep(),
    cacheMB: totalCacheBytes / 1024 / 1024,
    // ⭐ v3: 网络诊断信息
    networkMode: _networkMode,
    concurrency: adaptiveConcurrency(),
    fragmentTimeout: adaptiveFragmentTimeout(),
    consecutiveSlowSegs: _consecutiveSlowSegs,
  }
}

/** 按稳定 key 取一集的进度信息（兼容旧数字 idx 调用） */
export function episodeProgress(idx: number): { total: number; cached: number } {
  let key: string | null = null
  if (idx >= 0 && episodes[idx]) {
    const ep = episodes[idx]
    key = epKeyFrom(ep, idx)
  }
  const segs = (key && segmentsByEpisode.get(key)) || []
  const cached = segs.filter((u) => cacheHas(u)).length
  return { total: segs.length, cached }
}

export function getTotalEpisodes(): number { return episodes.length }
// hls.js always uses TsCacheLoader now. Do not monkey-patch window.fetch:
// Wails runtime RPC uses fetch too, and a global wrapper obscures its failures
// in DevTools while providing no cache benefit to the custom HLS loader.
export function enable(): void { enabled = true }

// ⭐ v3: ABR 回调注册 —— VideoPlayer 通过此接口接收降码率信号
export function setAbrSwitchCallback(cb: ((targetLevel: number) => void) | null): void {
  _abrSwitchCallback = cb
}

export function clear(): void {
  episodes = []
  currentEpIdx = -1
  currentEpKey = ''
  currentVodId = ''
  segmentsByEpisode.clear()
  playedSegmentsByEpisode.clear()
  epCacheCount.clear()
  cacheClear()
  m3u8TextCache.clear()
  beginCacheSession()
  pendingUrls.clear()
  hedgeInFlight.clear()
  epStats.clear()
  _curEpStats = { hits: 0, misses: 0 }
  recentFetchDurations.length = 0
  recentFetchBytes.length = 0
  // ⭐ v3: 重置网络诊断状态
  _networkMode = 'normal'
  _downloadAvgMs = 0
  _downloadVariance = 0
  _slowFetchCount = 0
  _fastFetchStreak = 0
  _consecutiveSlowSegs = 0
  _abrSwitchCallback = null
  if (debounceTimer != null) { window.clearTimeout(debounceTimer); debounceTimer = null }
  fireListeners()
}

// ====== 统一失效层的前端落点 ======
//
// Go 侧清完自己的缓存后发 cache:invalidate，这里按同一粒度丢掉片段缓存：内存 LRU、
// 该集的片段清单、IndexedDB 落盘副本。
//
// 刻意不碰 episodes / currentEpIdx 这类会话状态：用户可能正在看同源的另一个视频，
// 后台每写一次库就把播放器重置一遍，代价比留下几片旧片段大得多。

async function dropCacheByPrefixes(prefixes: string[]): Promise<void> {
  for (const entry of Array.from(lruCache.values())) {
    if (entry.episodeKey && prefixes.some((p) => entry.episodeKey.startsWith(p))) evict(entry)
  }
  for (const key of Array.from(segmentsByEpisode.keys())) {
    if (!prefixes.some((p) => key.startsWith(p))) continue
    segmentsByEpisode.delete(key)
    playedSegmentsByEpisode.delete(key)
    epStats.delete(key)
    epCacheCount.delete(key)
  }
  // 正在播的这一集被失效了：进行中的预取必须断掉，否则它们会把旧片段清单里的 URL
  // 重新填回刚清空的缓存。
  if (currentEpKey && prefixes.some((p) => currentEpKey.startsWith(p))) beginCacheSession()
  try {
    await diskCache.dropByEpisodePrefixes(prefixes)
  } catch { /* IndexedDB 打不开：内存侧已经失效，落盘副本最坏等到 TTL 自己走 */ }
  fireListeners()
}

/** 丢掉某一部剧全部集的片段缓存。 */
export function dropVideoCache(sourceKey: string, vodId: string): Promise<void> {
  if (!sourceKey || !vodId) return Promise.resolve()
  return dropCacheByPrefixes([`ep_${sourceKey}_${vodId}_`])
}

/** 丢掉某个采集源全部片的片段缓存。 */
export function dropSourceCache(sourceKey: string): Promise<void> {
  if (!sourceKey) return Promise.resolve()
  return dropCacheByPrefixes([`ep_${sourceKey}_`])
}

/** 用户显式清除：所有片段缓存与 m3u8 文本缓存一起丢掉，当前会话的剧集清单保留。 */
export async function dropAllSegmentCache(): Promise<void> {
  cacheClear()
  segmentsByEpisode.clear()
  playedSegmentsByEpisode.clear()
  epCacheCount.clear()
  epStats.clear()
  m3u8TextCache.clear()
  try {
    await diskCache.clear()
  } catch { /* 同上 */ }
  fireListeners()
}

// ====== 事件系统 ======

type Listener = () => void
const listeners = new Set<Listener>()

export function onStateChange(cb: Listener): () => void { listeners.add(cb); return () => listeners.delete(cb) }

let fireTimer: number | null = null
function fireListeners(): void {
  if (fireTimer != null) return
  fireTimer = window.setTimeout(() => { fireTimer = null; for (const cb of listeners) { try { cb() } catch { /* ignore */ } } }, 250)
}

// ====== 内部 ======

function findSegmentIndex(segments: string[], target: string): number {
  const i = segments.indexOf(target)
  if (i >= 0) return i
  const cleanTail = (s: string) => { try { return new URL(s).pathname.split('/').slice(-2).join('/') } catch { return s.split('?')[0].split('/').slice(-2).join('/') } }
  const t = cleanTail(target)
  for (let k = 0; k < segments.length; k++) { if (cleanTail(segments[k]) === t) return k }
  return -1
}

function enqueue(job: FetchJob): boolean {
  if (!job?.url) return false
  const generation = cacheSession
  if (pendingUrls.has(pendingKey(job.url, generation))) return false
  if (cacheHas(job.url)) return false
  const cnt = epQueueCount.get(job.episodeKey) || 0
  if (cnt >= MAX_QUEUE_PER_EPISODE) return false
  pendingUrls.add(pendingKey(job.url, generation))
  epQueueCount.set(job.episodeKey, cnt + 1)
  const scopedJob = { ...job, generation }
  if (scopedJob.priority === 1) queue.unshift(scopedJob); else queue.push(scopedJob)
  return true
}

function scheduleDrain(): void {
  if (debounceTimer != null) return
  debounceTimer = window.setTimeout(() => { debounceTimer = null; drainQueue() }, PREFETCH_DEBOUNCE_MS)
}

function drainQueue(): void {
  if (!canPrefetch()) return
  const maxConc = adaptiveConcurrency()
  while (activeInflight() < maxConc && queue.length > 0) {
    const job = queue.shift(); if (!job) break
    const generation = job.generation ?? cacheSession
    const active = activeInflight() + 1
    inflightBySession.set(generation, active)
    // ⭐ 交错启动：每个分片间隔 ~1s，避免服务器敏感封禁
    if (active > 1) {
      const staggerDelay = (active - 1) * 1000
      window.setTimeout(() => {
        runOne(job).finally(() => {
          finishInflight(generation)
          if (generation === cacheSession) drainQueue()
        })
      }, staggerDelay)
    } else {
      runOne(job).finally(() => {
        finishInflight(generation)
        if (generation === cacheSession) drainQueue()
      })
    }
  }
}

function canPrefetch(): boolean {
  return enabled && playbackBufferAhead >= Math.max(12, targetDuration * 2)
}

async function runOne(job: FetchJob): Promise<void> {
  const generation = job.generation ?? cacheSession
  if (generation !== cacheSession) return
  const t0 = performance.now()
  const timeout = adaptiveFragmentTimeout()
  let ctrl: AbortController | null = null
  let timeoutID: number | null = null
  try {
    ctrl = new AbortController()
    prefetchControllers.add(ctrl)
    timeoutID = window.setTimeout(() => ctrl?.abort(), timeout)
    const init: RequestInit = { signal: ctrl.signal }
    try { (init as any).priority = 'low' } catch { /* ignore */ }
    const resp = await fetch(job.url, init)
    if (timeoutID != null) window.clearTimeout(timeoutID)
    if (resp.ok && generation === cacheSession) {
      const buf = await resp.arrayBuffer()
      const elapsed = performance.now() - t0
      cacheSet(job.url, buf, job.episodeKey, null, elapsed)
      diskSave(job.url, buf, job.episodeKey).catch(() => { })
      recordFetchDuration(elapsed, buf.byteLength)
      updateNetworkDiagnosis()
      fireListeners()
    }
  } catch { /* 静默 */ }
  finally {
    if (timeoutID != null) window.clearTimeout(timeoutID)
    if (ctrl) prefetchControllers.delete(ctrl)
    pendingUrls.delete(pendingKey(job.url, generation))
    // Release the slot for failures as well as successes. Otherwise transient
    // errors eventually make an episode appear permanently queue-full.
    if (generation === cacheSession) {
      const remaining = (epQueueCount.get(job.episodeKey) || 0) - 1
      if (remaining > 0) epQueueCount.set(job.episodeKey, remaining)
      else epQueueCount.delete(job.episodeKey)
    }
  }
}

function recordFetchDuration(ms: number, bytes: number): void {
  recentFetchDurations.push(ms)
  recentFetchBytes.push(bytes)
  if (recentFetchDurations.length > SPEED_SAMPLE_COUNT) {
    recentFetchDurations.shift()
    recentFetchBytes.shift()
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
  const n = recentFetchDurations.length
  if (n === 0) return 0
  let bytes = 0
  let ms = 0
  for (let i = 0; i < n; i++) {
    bytes += recentFetchBytes[i] || 0
    ms += recentFetchDurations[i]
  }
  if (bytes <= 0 || ms <= 0) return 0
  return (bytes * 8) / (ms / 1000)
}

/**
 * 把「从缓存拿到」折算成一段假想的网络耗时，让 ABR 采样结果与这次真的去下载
 * 基本一致 —— 也就是对码率决策保持中性，既不因为命中而升档，也不伪造一条带宽记录。
 * 还没有任何真实样本时返回 0（调用方保持原样上报）。
 */
function cacheHitShapingMs(bytes: number): number {
  const bps = measuredNetworkBps()
  if (bps <= 0) return 0
  return (bytes * 8 * 1000) / bps
}

// ====== 对冲请求（Hedge Fetch）======
//
// 对紧邻播放位置的关键分片开两个并发请求（同一文件），取最快返回的，另一个 abort。
// 能大幅降低尾部延迟，避免单个慢请求拖住整个播放流水线。
// 第二个请求延迟 HEDGE_STAGGER_MS 后发射，避免同时冲击服务器。

function hedgeFetch(url: string): Promise<ArrayBuffer | null> {
  const ctrl1 = new AbortController()
  const ctrl2 = new AbortController()
  const sessionCtrl = new AbortController()
  prefetchControllers.add(sessionCtrl)
  const timeout = adaptiveFragmentTimeout()
  let settleRequest: ((value: ArrayBuffer | null) => void) | null = null
  let staggerTimer: number | null = null
  let timeoutTimer: number | null = null

  const doFetch = (signal: AbortSignal) => {
    const init: RequestInit = { signal }
    return fetch(url, init)
  }

  const racePromise = new Promise<ArrayBuffer | null>((resolve) => {
    let settled = false
    const settle = (value: ArrayBuffer | null) => {
      if (!settled) { settled = true; resolve(value) }
    }
    settleRequest = settle

    // 请求1：立即发射
    doFetch(ctrl1.signal)
      .then((r) => r.ok ? r.arrayBuffer() : Promise.reject(new Error('http ' + r.status)))
      .then((buf) => { settle(buf); try { ctrl2.abort() } catch { } })
      .catch(() => { /* let the delayed hedge try the same segment */ })

    // 请求2：延迟发射（错开避免服务器压力）
    staggerTimer = window.setTimeout(() => {
      if (settled || ctrl2.signal.aborted) return
      doFetch(ctrl2.signal)
        .then((r) => r.ok ? r.arrayBuffer() : Promise.reject(new Error('http ' + r.status)))
        .then((buf) => { settle(buf); try { ctrl1.abort() } catch { } })
        .catch(() => { if (!settled && !ctrl2.signal.aborted) settle(null) })
    }, HEDGE_STAGGER_MS)
  })

  sessionCtrl.signal.addEventListener('abort', () => {
    try { ctrl1.abort() } catch { }
    try { ctrl2.abort() } catch { }
    settleRequest?.(null)
  }, { once: true })

  // 全局超时保护
  return Promise.race([
    racePromise,
    new Promise<ArrayBuffer | null>((resolve) => {
      timeoutTimer = window.setTimeout(() => {
        sessionCtrl.abort()
        resolve(null)
      }, timeout)
    }),
  ]).finally(() => {
    if (staggerTimer != null) window.clearTimeout(staggerTimer)
    if (timeoutTimer != null) window.clearTimeout(timeoutTimer)
    prefetchControllers.delete(sessionCtrl)
  })
}

// ====== IndexedDB 持久化 ======

const MAX_DISK_BYTES = 160 * 1024 * 1024
const DISK_TTL_MS = 2 * 24 * 60 * 60 * 1000
const DISK_PRUNE_DEBOUNCE_MS = 5_000
const diskCache = new SegmentDiskCache(MAX_DISK_BYTES, DISK_TTL_MS, DISK_PRUNE_DEBOUNCE_MS)
let diskQuotaWarned = false

async function diskSave(url: string, buf: ArrayBuffer, epKey?: string, range?: string | null): Promise<void> {
  try {
    // 与内存层用同一个归一化键：签名轮换时否则同一片段在磁盘上堆积成多份。
    await diskCache.save(segmentCacheKey(url, range), buf, epKey || currentEpKey || '')
  } catch (error: any) {
    if (error?.name === 'QuotaExceededError' || String(error?.message || '').includes('quota')) {
      if (!diskQuotaWarned) {
        diskQuotaWarned = true
        console.warn(`${LOG_PREFIX} browser disk-cache quota is full; clearing cache`)
      }
      void diskCache.clear().catch(() => {})
    }
  }
}

async function diskLoadForEpisode(epKey: string): Promise<number> {
  if (!epKey) return 0
  try {
    const entries = await diskCache.loadEpisode(epKey)
    if (currentEpKey !== epKey) return 0
    for (const entry of entries) cacheSet(entry.url, entry.data, epKey)
    if (entries.length > 0) {
      const mb = (totalCacheBytes / 1024 / 1024).toFixed(1)
      console.log(`${LOG_PREFIX} restored ${entries.length} segments (${mb} MB) for ${epKey}`)
    }
    return entries.length
  } catch {
    return 0
  }
}

async function diskPrune(): Promise<void> {
  try {
    await diskCache.prune()
  } catch {
    // Disk caching is opportunistic and must not interrupt playback.
  }
}

async function diskClear(): Promise<void> {
  try {
    await diskCache.clear()
  } catch {
    // Disk caching is opportunistic and must not interrupt playback.
  }
}

export async function diskCacheInfo(): Promise<DiskCacheInfo> {
  return diskCache.info()
}

async function diskCachePrune(maxBytes: number): Promise<number> {
  try {
    return await diskCache.prune(maxBytes)
  } catch {
    return 0
  }
}


// ====== hls.js v1.7.0-beta.1 统一 loader（TsCacheLoader）======
//
// 接口完全匹配 hls.js v1.7.0-beta.1 BaseLoader/FetchLoader：
//   - 构造器: new TsCacheLoader(config)
//   - this.stats 必须有完整的 LoadStats 结构（hls.js 会直接读写）
//   - load(context, config, callbacks)  入口
//   - abort() / destroy()
//
// callbacks 结构（hls.js 内部 FragmentLoader 传入）:
//   onSuccess(response, stats, context, networkDetails)
//     response = { url: string, data: ArrayBuffer|string|object, code: number }
//   onError(error, context, networkDetails, stats)
//   onAbort(stats, context, networkDetails)
//   onTimeout(stats, context, networkDetails)
//   onProgress(stats, context, data, networkDetails) ← 【可选】，不调用就不会崩
//
// context.type 取值:
//   "manifest" | "level" | "audioTrack" | "subtitleTrack" | "media-fragment" | "key" | ...
//
// 策略：
//   - "media-fragment" (TS 片段) → LRU 缓存 + fetch 后缓存
//   - "manifest"/"level" (m3u8) → 文本缓存 + 正常 fetch
//   - 其他类型 → 正常 fetch（交给浏览器/hls.js 默认逻辑）

class TsCacheLoader {
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
        _curEpStats.hits++
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
      _curEpStats.misses++
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
      const useHedge = !range && _networkMode === 'server_congested' && hedgeInFlight.size < 2
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

// ====== m3u8 文本级缓存（避免重复请求同一个 m3u8）
// 上限：最多保留 16 条（每个 m3u8 文本很小，但 URL 无限多，不加限会长期泄漏）。
// 用 Map 的插入有序特性做简易 LRU：命中时 delete+set 重新插入到末尾，超限时删头部最旧。
const M3U8_CACHE_MAX = 16
const m3u8TextCache = new Map<string, string>()
function getM3u8FromCache(url: string): string | null {
  const v = m3u8TextCache.get(url)
  if (v == null) return null
  // LRU：命中则移到末尾（最近使用）
  m3u8TextCache.delete(url)
  m3u8TextCache.set(url, v)
  return v
}
function setM3u8Cache(url: string, text: string): void {
  if (m3u8TextCache.has(url)) m3u8TextCache.delete(url)
  m3u8TextCache.set(url, text)
  // 超限淘汰最旧（Map 迭代顺序 = 插入顺序，第一个即最久未用）
  while (m3u8TextCache.size > M3U8_CACHE_MAX) {
    const oldest = m3u8TextCache.keys().next().value
    if (oldest === undefined) break
    m3u8TextCache.delete(oldest)
  }
}

// ====== 解析 m3u8 -> 片段 URL 列表 + targetduration
// 关键区分：
//   master playlist → 含 #EXT-X-STREAM-INF，列出的是 variant m3u8 URL（多码率）
//   media playlist → 含 #EXTINF，列出的是真正的 TS 片段 URL
interface M3u8ParseResult {
  urls: string[];          // media playlist: TS 片段 URL；master playlist: 空数组
  targetduration: number;
  text: string;
  isMaster: boolean;       // 是否为 master playlist（多码率）
  variantUrls: string[];   // master playlist 时的 variant m3u8 URL
  streamInfo: StreamVariantInfo[];  // master playlist 时每个 variant 的元数据
}

/** master playlist 中 #EXT-X-STREAM-INF 提取的 variant 元数据 */
interface StreamVariantInfo {
  bandwidth: number;       // 码率 (bps)
  resolution: string;      // 分辨率 e.g. "1920x1080"
  codecs: string;          // 编解码器 e.g. "avc1.640028,mp4a.40.2"
  url: string;             // variant URL
}

/** 广告域名黑名单 localStorage 键 */
const AD_BLACKLIST_STORAGE_KEY = 'cczj_ad_domain_blacklist'

/** 内置广告域名黑名单（匹配 hostname 子串） */
const _BUILTIN_AD_DOMAINS: string[] = [
  'dcs-vod.', 'vod-dcs.',
  'ads.', 'ad.', 'advert',
  'dsp.', 'doubleclick',
  'googlesyndication', 'googleads',
]

/** 当前生效的广告域名黑名单（内置 + 用户上报） */
let AD_DOMAIN_BLACKLIST: string[] = (() => {
  try {
    const userDomains = readStorage<string[] | null>(AD_BLACKLIST_STORAGE_KEY, null)
    if (userDomains) {
      return [..._BUILTIN_AD_DOMAINS, ...userDomains]
    }
  } catch {}
  return [..._BUILTIN_AD_DOMAINS]
})()

/** 用户上报的广告域名列表（不含内置域名） */
function _getUserAdDomains(): string[] {
  return AD_DOMAIN_BLACKLIST.filter(d => !_BUILTIN_AD_DOMAINS.includes(d))
}

/** 将域名加入广告黑名单（持久化到 localStorage） */
function addAdDomain(domain: string): boolean {
  if (!domain || AD_DOMAIN_BLACKLIST.includes(domain)) return false
  AD_DOMAIN_BLACKLIST.push(domain)
  try {
    writeStorage(AD_BLACKLIST_STORAGE_KEY, _getUserAdDomains())
  } catch {}
  return true
}

/** 获取当前所有广告黑名单域名 */
function getAdDomains(): string[] {
  return [...AD_DOMAIN_BLACKLIST]
}

/** 从 URL 字符串提取 hostname */
function _hostname(u: string): string {
  try { return new URL(u).hostname } catch { return '' }
}

/** 将 m3u8 中的片段路径解析为绝对 URL */
function _resolveSegUrl(seg: string, base: string): string {
  try { return new URL(seg, base).href } catch { return base + seg }
}

/**
 * 从 m3u8 文本中移除广告片段（双层过滤）
 * 第一层：域名黑名单 — .ts 片段域名命中黑名单则视为广告
 * 第二层（兜底）：DISCONTINUITY 分组，保留片段数最多的组（主内容）
 * @param text  m3u8 原始文本
 * @param m3u8Url m3u8 自身的 URL，用于解析相对路径
 */
function stripAdFromM3u8Text(text: string, m3u8Url: string): string {
  const base = m3u8Url.substring(0, m3u8Url.lastIndexOf('/') + 1)
  const lines = text.split('\n')

  // ── 第一层：域名黑名单过滤 ──
  // 先扫描所有 .ts 片段，统计黑名单命中比例
  const segLines: { idx: number; raw: string; absUrl: string }[] = []
  for (let i = 0; i < lines.length; i++) {
    const trimmed = lines[i].trim()
    if (!trimmed || trimmed.startsWith('#')) continue
    // 跳过 #EXT-X-* 之后的值行（如 #EXT-X-MAP 的 URI 等），只收集真正的片段行
    // 片段行不以 # 开头，且上一行通常是 #EXTINF 或 #EXT-X-BYTERANGE 等
    const absUrl = _resolveSegUrl(trimmed, base)
    segLines.push({ idx: i, raw: trimmed, absUrl })
  }

  // 如果黑名单能过滤掉部分片段（但不是全部），直接用黑名单
  if (segLines.length > 0) {
    const adIndices = new Set<number>()
    for (const s of segLines) {
      const host = _hostname(s.absUrl)
      if (host && AD_DOMAIN_BLACKLIST.some(d => host.includes(d))) {
        adIndices.add(s.idx)
      }
    }
    // 黑名单命中了部分片段（非全部）→ 移除广告片段及其关联标签
    if (adIndices.size > 0 && adIndices.size < segLines.length) {
      const removeLines = new Set<number>()
      for (const idx of adIndices) {
        removeLines.add(idx)
        // 向上移除该片段关联的 #EXTINF / #EXT-X-* 标签行
        for (let j = idx - 1; j >= 0; j--) {
          const t = lines[j].trim()
          if (!t) continue
          if (t.startsWith('#EXTINF') || t.startsWith('#EXT-X-BYTERANGE') ||
              t.startsWith('#EXT-X-PROGRAM-DATE-TIME') || t.startsWith('#EXT-X-MAP')) {
            removeLines.add(j)
            continue
          }
          break // 遇到 DISCONTINUITY 或其他非关联标签停止
        }
      }
      // 同时移除孤立的 #EXT-X-DISCONTINUITY（前后片段都被删了）
      const remaining = lines.filter((_, i) => !removeLines.has(i))
      return _cleanOrphanDiscontinuity(remaining).join('\n')
    }
    // 黑名单未命中任何片段 → 进入第二层
  }

  // ── 第二层（兜底）：DISCONTINUITY 分组，保守移除小组（广告） ──
  return _stripAdByDiscontinuityGroup(lines)
}

/** 广告片段最大数量阈值（广告一般 10~25s，片段 2~8 个，放宽到 12 兜底） */
const MAX_AD_SEGMENT_COUNT = 12
/** 广告最大总时长（秒）—— 25s 的广告组也能被识别 */
const MAX_AD_TOTAL_DURATION = 30

/**
 * DISCONTINUITY 分组兜底：保守策略
 * 只移除同时满足以下条件的小组：
 *   1. 片段数 ≤ MAX_AD_SEGMENT_COUNT
 *   2. 总时长 ≤ MAX_AD_TOTAL_DURATION
 *   3. 不是唯一的内容组（避免把所有内容当广告删掉）
 *   4. 【新增】时长占比兜底：若某组片段数远小于最大组（< 10%），且
 *      平均片段时长明显小于内容组（广告常 3.5s/片 vs 正片 4.0s/片），也判为广告
 * 其余所有组保留，组间用 #EXT-X-DISCONTINUITY 连接
 */
function _stripAdByDiscontinuityGroup(lines: string[]): string {
  interface Group { lines: string[]; segCount: number; totalDuration: number }
  const groups: Group[] = []
  let cur: Group = { lines: [], segCount: 0, totalDuration: 0 }

  const header: string[] = []
  const footer: string[] = []
  let inFooter = false

  for (const line of lines) {
    const trimmed = line.trim()

    // 收集头部（全局标签）
    if (cur.segCount === 0 && groups.length === 0 &&
        (trimmed.startsWith('#EXTM3U') || trimmed.startsWith('#EXT-X-VERSION') ||
         trimmed.startsWith('#EXT-X-TARGETDURATION') || trimmed.startsWith('#EXT-X-MEDIA-SEQUENCE') ||
         trimmed.startsWith('#EXT-X-PLAYLIST-TYPE') || trimmed.startsWith('#EXT-X-INDEPENDENT-SEGMENTS'))) {
      header.push(line)
      continue
    }

    // 尾部标签
    if (trimmed === '#EXT-X-ENDLIST') {
      inFooter = true
      footer.push(line)
      continue
    }
    if (inFooter) { footer.push(line); continue }

    if (trimmed === '#EXT-X-DISCONTINUITY') {
      groups.push(cur)
      cur = { lines: [], segCount: 0, totalDuration: 0 }
      continue
    }

    cur.lines.push(line)
    if (trimmed && !trimmed.startsWith('#')) {
      cur.segCount++
    } else {
      // 提取 #EXTINF 时长
      const m = trimmed.match(/^#EXTINF:([\d.]+)/)
      if (m) cur.totalDuration += parseFloat(m[1])
    }
  }
  groups.push(cur)

  // 无分组或只有一组 → 原样返回
  if (groups.length <= 1) {
    return [...header, ...(groups[0]?.lines || []), ...footer].join('\n')
  }

  // 计算内容组参考值：最大片段数、内容组平均片段时长
  const maxSegCount = Math.max(...groups.map(g => g.segCount))
  // 收集"大组"（片段数 > maxSegCount * 30%）的平均片段时长，作为正片参考
  const largeGroups = groups.filter(g => g.segCount > maxSegCount * 0.3 && g.totalDuration > 0)
  const contentAvgDuration = largeGroups.length > 0
    ? largeGroups.reduce((s, g) => s + g.totalDuration / g.segCount, 0) / largeGroups.length
    : 0

  // 判断哪些组是广告（小组）
  const isAdGroup = (g: Group): boolean => {
    if (g.segCount <= 0) return false
    // 基本阈值判定
    if (g.segCount <= MAX_AD_SEGMENT_COUNT && g.totalDuration > 0 && g.totalDuration <= MAX_AD_TOTAL_DURATION) {
      return true
    }
    // 【增强】时长占比兜底：片段数远小于最大组 + 平均片段时长明显偏短
    if (maxSegCount > 20 && g.segCount < maxSegCount * 0.1 && contentAvgDuration > 0) {
      const avgDur = g.totalDuration / g.segCount
      // 广告片段平均时长比正片短 10% 以上
      if (avgDur > 0 && avgDur < contentAvgDuration * 0.9) {
        return true
      }
    }
    return false
  }

  // 统计非广告组数量
  const contentGroups = groups.filter(g => !isAdGroup(g))

  // 如果所有内容都被判定为广告（不应该发生）→ 全部保留，不做任何删除
  if (contentGroups.length === 0) {
    const result: string[] = [...header]
    for (let i = 0; i < groups.length; i++) {
      if (i > 0) result.push('#EXT-X-DISCONTINUITY')
      result.push(...groups[i].lines)
    }
    result.push(...footer)
    return result.join('\n')
  }

  // 正常情况：只移除广告小组，保留其余所有组
  const result: string[] = [...header]
  let firstKept = true
  for (const g of groups) {
    if (isAdGroup(g)) continue // 跳过广告组
    if (!firstKept) result.push('#EXT-X-DISCONTINUITY')
    result.push(...g.lines)
    firstKept = false
  }
  result.push(...footer)
  return result.join('\n')
}

/** 清理孤立的 #EXT-X-DISCONTINUITY（前后无片段时移除） */
function _cleanOrphanDiscontinuity(lines: string[]): string[] {
  const result: string[] = []
  for (let i = 0; i < lines.length; i++) {
    if (lines[i].trim() === '#EXT-X-DISCONTINUITY') {
      // 检查后面是否紧跟有效片段（跳过空行和标签）
      let hasSegAfter = false
      for (let j = i + 1; j < lines.length; j++) {
        const t = lines[j].trim()
        if (!t) continue
        if (!t.startsWith('#')) { hasSegAfter = true; break }
        if (t === '#EXT-X-DISCONTINUITY') break
        if (t === '#EXT-X-ENDLIST') break
      }
      if (hasSegAfter) result.push(lines[i])
      // 否则丢弃（孤立 DISCONTINUITY）
    } else {
      result.push(lines[i])
    }
  }
  return result
}

function _parseM3u8Text(text: string, url: string): M3u8ParseResult {
  const base = url.substring(0, url.lastIndexOf('/') + 1)
  const urls: string[] = []
  const variantUrls: string[] = []
  const streamInfo: StreamVariantInfo[] = []
  let nextLineIsVariant = false
  let currentVariantMeta: { bandwidth: number; resolution: string; codecs: string } | null = null
  const hasStreamInf = /#EXT-X-STREAM-INF/.test(text)
  const hasExtInf = /#EXTINF/.test(text)
  const isMaster = hasStreamInf && !hasExtInf

  for (const line of text.split('\n')) {
    const trimmed = line.trim()
    if (!trimmed) continue
    if (trimmed.startsWith('#EXT-X-STREAM-INF')) {
      // 提取 BANDWIDTH, RESOLUTION, CODECS
      const bw = trimmed.match(/BANDWIDTH=(\d+)/)
      const res = trimmed.match(/RESOLUTION=([\dx]+)/i)
      const cod = trimmed.match(/CODECS="([^"]+)"/)
      currentVariantMeta = {
        bandwidth: bw ? parseInt(bw[1]) : 0,
        resolution: res ? res[1] : '',
        codecs: cod ? cod[1] : '',
      }
      nextLineIsVariant = true
      continue
    }
    if (trimmed.startsWith('#')) continue
    let absUrl: string
    try { absUrl = new URL(trimmed, base).href } catch { absUrl = base + trimmed }
    if (isMaster || nextLineIsVariant) {
      variantUrls.push(absUrl)
      if (currentVariantMeta) {
        streamInfo.push({ ...currentVariantMeta, url: absUrl })
      }
    } else {
      urls.push(absUrl)
    }
    nextLineIsVariant = false
    currentVariantMeta = null
  }

  const m = text.match(/#EXT-X-TARGETDURATION:(\d+)/)
  return { urls, variantUrls, targetduration: m ? parseInt(m[1]) : 6, text, isMaster, streamInfo }
}

async function fetchAndParseM3u8(url: string): Promise<M3u8ParseResult> {
  // 1) 文本缓存命中
  const cachedText = getM3u8FromCache(url)
  if (cachedText) {
    const clean = stripAdFromM3u8Text(cachedText, url)
    return _parseM3u8Text(clean, url)
  }

  // 2) 真正 fetch
  const resp = await fetch(url)
  if (!resp.ok) throw new Error('m3u8 fetch failed: ' + resp.status)
  const rawText = await resp.text()
  setM3u8Cache(url, rawText) // 缓存原始文本
  const clean = stripAdFromM3u8Text(rawText, url)
  return _parseM3u8Text(clean, url)
}

// 从已解析的片段 URL 直接开始预取（不需要再请求 m3u8 了）
function prefetchFromSegments(segUrls: string[], epIdx: number, startFrom: number = 0, count: number = 20): number {
  if (!enabled || segUrls.length === 0) return 0
  const ep = episodes[epIdx]
  const epKey = epKeyFrom(ep, epIdx)
  if (!epKey) return 0
  segmentsByEpisode.set(epKey, segUrls)
  if (currentEpIdx < 0) { currentEpIdx = epIdx; currentEpKey = epKey }
  const start = Math.max(0, startFrom)
  const end = Math.min(segUrls.length, start + Math.min(count, adaptivePrefetchCount()))
  let added = 0
  for (let i = end - 1; i >= start; i--) {
    if (enqueue({ url: segUrls[i], episodeKey: epKey, priority: 1 })) added++
  }
  if (added > 0) scheduleDrain()
  return added
}

export const TsCache = {
  enable, clear, stats,
  setEpisodes, setCurrentEpisode, setSegments, setTargetDuration, setPlaybackBufferAhead,
  prefetchFirst, prefetchNextEpisode, prefetchFromM3u8,
  notifyCurrentTs, notifyFragmentRequested,
  episodeProgress, getTotalEpisodes,
  onStateChange, removeListener: (cb: Listener) => listeners.delete(cb),
  diskClear, diskCacheInfo, diskCachePrune,
  dropVideoCache, dropSourceCache, dropAllSegmentCache,
  // ⭐ v1.7.0-beta.1 统一 loader：hls.js 新 API，一个 loader 处理所有请求类型
  //   用法: new Hls({ loader: TsCache.TsCacheLoader })
  TsCacheLoader,
  fetchAndParseM3u8, getM3u8FromCache, setM3u8Cache,
  prefetchFromSegments: prefetchFromSegments,
  // ⭐ v3: 网络诊断 + ABR 集成
  getNetworkMode,
  setAbrSwitchCallback,
  adaptiveConcurrency,
  adaptiveFragmentTimeout,
  // ⭐ 广告域名上报
  addAdDomain,
  getAdDomains,
}
