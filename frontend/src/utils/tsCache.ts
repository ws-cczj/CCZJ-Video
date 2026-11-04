/**
 * m3u8 TS 分片预取 + 内存缓存 + IndexedDB 持久化
 *
 * 核心机制：
 *   1. HLS loader 通过共享传输读取内存、磁盘与在途分片。
 *   2. 连续未播放库存按上下限补货，三路预取与播放器协助独占领取。
 *   3. 详情与短剧预热复用同一传输及缓存，不创建额外解码器。
 *   4. IndexedDB 持久化：256 MiB / 7 天 TTL，播放释放后提交附近分片及 LRU 维护
 *
 * 实现按职责拆在 ./ts-cache/ 下：内存 LRU（memory-cache）、预取与取回
 * （prefetch）、共享传输与重试账本（transfer）、网络策略（network）、统计（telemetry）、
 * m3u8 解析与去广告（m3u8 / ad-filter）、落盘（disk-cache）、播放器 loader
 * （loader）、对外门面（api）。store.ts 保留共享缓存与播放身份，
 * 传输和补货的内部状态分别由所属模块持有。
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
export { prefetchMediaStart } from './ts-cache/prefetch'
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
