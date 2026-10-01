/**
 * m3u8 TS 分片预取 + 内存缓存 + IndexedDB 持久化
 *
 * 核心机制：
 *   1. fetch 拦截：透明拦截 hls.js 的 .ts 请求，命中 LRU 直接返回 ArrayBuffer
 *   2. 自适应预取：根据网速动态调整预取窗口大小和起始偏移
 *   3. 详情页预取：自行 fetch + 解析 m3u8，不等播放器
 *   4. IndexedDB 持久化：磁盘 LRU + 2 天 TTL
 *
 * 实现按职责拆在 ./ts-cache/ 下：内存 LRU（memory-cache）、预取与取回
 * （prefetch）、网络诊断与自适应策略（network）、统计（telemetry）、
 * m3u8 解析与去广告（m3u8 / ad-filter）、落盘（disk-cache）、播放器 loader
 * （loader）、对外门面（api）。共享可变状态只有 store.ts 一份。
 *
 * 本文件只做转发：导入路径 `../utils/tsCache` 保持不变，导出名单与拆分前逐一对应。
 */
export { segmentCacheKey } from './ts-cache/segment-key'
export { onStateChange } from './ts-cache/store'
export { diskCacheInfo } from './ts-cache/disk-cache'
export { notifyFragmentRequested } from './ts-cache/telemetry'
export { prefetchFirst } from './ts-cache/prefetch'
export { prefetchNextEpisode } from './ts-cache/prefetch'
export { prefetchFromM3u8 } from './ts-cache/prefetch'
export { notifyCurrentTs } from './ts-cache/prefetch'
export { setPlaybackBufferAhead } from './ts-cache/prefetch'
export { TsCache } from './ts-cache/api'
export { enable } from './ts-cache/api'
export { clear } from './ts-cache/api'
export { stats } from './ts-cache/api'
export { episodeProgress } from './ts-cache/api'
export { getTotalEpisodes } from './ts-cache/api'
export { setEpisodes } from './ts-cache/api'
export { setCurrentEpisode } from './ts-cache/api'
export { setSegments } from './ts-cache/api'
export { setTargetDuration } from './ts-cache/api'
export { setAbrSwitchCallback } from './ts-cache/api'
export { dropVideoCache } from './ts-cache/api'
export { dropSourceCache } from './ts-cache/api'
export { dropAllSegmentCache } from './ts-cache/api'
