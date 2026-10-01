/**
 * 片段缓存子系统的共享可变状态 + 事件广播。
 *
 * 拆分文件时唯一必须集中放置的东西：内存 LRU、会话定位、统计样本、网络诊断状态
 * 都被多个模块读写（缓存层要查片段清单、预取层要写缓存、统计要读缓存字节数）。
 * 各处各自持有一份就是正确性 bug，所以这里定义一次、其余模块只引用同一个对象。
 * 本模块不 import 子系统内的任何其他模块 —— 依赖图因此不可能成环。
 */

// ====== 类型 ======

export interface EpisodeLite {
  source_key?: string
  vod_id: string | number
  ep_url: string
  ep_name?: string
  ep_num?: number
}

export interface FetchJob {
  url: string
  episodeKey: string      // 之前是 episodeIdx (number)，改字符串可跨视频区分
  priority: number
  generation?: number
}

export interface CacheEntry {
  buffer: ArrayBuffer
  size: number
  cacheKey: string        // 在 lruCache 中的键（= segmentCacheKey(原始 URL)，不是原始 URL）
  ts: number              // 最后一次访问时间（Date.now），淘汰只看这一个字段
  episodeKey: string      // 所属集稳定 key (空字符串 = 未知)
  fetchMs: number         // 当初真下载这些字节用了多久（0 = 未知），命中时据此回报耗时
}

export type NetworkMode = 'normal' | 'server_congested' | 'local_network_slow'

export interface EpisodeCounter { hits: number; misses: number }

export type Listener = () => void

// ====== LRU 内存缓存（状态）======

export const cache = {
  lruCache: new Map<string, CacheEntry>(),
  totalCacheBytes: 0,

  // 每集缓存的 cacheKey 集合：既把「该集是否超限」降成一次 size 查表，
  // 也让「找该集最久未读的片段」只遍历本集（≤ MAX_PER_EPISODE 条）而不是整张 lruCache。
  epCacheKeys: new Map<string, Set<string>>(),
}

// ====== 全局状态 ======

export const session = {
  enabled: false,
  episodes: [] as EpisodeLite[],
  currentEpIdx: -1,                    // 保留：当前播放"第几集（列表索引）"
  currentEpKey: '',                    // ⭐ 新增：当前播放集的稳定 key（vod_id+ep_num）
  targetDuration: 6,
  // 当前正在播放的 vod_id（字符串化）
  currentVodId: '' as string,
  // hls.js owns playback buffering. Background fetches start only once there is
  // sufficient media ahead, so they cannot compete with first play or recovery.
  playbackBufferAhead: 0,
  // 当前这批剧集所属的采集源：epKeyFrom 在没有 ep.source_key 时兜底用它
  currentSourceKey: '',
}

// ⭐ segmentsByEpisode 改以稳定 episodeKey 做 key，跨视频也不冲突
export const episodeIndex = {
  segmentsByEpisode: new Map<string, string[]>(),
  // 已播放片段追踪：key = episodeKey，value = 已播放 segment 索引集合
  playedSegmentsByEpisode: new Map<string, Set<number>>(),
}

export const prefetchState = {
  epQueueCount: new Map<string, number>(),
  // 去重键也走归一化：签名换了但内容没换的片段，不该被当成两个不同的下载任务。
  pendingUrls: new Set<string>(),
  queue: [] as FetchJob[],
  cacheSession: 0,
  inflightBySession: new Map<number, number>(),
  prefetchControllers: new Set<AbortController>(),
  debounceTimer: null as number | null,
}

// ====== 统计与速率样本 ======

// ⭐ 优化：按集数级统计（避免跨集数污染），替代单一全局 hits/misses
export const telemetry = {
  epStats: new Map<string, EpisodeCounter>(),
  recentFetchDurations: [] as number[],
  // 与 recentFetchDurations 一一对应的字节数：只有耗时算不出速率，也就无法判断命中该折算成多久
  recentFetchBytes: [] as number[],
  // 为当前集使用一个可变引用，减少每次查找
  curEpStats: { hits: 0, misses: 0 } as EpisodeCounter,
}

// ====== 网络诊断状态 ======

export const netDiag = {
  mode: 'normal' as NetworkMode,
  downloadAvgMs: 0,        // 滚动平均下载耗时 (ms)
  downloadVariance: 0,     // 下载耗时方差
  slowFetchCount: 0,       // 近期慢请求计数（>3s 计一次）
  fastFetchStreak: 0,      // 连续快请求计数（<1s 计一次）
  consecutiveSlowSegs: 0,  // 连续慢分片计数（触发 ABR 降级）
  abrSwitchCallback: null as ((targetLevel: number) => void) | null,
  normalCooldown: 0,         // 冷却倒计时：>0 时阻止切入 congested / local_network_slow
  congestedReentryCount: 0,  // 冷却结束后，连续确认 congested 的计数
  slowReentryCount: 0,       // 冷却结束后，连续确认 local_network_slow 的计数
  hedgeInFlight: new Set<string>(),  // 正在进行对冲请求的 URL
}

/**
 * 生成"来源+视频+集数"稳定唯一 key ——
 *   - 包含 source_key，切换采集源时不会误复用缓存
 *   - 跨视频即使 ep_num 相同也不同
 */
export function episodeKey(sourceKey: unknown, vodId: unknown, epNum: unknown): string {
  return `ep_${String(sourceKey ?? '')}_${String(vodId ?? '')}_${String(epNum ?? '')}`
}

export function epKeyFrom(ep: EpisodeLite | undefined | null, fallbackIdx?: number): string {
  if (!ep) return ''
  const sk = ep.source_key ?? session.currentSourceKey
  return episodeKey(sk, ep.vod_id, ep.ep_num ?? fallbackIdx ?? 0)
}

// ====== 事件系统 ======

const listeners = new Set<Listener>()

export function getListeners(): Set<Listener> { return listeners }

export function onStateChange(cb: Listener): () => void { listeners.add(cb); return () => listeners.delete(cb) }

let fireTimer: number | null = null
export function fireListeners(): void {
  if (fireTimer != null) return
  fireTimer = window.setTimeout(() => { fireTimer = null; for (const cb of listeners) { try { cb() } catch { /* ignore */ } } }, 250)
}
