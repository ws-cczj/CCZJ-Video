/**
 * m3u8 TS 分片预取 + 内存缓存 + IndexedDB 持久化
 *
 * 核心机制：
 *   1. fetch 拦截：透明拦截 hls.js 的 .ts 请求，命中 LRU 直接返回 ArrayBuffer
 *   2. 自适应预取：根据网速动态调整预取窗口大小和起始偏移
 *   3. 详情页预取：自行 fetch + 解析 m3u8，不等播放器
 *   4. IndexedDB 持久化：磁盘 LRU + 7 天 TTL
 */
/* eslint-disable no-console */

const LOG_PREFIX = '[TsCache]'

// ====== 可调参数 ======
// ⭐ 用户建议：不要粗暴清空，用"单集上限 + 全局上限 + 权重分配"的 LRU 调度
//   - 单集最多 64 片（超出就优先淘汰该集的旧片段）
//   - 全局最多 1024 片 / 512 MB（超出就按权重淘汰最老 / 最没用的）
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
}

interface CacheEntry {
  buffer: ArrayBuffer
  size: number
  url: string
  ts: number              // 最后一次访问时间（Date.now）
  segIdx: number          // 在所属集中的片段索引 (-1 表示未知)
  episodeKey: string      // 所属集稳定 key (空字符串 = 未知)
}

// ====== LRU 内存缓存 ======

const lruCache = new Map<string, CacheEntry>()
let totalCacheBytes = 0

// ⭐ 优化：维护每集缓存数量，避免每次 cacheSet/findOldestEntry 都 O(n) 遍历
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

function cacheGet(url: string): ArrayBuffer | null {
  const entry = lruCache.get(url)
  if (!entry) return null
  entry.ts = Date.now()
  return entry.buffer
}

function cacheSet(url: string, buf: ArrayBuffer): void {
  const size = buf.byteLength

  // ---- 1) 先算 segIdx 与所属 epKey（用于权重分配）----
  let segIdx = -1
  let epKey = ''
  if (currentEpKey) {
    const segs = segmentsByEpisode.get(currentEpKey) || []
    for (let i = 0; i < segs.length; i++) {
      if (segs[i] === url) { segIdx = i; epKey = currentEpKey; break }
    }
  }

  // 如果已经命中（同一 url 重写），先扣 size + 更新计数
  const prevEntry = lruCache.get(url)
  if (prevEntry) {
    totalCacheBytes -= prevEntry.size
    if (prevEntry.episodeKey) _decEpCount(prevEntry.episodeKey)
    lruCache.delete(url)
  }

  // ---- 2) 硬约束：单集最多 "min(MAX_PER_EPISODE, 实际片段数)" 片 ----
  if (epKey) {
    const totalSegsForEp = segmentsByEpisode.get(epKey)?.length ?? 0
    const hardLimit = Math.min(MAX_PER_EPISODE, totalSegsForEp > 0 ? totalSegsForEp : MAX_PER_EPISODE)
    const epCount = epCacheCount.get(epKey) || 0

    if (epCount >= hardLimit) {
      // 找该集最旧的条目淘汰（O(n) 遍历仍需要，但只在该集超限时）
      let oldestOfEp: CacheEntry | null = null
      for (const entry of lruCache.values()) {
        if (entry.episodeKey === epKey) {
          if (!oldestOfEp || entry.ts < oldestOfEp.ts) oldestOfEp = entry
        }
      }
      if (oldestOfEp) {
        totalCacheBytes -= oldestOfEp.size
        if (oldestOfEp.episodeKey) _decEpCount(oldestOfEp.episodeKey)
        lruCache.delete(oldestOfEp.url)
      }
    }
  }

  // ---- 3) 全局 LRU / 字节上限 ----
  while (lruCache.size >= MAX_CACHED_SEGMENTS || totalCacheBytes + size > MAX_CACHED_BYTES) {
    const oldest = findOldestEntry()
    if (!oldest) break
    totalCacheBytes -= oldest.size
    if (oldest.episodeKey) _decEpCount(oldest.episodeKey)
    lruCache.delete(oldest.url)
  }

  lruCache.set(url, { buffer: buf, size, url, ts: Date.now(), segIdx, episodeKey: epKey })
  totalCacheBytes += size
  if (epKey) _incEpCount(epKey)
}

function findOldestEntry(): CacheEntry | null {
  // 当前播放位置：从 playedSegmentsByEpisode 取最大已播放索引
  let currentPos = -1
  if (currentEpKey) {
    const playedSet = playedSegmentsByEpisode.get(currentEpKey)
    if (playedSet && playedSet.size > 0) {
      for (const idx of playedSet) if (idx > currentPos) currentPos = idx
    }
  }

  let worstEntry: CacheEntry | null = null
  let worstScore = -1

  for (const entry of lruCache.values()) {
    let score: number
    const hoursStale = Math.max(0, (Date.now() - entry.ts) / 3600000)
    const secondsStale = Math.max(0, (Date.now() - entry.ts) / 1000)

    // ==== 0) "该集已超单集上限" → 最高优先级淘汰 ====
    if (entry.episodeKey) {
      const totalSegsForEp = segmentsByEpisode.get(entry.episodeKey)?.length ?? MAX_PER_EPISODE
      const hardLimit = Math.min(MAX_PER_EPISODE, totalSegsForEp > 0 ? totalSegsForEp : MAX_PER_EPISODE)
      const ec = epCacheCount.get(entry.episodeKey) || 0
      if (ec > hardLimit) {
        score = 5000 + secondsStale
        if (score > worstScore) { worstScore = score; worstEntry = entry }
        continue
      }
    }

    // ==== 1) 其他集 → 按陈旧度递增淘汰 ====
    if (entry.episodeKey && entry.episodeKey !== currentEpKey) {
      score = 1000 + hoursStale * 200 + secondsStale
    }
    // ==== 2) 当前集：距离当前播放位置越近越保留 ====
    else if (entry.segIdx >= 0 && currentPos >= 0) {
      const distance = entry.segIdx - currentPos
      if (distance <= 0) score = 500 + Math.abs(distance) + hoursStale * 50
      else if (distance <= 30) score = 10 + hoursStale
      else if (distance <= 60) score = 50 + hoursStale * 2
      else if (distance <= 120) score = 150 + hoursStale * 3
      else score = 300 + (distance - 120) + hoursStale * 5
    } else {
      // segIdx 未知的条目按中等优先级处理
      score = 300 + hoursStale * 5 + secondsStale * 0.1
    }

    if (score > worstScore) { worstScore = score; worstEntry = entry }
  }
  return worstEntry
}

function cacheHas(url: string): boolean { return lruCache.has(url) }
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
let inflight = 0
let debounceTimer: number | null = null

// ⭐ 优化：按集数级统计（避免跨集数污染），替代单一全局 hits/misses
interface EpisodeCounter { hits: number; misses: number }
const epStats = new Map<string, EpisodeCounter>()
const recentFetchDurations: number[] = []
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

let originalFetch: typeof fetch | null = null

function isTsUrl(u: string): boolean {
  // 匹配 .ts (MPEG-TS) 或 .m4s (fMP4 segment)，忽略大小写
  return /\.(ts|m4s)(\?|$)/i.test(u)
}

function installFetchInterceptor(): void {
  if (originalFetch) return
  originalFetch = window.fetch
  let logCounter = 0

  window.fetch = function (input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
    const urlStr = typeof input === 'string' ? input
      : input instanceof URL ? input.href
        : input instanceof Request ? input.url : String(input)

    // 非 TS 片段：直接透传（m3u8、图片、API 等都走这里）
    if (!enabled || !isTsUrl(urlStr)) {
      return originalFetch!.call(window, input, init)
    }

    // === TS 片段请求：先走缓存 ===
    const cached = cacheGet(urlStr)
    if (cached) {
      _curEpStats.hits++
      fireListeners()
      logCounter++
      if (logCounter % 10 === 0) {
        const h = _curEpStats.hits, m = _curEpStats.misses
        const total = h + m
        const rate = total === 0 ? 0 : h / total
        console.log(`${LOG_PREFIX} 🎯 hit #${h} (${(rate * 100).toFixed(1)}%), cache ${lruCache.size} / ${(totalCacheBytes / 1024 / 1024).toFixed(1)} MB`)
      }
      return Promise.resolve(new Response(cached.slice(0), {
        status: 200, statusText: 'OK',
        headers: { 'Content-Type': 'video/mp2t', 'Content-Length': String(cached.byteLength), 'X-TsCache': 'hit' },
      }))
    }

    // === 未命中：fetch 但在完成后把数据存回缓存 ===
    _curEpStats.misses++
    fireListeners()
    logCounter++
    const missT0 = performance.now()  // ⭐ v3: 记录 miss 耗时用于网络诊断
    if (logCounter % 10 === 0) {
      const h = _curEpStats.hits, m = _curEpStats.misses
      const total = h + m
      const rate = total === 0 ? 0 : h / total
      console.log(`${LOG_PREFIX} 💫 miss #${m} (${(rate * 100).toFixed(1)}%), cache ${lruCache.size} / ${(totalCacheBytes / 1024 / 1024).toFixed(1)} MB`)
    }

    return originalFetch!.call(window, input, init).then((response) => {
      // 只缓存 200 OK 的 TS 片段
      if (!response.ok || response.status !== 200) return response

      // ⭐ 关键：clone() 一份 response 用来缓存，原始返回给 hls.js
      //   （Response.body 只能读一次，clone 后两边各读一份）
      const cloned = response.clone()
      cloned.arrayBuffer().then((buf) => {
        if (!enabled) return
        cacheSet(urlStr, buf)
        diskSave(urlStr, buf).catch(() => { })
        // ⭐ v3: 记录实际下载耗时用于网络诊断（不再是 0）
        recordFetchDuration(performance.now() - missT0)
        updateNetworkDiagnosis()
        fireListeners()
      }).catch(() => { /* clone 读取失败没关系，hls.js 还能拿到原始 response */ })
      return response
    })
  }
}

function uninstallFetchInterceptor(): void {
  if (originalFetch) { window.fetch = originalFetch; originalFetch = null }
}

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
    case 'server_congested': return 5     // 提高并发抢占带宽
    case 'local_network_slow': return 2   // 低并发避免加剧拥塞
    default: return 3
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
  epQueueCount.clear()
  pendingUrls.clear()
  queue.length = 0
  inflight = 0
  epStats.clear()  // ⭐ 集数级统计重置
  _curEpStats = { hits: 0, misses: 0 }
  recentFetchDurations.length = 0
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
  fireListeners()
  // ⭐ 注意：此处不再调用 diskLoad() —— 避免把其他视频的缓存全量加载到内存。
  //   改为在 setCurrentEpisode 中按需调用 diskLoadForEpisode(...)。
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
  if (currentEpKey) {
    if (!playedSegmentsByEpisode.has(currentEpKey)) {
      playedSegmentsByEpisode.set(currentEpKey, new Set<number>())
    }
    // ⭐ 从 epStats 中拿出该集的计数器（或新建）
    let cs = epStats.get(currentEpKey)
    if (!cs) { cs = { hits: 0, misses: 0 }; epStats.set(currentEpKey, cs) }
    _curEpStats = cs

    // ⭐ 按需从磁盘加载该集的缓存（只有当前集的片段才恢复到内存）
    const segUrls = segmentsByEpisode.get(currentEpKey) || undefined
    diskLoadForEpisode(currentEpKey, segUrls).catch(() => { })
  } else {
    _curEpStats = { hits: 0, misses: 0 }
  }
  fireListeners()
}

export function setTargetDuration(sec: number): void { if (sec > 0 && sec <= 30) targetDuration = sec }

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

  // ⭐ m3u8 解析完成：此时已拿到该集所有 segmentUrls，再做一次按需加载。
  //   对 episodeKey 字段的新数据—— setCurrentEpisode 中已匹配过。
  //   对老数据（只有 url 没有 episodeKey）—— 这里用 segmentUrls 白名单再次匹配。
  if (key === currentEpKey && list.length > 0) {
    diskLoadForEpisode(key, list).catch(() => { })
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
      if (cacheHas(u) || pendingUrls.has(u) || hedgeInFlight.has(u)) continue
      // 对冲：双请求取最快
      hedgeAdded++
      addedCount++
      hedgeInFlight.add(u)
      const t0 = performance.now()
      hedgeFetch(u).then((buf) => {
        if (buf) {
          cacheSet(u, buf)
          diskSave(u, buf, currentEpKey).catch(() => { })
          recordFetchDuration(performance.now() - t0)
          fireListeners()
        }
      }).finally(() => { hedgeInFlight.delete(u) })
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
    if (cacheHas(u) || pendingUrls.has(u) || hedgeInFlight.has(u)) continue
    if (enqueue({ url: u, episodeKey: currentEpKey, priority: 1 })) addedCount++
  }

  // --- 冷区：稀疏预取 (pos+16+, 按 spreadStep 间距) ---
  const coldStart = Math.max(16, warmEnd + 1)
  for (let offset = coldStart; addedCount < totalCount && pos + offset < segs.length; offset += spreadStep) {
    const idx = pos + offset
    if (playedSet.has(idx)) continue
    const u = segs[idx]
    if (cacheHas(u) || pendingUrls.has(u) || hedgeInFlight.has(u)) continue
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
          if (cacheHas(nextSegs[i]) || pendingUrls.has(nextSegs[i])) continue
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
export function enable(): void { enabled = true; installFetchInterceptor() }
export function disable(): void { enabled = false; uninstallFetchInterceptor() }
export function isEnabled(): boolean { return enabled }

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
  epQueueCount.clear()
  pendingUrls.clear()
  hedgeInFlight.clear()
  queue.length = 0
  inflight = 0
  epStats.clear()
  _curEpStats = { hits: 0, misses: 0 }
  recentFetchDurations.length = 0
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
  if (pendingUrls.has(job.url)) return false
  if (cacheHas(job.url)) return false
  const cnt = epQueueCount.get(job.episodeKey) || 0
  if (cnt >= MAX_QUEUE_PER_EPISODE) return false
  pendingUrls.add(job.url)
  epQueueCount.set(job.episodeKey, cnt + 1)
  if (job.priority === 1) queue.unshift(job); else queue.push(job)
  return true
}

function scheduleDrain(): void {
  if (debounceTimer != null) return
  debounceTimer = window.setTimeout(() => { debounceTimer = null; drainQueue() }, PREFETCH_DEBOUNCE_MS)
}

function drainQueue(): void {
  const maxConc = adaptiveConcurrency()
  while (inflight < maxConc && queue.length > 0) {
    const job = queue.shift(); if (!job) break
    inflight++
    // ⭐ 交错启动：每个分片间隔 ~1s，避免服务器敏感封禁
    if (inflight > 1) {
      const staggerDelay = (inflight - 1) * 1000
      window.setTimeout(() => {
        runOne(job).finally(() => { inflight--; drainQueue() })
      }, staggerDelay)
    } else {
      runOne(job).finally(() => { inflight--; drainQueue() })
    }
  }
}

async function runOne(job: FetchJob): Promise<void> {
  const t0 = performance.now()
  const timeout = adaptiveFragmentTimeout()
  try {
    const ctrl = new AbortController()
    const tid = window.setTimeout(() => ctrl.abort(), timeout)
    const init: RequestInit = { signal: ctrl.signal }
    try { (init as any).priority = 'low' } catch { /* ignore */ }
    const resp = originalFetch
      ? await originalFetch.call(window, job.url, init)
      : await fetch(job.url, init)
    window.clearTimeout(tid)
    if (resp.ok) {
      const buf = await resp.arrayBuffer()
      cacheSet(job.url, buf)
      diskSave(job.url, buf, job.episodeKey).catch(() => { })
      epQueueCount.set(job.episodeKey, (epQueueCount.get(job.episodeKey) || 0) - 1)
      const elapsed = performance.now() - t0
      recordFetchDuration(elapsed)
      updateNetworkDiagnosis()
      fireListeners()
    }
  } catch { /* 静默 */ }
  finally { pendingUrls.delete(job.url) }
}

function recordFetchDuration(ms: number): void {
  recentFetchDurations.push(ms)
  if (recentFetchDurations.length > SPEED_SAMPLE_COUNT) recentFetchDurations.shift()
}

// ====== 对冲请求（Hedge Fetch）======
//
// 对紧邻播放位置的关键分片开两个并发请求（同一文件），取最快返回的，另一个 abort。
// 能大幅降低尾部延迟，避免单个慢请求拖住整个播放流水线。
// 第二个请求延迟 HEDGE_STAGGER_MS 后发射，避免同时冲击服务器。

function hedgeFetch(url: string): Promise<ArrayBuffer | null> {
  const ctrl1 = new AbortController()
  const ctrl2 = new AbortController()
  const timeout = adaptiveFragmentTimeout()

  const doFetch = (signal: AbortSignal) => {
    const init: RequestInit = { signal }
    return originalFetch
      ? originalFetch.call(window, url, init)
      : fetch(url, init)
  }

  const racePromise = new Promise<ArrayBuffer | null>((resolve, reject) => {
    let settled = false
    const settle = (value: ArrayBuffer | null) => {
      if (!settled) { settled = true; resolve(value) }
    }

    // 请求1：立即发射
    doFetch(ctrl1.signal)
      .then((r) => r.ok ? r.arrayBuffer() : Promise.reject(new Error('http ' + r.status)))
      .then((buf) => { settle(buf); try { ctrl2.abort() } catch { } })
      .catch((err) => { if (!settled && !ctrl1.signal.aborted) reject(err) })

    // 请求2：延迟发射（错开避免服务器压力）
    window.setTimeout(() => {
      if (settled || ctrl2.signal.aborted) return
      doFetch(ctrl2.signal)
        .then((r) => r.ok ? r.arrayBuffer() : Promise.reject(new Error('http ' + r.status)))
        .then((buf) => { settle(buf); try { ctrl1.abort() } catch { } })
        .catch((err) => { if (!settled && !ctrl2.signal.aborted) reject(err) })
    }, HEDGE_STAGGER_MS)
  })

  // 全局超时保护
  return Promise.race([
    racePromise,
    new Promise<ArrayBuffer | null>((resolve) => {
      window.setTimeout(() => {
        try { ctrl1.abort() } catch { }
        try { ctrl2.abort() } catch { }
        resolve(null)
      }, timeout)
    }),
  ])
}

// ====== IndexedDB 持久化 ======

const DB_NAME = 'tscache', DB_VERSION = 1, STORE_NAME = 'segments'
const MAX_DISK_BYTES = 160 * 1024 * 1024   // 磁盘上限 160 MB（≈ 8 集 × 128 片 × 约 150 KB/片）
const DISK_TTL_MS = 2 * 24 * 3600 * 1000    // 2 天 TTL

function openDB(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const req = indexedDB.open(DB_NAME, DB_VERSION)
    req.onupgradeneeded = () => { req.result.createObjectStore(STORE_NAME, { keyPath: 'url' }) }
    req.onsuccess = () => resolve(req.result)
    req.onerror = () => reject(req.error)
  })
}

async function diskSave(url: string, buf: ArrayBuffer, epKey?: string): Promise<void> {
  try {
    const db = await openDB()
    const tx = db.transaction(STORE_NAME, 'readwrite')
    tx.objectStore(STORE_NAME).put({ url, data: buf, ts: Date.now(), episodeKey: epKey || currentEpKey || '' })
    await new Promise<void>((r, j) => { tx.oncomplete = () => r(); tx.onerror = () => j(tx.error) })
    db.close(); diskPrune()
  } catch (e: any) {
    if (e?.name === 'QuotaExceededError' || String(e?.message || '').includes('quota')) {
      if (!_diskQuotaWarned) { _diskQuotaWarned = true; console.warn('[TsCache] 磁盘缓存配额已满，自动清理中') }
      diskClear().catch(() => { })
    }
  }
}
let _diskQuotaWarned = false

/**
 * ⭐ 按需从磁盘加载指定集的缓存 —— 只把"与当前 epKey/segmentUrls 匹配"的分片恢复到内存。
 *
 * 匹配策略（任一满足即可）：
 *   1) episodeKey === epKey                        —— 新数据（带 episodeKey 字段）
 *   2) url in segmentUrls                           —— 老数据兼容（通过 URL 匹配该集片段列表）
 *
 * 避免把其他视频的 650 片全部加载到内存。
 */
async function diskLoadForEpisode(epKey: string, segmentUrls?: string[]): Promise<number> {
  if (!epKey && (!segmentUrls || segmentUrls.length === 0)) return 0
  try {
    const db = await openDB()
    const tx = db.transaction(STORE_NAME, 'readonly')
    const store = tx.objectStore(STORE_NAME); const req = store.getAll()
    await new Promise<void>((r, j) => { tx.oncomplete = () => r(); tx.onerror = () => j(tx.error) })
    const entries: Array<{ url: string; data: ArrayBuffer; ts: number; episodeKey?: string }> = req.result || []
    db.close()
    const now = Date.now(); let loaded = 0
    const urlSet = segmentUrls && segmentUrls.length > 0 ? new Set(segmentUrls) : null
    for (const e of entries) {
      if (now - e.ts > DISK_TTL_MS) continue
      if (!e.data || e.data.byteLength <= 0) continue
      // epKey 匹配（含 source_key）
      if (epKey && e.episodeKey && e.episodeKey === epKey) { cacheSet(e.url, e.data); loaded++; continue }
      // 老数据 fallback：通过 URL 白名单匹配
      if (urlSet && urlSet.has(e.url)) { cacheSet(e.url, e.data); loaded++; continue }
    }
    if (loaded > 0) {
      const mb = (totalCacheBytes / 1024 / 1024).toFixed(1)
      console.log(`${LOG_PREFIX} 💾 按需恢复 ${loaded} 片 (${mb} MB) · ep=${epKey || 'url-match'}`)
    }
    return loaded
  } catch { return 0 }
}

/** 全量加载 —— 仅供 diskCacheInfo/调试使用，不在日常播放流程中调用 */
async function diskLoadAll(): Promise<number> {
  try {
    const db = await openDB()
    const tx = db.transaction(STORE_NAME, 'readonly')
    const store = tx.objectStore(STORE_NAME); const req = store.getAll()
    await new Promise<void>((r, j) => { tx.oncomplete = () => r(); tx.onerror = () => j(tx.error) })
    const entries: Array<{ url: string; data: ArrayBuffer; ts: number }> = req.result || []
    db.close()
    const now = Date.now(); let loaded = 0
    for (const e of entries) {
      if (now - e.ts > DISK_TTL_MS) continue
      if (e.data && e.data.byteLength > 0) { cacheSet(e.url, e.data); loaded++ }
    }
    if (loaded > 0) console.log(`${LOG_PREFIX} 💾 全量恢复 ${loaded} 片 (${(totalCacheBytes / 1024 / 1024).toFixed(1)} MB)`)
    return loaded
  } catch { return 0 }
}

// 保留旧名，便于 diskLoad() 仍可被调用（但走按需逻辑而非全量）
async function diskLoad(): Promise<void> {
  // 不做任何事 —— 避免被意外调用时把所有缓存全量加载
  // 需要恢复缓存请显式调用 diskLoadForEpisode(epKey, segmentUrls)
}

async function diskPrune(): Promise<void> {
  try {
    const db = await openDB()
    const tx = db.transaction(STORE_NAME, 'readwrite')
    const store = tx.objectStore(STORE_NAME); const req = store.getAll()
    await new Promise<void>((r, j) => { tx.oncomplete = () => r(); tx.onerror = () => j(tx.error) })
    const entries: Array<{ url: string; data: ArrayBuffer; ts: number }> = req.result || []
    const now = Date.now(); let totalBytes = 0
    entries.sort((a, b) => b.ts - a.ts); const toDelete: string[] = []
    for (const e of entries) {
      if (now - e.ts > DISK_TTL_MS) { toDelete.push(e.url); continue }
      totalBytes += (e.data?.byteLength || 0)
      if (totalBytes > MAX_DISK_BYTES) toDelete.push(e.url)
    }
    if (toDelete.length > 0) {
      const delTx = db.transaction(STORE_NAME, 'readwrite')
      const delStore = delTx.objectStore(STORE_NAME)
      for (const url of toDelete) delStore.delete(url)
      await new Promise<void>((r, j) => { delTx.oncomplete = () => r(); delTx.onerror = () => j(delTx.error) })
    }
    db.close()
  } catch { /* ignore */ }
}

async function diskClear(): Promise<void> {
  try { const db = await openDB(); db.transaction(STORE_NAME, 'readwrite').objectStore(STORE_NAME).clear(); db.close() } catch { /* ignore */ }
}

// ====== 磁盘缓存：位置与统计信息 ======
//
// 存储位置：浏览器 IndexedDB
//   - 数据库名: "tscache"
//   - 对象仓库: "segments"
//   - key: url (字符串)
//   - value: { url: string, data: ArrayBuffer, ts: number }
//
// 用户查看/清理方式：
//   1) Chrome/Edge: F12 → Application → Storage → IndexedDB → tscache → segments
//   2) Firefox: F12 → 存储 → IndexedDB → tscache
//   3) 代码: TsCache.diskCacheInfo() → { count, bytes, dbName, storeName }
//   4) 代码: TsCache.diskCacheClear() → 清空
//   5) 代码: TsCache.diskCachePrune(maxBytes) → 只保留最近 maxBytes 大小

export async function diskCacheInfo(): Promise<{ dbName: string; storeName: string; count: number; bytes: number; ttlDays: number; }> {
  try {
    const db = await openDB()
    const tx = db.transaction(STORE_NAME, 'readonly')
    const store = tx.objectStore(STORE_NAME)
    const req = store.getAll()
    await new Promise<void>((r, j) => { tx.oncomplete = () => r(); tx.onerror = () => j(tx.error) })
    const entries: Array<{ url: string; data: ArrayBuffer; ts: number }> = req.result || []
    db.close()
    let bytes = 0; for (const e of entries) bytes += (e.data?.byteLength || 0)
    return { dbName: DB_NAME, storeName: STORE_NAME, count: entries.length, bytes, ttlDays: DISK_TTL_MS / (24 * 3600 * 1000) }
  } catch { return { dbName: DB_NAME, storeName: STORE_NAME, count: 0, bytes: 0, ttlDays: DISK_TTL_MS / (24 * 3600 * 1000) } }
}

async function diskCachePrune(maxBytes: number): Promise<number> {
  try {
    const db = await openDB()
    const tx = db.transaction(STORE_NAME, 'readwrite')
    const store = tx.objectStore(STORE_NAME)
    const req = store.getAll()
    await new Promise<void>((r, j) => { tx.oncomplete = () => r(); tx.onerror = () => j(tx.error) })
    const entries: Array<{ url: string; data: ArrayBuffer; ts: number }> = req.result || []
    entries.sort((a, b) => b.ts - a.ts)
    let kept = 0; let totalSize = 0; const toDelete: string[] = []
    for (const e of entries) {
      const sz = e.data?.byteLength || 0
      if (totalSize + sz > maxBytes) toDelete.push(e.url)
      else { totalSize += sz; kept++ }
    }
    if (toDelete.length > 0) {
      const delTx = db.transaction(STORE_NAME, 'readwrite')
      const delStore = delTx.objectStore(STORE_NAME)
      for (const u of toDelete) delStore.delete(u)
      await new Promise<void>((r, j) => { delTx.oncomplete = () => r(); delTx.onerror = () => j(delTx.error) })
    }
    db.close()
    return toDelete.length
  } catch { return 0 }
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
      const cached = cacheGet(url)
      if (cached) {
        _curEpStats.hits++
        fireListeners()
        // 异步回调 onSuccess（模拟 fetch 的 async 行为）
        Promise.resolve().then(() => {
          if (this._destroyed) return
          const now = performance.now()
          this.stats.loading.first = Math.max(now, this.stats.loading.start)
          this.stats.loading.end = Math.max(this.stats.loading.first, now)
          this.stats.loaded = this.stats.total = cached.byteLength
          // 缓存命中：极高带宽估计值，hls.js ABR 会选择较高码率
          this.stats.bwEstimate = Math.round((cached.byteLength * 8 * 1000) / Math.max(1, now - this.stats.loading.start))
          this.stats.chunkCount = 1
          if (callbacks?.onSuccess) {
            callbacks.onSuccess(
              { url, data: cached.slice(0), code: 200 },
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

      const doFetch = () => {
        if (originalFetch) return originalFetch.call(window, url, { signal: ctrl.signal })
        return fetch(url, { signal: ctrl.signal })
      }

      // ⭐ v3: 根据网络诊断决定使用对冲请求还是普通请求
      const useHedge = _networkMode === 'server_congested' && hedgeInFlight.size < 2
      const fetchPromise = useHedge
        ? hedgeFetch(url).then((buf) => buf ? { buf, hedged: true as const, resp: null as Response | null } : Promise.reject(new Error('hedge timeout')))
        : doFetch().then(async (resp) => {
          if (this._destroyed || ctrl.signal.aborted) throw new Error('aborted')
          if (!resp.ok) throw new Error('HTTP ' + resp.status)
          const buf = await resp.arrayBuffer()
          return { buf, hedged: false as const, resp: resp as Response | null }
        })

      fetchPromise
        .then(({ buf, hedged, resp }) => {
          if (this._destroyed) return
          // 写入缓存
          cacheSet(url, buf)
          diskSave(url, buf).catch(() => { })
          const elapsed = performance.now() - t0
          recordFetchDuration(elapsed)
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
      const cachedText = m3u8TextCache.get(url)
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
          this.stats.bwEstimate = 100000000
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
      const doFetch2 = () => {
        if (originalFetch) return originalFetch.call(window, url, { signal: ctrl.signal })
        return fetch(url, { signal: ctrl.signal })
      }
      doFetch2()
        .then(async (resp) => {
          if (this._destroyed || ctrl.signal.aborted) return
          if (!resp.ok) throw new Error('HTTP ' + resp.status)
          const rawText = await resp.text()
          if (this._destroyed) return
          m3u8TextCache.set(url, rawText) // 缓存原始文本
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
    const doFetch3 = () => {
      if (originalFetch) return originalFetch.call(window, url, { signal: ctrl.signal })
      return fetch(url, { signal: ctrl.signal })
    }
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
    const saved = localStorage.getItem(AD_BLACKLIST_STORAGE_KEY)
    if (saved) {
      const userDomains: string[] = JSON.parse(saved)
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
    localStorage.setItem(AD_BLACKLIST_STORAGE_KEY, JSON.stringify(_getUserAdDomains()))
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
  enable, disable, isEnabled, clear, stats,
  setEpisodes, setCurrentEpisode, setSegments, setTargetDuration,
  prefetchFirst, prefetchNextEpisode, prefetchFromM3u8,
  notifyCurrentTs, notifyFragmentRequested,
  episodeProgress, getTotalEpisodes,
  onStateChange, removeListener: (cb: Listener) => listeners.delete(cb),
  diskLoad, diskClear, diskCacheInfo, diskCachePrune,
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
