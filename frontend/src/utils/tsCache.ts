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
//   - 单集最多 128 片（超出就优先淘汰该集的旧片段）
//   - 全局最多 1024 片 / 256 MB（超出就按权重淘汰最老 / 最没用的）
//   - 这样切换到已看过的集，缓存仍能命中；而长期不用的片段会自然被淘汰
const DEFAULT_PREFETCH_SECONDS = 60
const MIN_PREFETCH_COUNT = 4
const MAX_PREFETCH_COUNT = 20
const PREFETCH_TRIGGER_WHEN_LESS_THAN = 12
const PREFETCH_AHEAD_NEXT_EPISODE = 5
const FETCH_TIMEOUT_MS = 8000
const PREFETCH_DEBOUNCE_MS = 300
const MAX_CONCURRENT_FETCH = 4
const MAX_QUEUE_PER_EPISODE = 30
const MAX_CACHED_SEGMENTS = 1024          // 全局 LRU 上限：1024 片
const MAX_CACHED_BYTES = 256 * 1024 * 1024 // 全局上限：256 MB
const MAX_PER_EPISODE = 128                // ⭐ 单集上限：128 片（硬约束）
const SPEED_SAMPLE_COUNT = 8

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

/** 旧版 key（无 source_key），用于 IndexedDB 历史数据兼容 */
function legacyEpisodeKey(vodId: unknown, epNum: unknown): string {
  return `ep_${String(vodId ?? '')}_${String(epNum ?? '')}`
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
        recordFetchDuration(0)  // 0 表示非预取请求，不影响网速自适应
        fireListeners()
      }).catch(() => { /* clone 读取失败没关系，hls.js 还能拿到原始 response */ })
      return response
    })
  }
}

function uninstallFetchInterceptor(): void {
  if (originalFetch) { window.fetch = originalFetch; originalFetch = null }
}

// ====== 自适应预取 ======
//
// 核心原则（v2 - 非连续分散策略）：
//   1. 网速越慢，预取的起始偏移越大（给预取留更多时间）
//   2. 网速越慢，预取片段之间的间距越大（覆盖更远距离）
//   3. 只预取未播放的片段（已播放的缓存意义不大）
//   4. 不连续预取（避免与 hls.js 的顺序拉取竞争同一批数据）
//
// 示意（pos = 当前播放片段位置）：
//   慢速网络:  pos ... [预取1] ... [预取2] ... [预取3] ... （间距大，起点远）
//   中速网络:  pos . [预取1] .. [预取2] ... [预取3] ...   （间距中）
//   快速网络:  pos [预取1][预取2][预取3] ...                （连续也可，因为下载很快）

function adaptivePrefetchCount(): number {
  // 预取总量：网速慢时略多一点，但绝不超过 MAX_PREFETCH_COUNT
  const base = Math.ceil(DEFAULT_PREFETCH_SECONDS / Math.max(targetDuration, 1))
  if (recentFetchDurations.length < 3) return clamp(base, MIN_PREFETCH_COUNT, MAX_PREFETCH_COUNT)
  const avg = recentFetchDurations.reduce((a, b) => a + b, 0) / recentFetchDurations.length
  if (avg > 5000) return clamp(base + 8, MIN_PREFETCH_COUNT, MAX_PREFETCH_COUNT)
  if (avg > 3000) return clamp(base + 6, MIN_PREFETCH_COUNT, MAX_PREFETCH_COUNT)
  if (avg > 1500) return clamp(base + 4, MIN_PREFETCH_COUNT, MAX_PREFETCH_COUNT)
  if (avg < 500) return clamp(base - 2, MIN_PREFETCH_COUNT, MAX_PREFETCH_COUNT)
  return clamp(base, MIN_PREFETCH_COUNT, MAX_PREFETCH_COUNT)
}

/**
 * 起始偏移：从当前播放位置往后第几片开始预取。
 * 逻辑：网速越慢，offset 越大 —— 让 hls.js 自己处理紧接的几片，
 * 我们的预取专注于更远的位置，这样才能保证"到时候已经下好了"。
 */
function adaptiveBufferOffset(): number {
  if (recentFetchDurations.length < 3) return 8
  const avg = recentFetchDurations.reduce((a, b) => a + b, 0) / recentFetchDurations.length
  // 单片下载时间 / 片时长 = 下载一片需要多少"播放时间"
  // 比例 > 1 意味着下载比播放慢，必须把预取起点推远
  const ratio = avg / (targetDuration * 1000)
  if (avg > 5000 || ratio > 1.5) return 20   // 很慢：起点推到 20 片后
  if (avg > 3000 || ratio > 1.0) return 15   // 较慢：起点推到 15 片后
  if (avg > 1500 || ratio > 0.5) return 12   // 中等：起点推到 12 片后
  if (avg < 500) return 8                    // 很快：起点可以近一点
  return 10
}

/**
 * 预取间距：相邻两个预取片段之间隔几片。
 * 逻辑：网速越慢，间距越大，用"稀疏但广覆盖"换取更高的命中概率。
 *   step=1 → 连续预取 [pos+offset, pos+offset+1, pos+offset+2, ...]
 *   step=2 → 隔一片预取 [pos+offset, pos+offset+2, pos+offset+4, ...]
 *   step=3 → 隔两片预取 [pos+offset, pos+offset+3, pos+offset+6, ...]
 */
function adaptiveSpreadStep(): number {
  if (recentFetchDurations.length < 3) return 2
  const avg = recentFetchDurations.reduce((a, b) => a + b, 0) / recentFetchDurations.length
  const ratio = avg / (targetDuration * 1000)
  if (avg > 5000 || ratio > 1.5) return 4   // 很慢：大片间距，广撒网
  if (avg > 3000 || ratio > 1.0) return 3   // 较慢：中等间距
  if (avg > 1500 || ratio > 0.5) return 2   // 中等：较小间距
  if (avg < 300) return 1                    // 极快：连续预取也不怕
  return 2
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
    const legacyKey = ep ? legacyEpisodeKey(ep.vod_id, ep.ep_num ?? idx) : ''
    diskLoadForEpisode(currentEpKey, segUrls, legacyKey).catch(() => { })
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

  // 2. 预取参数：上限同时考虑 "adaptivePrefetchCount()" 与"剩余未播放片段数"，避免重复
  const totalActual = segs.length
  const remaining = totalActual - pos - 1
  const prefetchCount = Math.min(adaptivePrefetchCount(), remaining)
  const startOffset = adaptiveBufferOffset()
  const spreadStep = adaptiveSpreadStep()

  // 3. 非连续预取：按 spreadStep 间距跳跃；已有缓存的不拉
  let addedCount = 0
  let tried = 0
  const maxTries = prefetchCount * spreadStep * 2

  for (let offset = startOffset; tried < maxTries && addedCount < prefetchCount; offset += spreadStep) {
    const idx = pos + offset
    if (idx >= segs.length) break
    if (playedSet.has(idx)) { tried++; continue }
    const u = segs[idx]
    if (cacheHas(u) || pendingUrls.has(u)) { tried++; continue }
    if (enqueue({ url: u, episodeKey: currentEpKey, priority: 1 })) addedCount++
    tried++
  }

  if (pos % 5 === 0) {
    const h = _curEpStats.hits, m = _curEpStats.misses
    const total = h + m
    const rate = total === 0 ? 0 : h / total
    console.log(
      `${LOG_PREFIX} pos=${pos}/${totalActual}, prefetch=${addedCount}, ` +
      `命中率=${(rate * 100).toFixed(1)}%`
    )
  }

  // 4. 下一集预取：进度到 30% 或剩余 ≤12 片时触发
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
  // ⭐ 当前集已缓存的片段数（不是所有 LRU 条目）
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
  queue.length = 0
  inflight = 0
  epStats.clear()
  _curEpStats = { hits: 0, misses: 0 }
  recentFetchDurations.length = 0
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
  while (inflight < MAX_CONCURRENT_FETCH && queue.length > 0) {
    const job = queue.shift(); if (!job) break
    inflight++; runOne(job).finally(() => { inflight--; drainQueue() })
  }
}

async function runOne(job: FetchJob): Promise<void> {
  const t0 = performance.now()
  try {
    const ctrl = new AbortController()
    const tid = window.setTimeout(() => ctrl.abort(), FETCH_TIMEOUT_MS)
    const init: RequestInit = { signal: ctrl.signal }
    try { (init as any).priority = 'low' } catch { /* ignore */ }
    const resp = originalFetch
      ? await originalFetch.call(window, job.url, init)
      : await fetch(job.url, init)
    window.clearTimeout(tid)
    if (resp.ok) {
      const buf = await resp.arrayBuffer()
      cacheSet(job.url, buf)
      diskSave(job.url, buf).catch(() => { })
      epQueueCount.set(job.episodeKey, (epQueueCount.get(job.episodeKey) || 0) - 1)
      recordFetchDuration(performance.now() - t0)
      fireListeners()
    }
  } catch { /* 静默 */ }
  finally { pendingUrls.delete(job.url) }
}

function recordFetchDuration(ms: number): void {
  recentFetchDurations.push(ms)
  if (recentFetchDurations.length > SPEED_SAMPLE_COUNT) recentFetchDurations.shift()
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
  } catch { /* ignore */ }
}

/**
 * ⭐ 按需从磁盘加载指定集的缓存 —— 只把"与当前 epKey/segmentUrls 匹配"的分片恢复到内存。
 *
 * 匹配策略（任一满足即可）：
 *   1) episodeKey === epKey                        —— 新数据（带 episodeKey 字段）
 *   2) url in segmentUrls                           —— 老数据兼容（通过 URL 匹配该集片段列表）
 *
 * 避免把其他视频的 650 片全部加载到内存。
 */
async function diskLoadForEpisode(epKey: string, segmentUrls?: string[], legacyKey?: string): Promise<number> {
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
      // epKey 匹配（新格式含 source_key，或旧格式兼容）
      if (epKey && e.episodeKey && e.episodeKey === epKey) { cacheSet(e.url, e.data); loaded++; continue }
      if (legacyKey && e.episodeKey && e.episodeKey === legacyKey) { cacheSet(e.url, e.data); loaded++; continue }
      // 老数据：通过 URL 白名单匹配
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

async function diskCacheInfo(): Promise<{ dbName: string; storeName: string; count: number; bytes: number; ttlDays: number; }> {
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

      const doFetch = () => {
        if (originalFetch) return originalFetch.call(window, url, { signal: ctrl.signal })
        return fetch(url, { signal: ctrl.signal })
      }

      doFetch()
        .then(async (resp) => {
          if (this._destroyed || ctrl.signal.aborted) return
          if (!resp.ok) throw new Error('HTTP ' + resp.status)
          const buf = await resp.arrayBuffer()
          if (this._destroyed) return
          // 写入缓存
          cacheSet(url, buf)
          diskSave(url, buf).catch(() => { })
          recordFetchDuration(performance.now() - this.stats.loading.start)
          fireListeners()
          // 统计
          const now = performance.now()
          this.stats.loading.first = Math.max(this.stats.loading.start + 1, this.stats.loading.start)
          this.stats.loading.end = Math.max(this.stats.loading.first, now)
          this.stats.loaded = this.stats.total = buf.byteLength
          this.stats.bwEstimate = Math.round((buf.byteLength * 8 * 1000) / Math.max(1, now - this.stats.loading.start))
          this.stats.chunkCount = 1
          if (callbacks?.onSuccess) {
            callbacks.onSuccess({ url, data: buf, code: 200 }, this.stats, context, resp)
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
        // 异步回传缓存文本
        Promise.resolve().then(() => {
          if (this._destroyed) return
          const now = performance.now()
          this.stats.loading.first = Math.max(now, this.stats.loading.start)
          this.stats.loading.end = Math.max(this.stats.loading.first, now)
          this.stats.loaded = this.stats.total = cachedText.length
          this.stats.bwEstimate = 100000000
          this.stats.chunkCount = 1
          if (callbacks?.onSuccess) {
            callbacks.onSuccess(
              { url, data: cachedText, code: 200 },
              this.stats,
              context,
              null
            )
          }
        })
        return
      }

      // 未命中 → 原生 fetch 并缓存文本
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
          const text = await resp.text()
          if (this._destroyed) return
          m3u8TextCache.set(url, text)
          const now = performance.now()
          this.stats.loading.first = Math.max(this.stats.loading.start + 1, this.stats.loading.start)
          this.stats.loading.end = Math.max(this.stats.loading.first, now)
          this.stats.loaded = this.stats.total = text.length
          this.stats.bwEstimate = Math.round((text.length * 8 * 1000) / Math.max(1, now - this.stats.loading.start))
          this.stats.chunkCount = 1
          if (callbacks?.onSuccess) {
            callbacks.onSuccess({ url, data: text, code: 200 }, this.stats, context, resp)
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
  }

  destroy() {
    this._destroyed = true
    try { this._abort?.abort() } catch { }
  }
}

// ====== m3u8 文本级缓存（避免重复请求同一个 m3u8）
const m3u8TextCache = new Map<string, string>()
function getM3u8FromCache(url: string): string | null { return m3u8TextCache.get(url) || null }
function setM3u8Cache(url: string, text: string): void { m3u8TextCache.set(url, text) }

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
}

function _parseM3u8Text(text: string, url: string): M3u8ParseResult {
  const base = url.substring(0, url.lastIndexOf('/') + 1)
  const urls: string[] = []
  const variantUrls: string[] = []
  let nextLineIsVariant = false
  // ⭐ 更严谨的检测：仅当同时存在 #EXT-X-STREAM-INF 且 无 #EXTINF 时才算 master
  const hasStreamInf = /#EXT-X-STREAM-INF/.test(text)
  const hasExtInf = /#EXTINF/.test(text)
  const isMaster = hasStreamInf && !hasExtInf

  for (const line of text.split('\n')) {
    const trimmed = line.trim()
    if (!trimmed) continue
    if (trimmed.startsWith('#EXT-X-STREAM-INF')) {
      nextLineIsVariant = true
      continue
    }
    if (trimmed.startsWith('#')) continue
    // 非 # 行：URL
    let absUrl: string
    try { absUrl = new URL(trimmed, base).href } catch { absUrl = base + trimmed }
    // master 时所有非 # 行都是 variant m3u8；否则都是 TS
    if (isMaster || nextLineIsVariant) {
      variantUrls.push(absUrl)
    } else {
      urls.push(absUrl)
    }
    nextLineIsVariant = false
  }

  const m = text.match(/#EXT-X-TARGETDURATION:(\d+)/)
  return { urls, variantUrls, targetduration: m ? parseInt(m[1]) : 6, text, isMaster }
}

async function fetchAndParseM3u8(url: string): Promise<M3u8ParseResult> {
  // 1) 文本缓存命中
  const cachedText = getM3u8FromCache(url)
  if (cachedText) return _parseM3u8Text(cachedText, url)

  // 2) 真正 fetch
  const resp = await fetch(url)
  if (!resp.ok) throw new Error('m3u8 fetch failed: ' + resp.status)
  const text = await resp.text()
  setM3u8Cache(url, text)
  return _parseM3u8Text(text, url)
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
}
