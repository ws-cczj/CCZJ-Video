import { defineStore } from 'pinia'
import { ref } from 'vue'
import { GetVideoDetail } from '../api/app'
import { tr } from '../locales'
import type { Video } from '../types'
import { readStorage, removeStorage } from '../platform/storage'
import { createDebouncedWriter } from '../platform/debouncedWrite'

/**
 * 单个海报缓存项
 * key 格式: "source_key:vod_id"
 */
export interface PosterCacheEntry {
  vod_name?: string
  vod_pic?: string
  cached_at: number      // 首次缓存时间 (ms)
  last_accessed: number  // 最后访问/点击时间 (ms)
  click_count: number    // 点击频次
}

const STORAGE_KEY = 'poster_cache_v1'
// 收藏和历史里的片名/海报由本机数据库给出，不会自己变；改片名或换海报时 Go 侧的统一
// 失效层会推 cache:invalidate 过来（dropSource / clearAll）。所以这里可以按「月」而不
// 是「周」来留——留久的代价只是 localStorage 里几条字符串，留短的代价是每次翻收藏
// 都要重发一遍 GetVideoDetail。30 天是不指望失效兜底时的上限。
const STALE_MS = 30 * 24 * 60 * 60 * 1000
const MAX_CACHED_ITEMS = 500   // 最大缓存条目数
const CONCURRENT_FETCH_LIMIT = 6

// 正在加载中的请求，防止重复请求
const loadingPromises = new Map<string, Promise<void>>()

// 失效代数：dropSource 每调一次加一。进行中的 ensureLoaded 在发请求前记下当前值，
// 回来时如果已经变了，说明这份响应是失效之前的旧内容，不能再把它写回缓存。
let dropGeneration = 0

function cacheKey(sourceKey: string, vodId: string): string {
  return `${sourceKey}:${vodId}`
}

function nowMs(): number {
  return Date.now()
}

export const usePosterCacheStore = defineStore('posterCache', () => {
  const cache = ref<Record<string, PosterCacheEntry>>({})
  const initialized = ref(false)

  // 诊断页的读数：命中=缓存里已有可用内容，未命中=这一眼只能去取详情。
  // 它是普通对象而不是 ref：只有诊断面板轮询时会读一次，不该让每次海报渲染都触发响应式。
  const counters = { hits: 0, misses: 0, fetchOK: 0, fetchFail: 0 }

  // 这张 map 最多 500 条、装着片名和 data URL，整张 stringify 一次并不便宜；而命中一条海报、
  // 点一次封面都要重写全部。落盘因此走防抖（见 createDebouncedWriter），铺满一屏的几十次
  // set/recordClick 合并成一次写，卸载前补写。
  const persist = createDebouncedWriter(STORAGE_KEY, () => cache.value)

  // ------- 持久化读写 -------
  function loadFromStorage(): void {
    try {
      cache.value = readStorage<Record<string, PosterCacheEntry>>(STORAGE_KEY, {})
    } catch {
      cache.value = {}
    }
    cleanupExpired()
    trimToMax()
    initialized.value = true
  }

  function saveToStorage(): void {
    persist.schedule()
  }

  // ------- 清理策略 -------
  /** 清理长期未访问的条目 */
  function cleanupExpired(): void {
    const cutoff = nowMs() - STALE_MS
    let removed = 0
    for (const key of Object.keys(cache.value)) {
      if (cache.value[key].last_accessed < cutoff) {
        delete cache.value[key]
        removed++
      }
    }
    if (removed > 0) saveToStorage()
  }

  /** 如果缓存超过最大数量，则优先清理点击少、访问久的条目 */
  function trimToMax(): void {
    const keys = Object.keys(cache.value)
    if (keys.length <= MAX_CACHED_ITEMS) return

    const sorted = keys
      .map(k => ({
        key: k,
        clicks: cache.value[k].click_count,
        last: cache.value[k].last_accessed,
      }))
      // 点击频次优先（升序 = 低优先），其次是时间（越旧越优先删除）
      .sort((a, b) => {
        if (a.clicks !== b.clicks) return a.clicks - b.clicks
        return a.last - b.last
      })

    const toRemove = sorted.slice(0, keys.length - MAX_CACHED_ITEMS)
    for (const item of toRemove) {
      delete cache.value[item.key]
    }
    saveToStorage()
  }

  // ------- 主要 API -------
  function ensureInit(): void {
    if (!initialized.value) {
      loadFromStorage()
    }
  }

  /**
   * 查找缓存项。每次访问都会刷新 last_accessed。
   */
  function get(sourceKey: string, vodId: string): PosterCacheEntry | null {
    ensureInit()
    const key = cacheKey(sourceKey, vodId)
    const entry = cache.value[key]
    if (!entry) return null

    const now = nowMs()
    // 检查是否已过期
    if (now - entry.last_accessed > STALE_MS) {
      delete cache.value[key]
      saveToStorage()
      return null
    }
    // 兑现「每次访问刷新 last_accessed」：收藏里天天看、却从没点过的条目不该被 TTL/淘汰当成冷数据清掉。
    // last_accessed 没有任何渲染依赖它（模板只读 vod_name/vod_pic），所以在这里就地续期不会触发重渲染。
    entry.last_accessed = now
    saveToStorage()
    return entry
  }

  /**
   * 写入缓存。如果是新视频，click_count=1；否则保留原有计数和时间。
   */
  function set(
    sourceKey: string,
    vodId: string,
    data: { vod_name?: string; vod_pic?: string }
  ): void {
    ensureInit()
    const key = cacheKey(sourceKey, vodId)
    const existing = cache.value[key]
    const now = nowMs()
    if (existing) {
      // 保留原有点击计数和时间，只更新内容
      if (data.vod_name) existing.vod_name = data.vod_name
      if (data.vod_pic) existing.vod_pic = data.vod_pic
      existing.last_accessed = now
    } else {
      cache.value[key] = {
        vod_name: data.vod_name,
        vod_pic: data.vod_pic,
        cached_at: now,
        last_accessed: now,
        click_count: 1,
      }
    }
    saveToStorage()
    trimToMax()
  }

  /**
   * 记录一次点击（增加频次 + 更新时间）
   */
  function recordClick(sourceKey: string, vodId: string): void {
    ensureInit()
    const key = cacheKey(sourceKey, vodId)
    const entry = cache.value[key]
    const now = nowMs()
    if (entry) {
      entry.click_count += 1
      entry.last_accessed = now
    } else {
      cache.value[key] = {
        cached_at: now,
        last_accessed: now,
        click_count: 1,
      }
    }
    saveToStorage()
  }

  /**
   * 异步获取海报信息，若无缓存则调用 GetVideoDetail。
   * 不会重复发起相同请求。
   */
  async function ensureLoaded(
    sourceKey: string,
    vodId: string
  ): Promise<PosterCacheEntry | null> {
    ensureInit()
    const key = cacheKey(sourceKey, vodId)
    const cached = get(sourceKey, vodId)
    if (cached && (cached.vod_pic || cached.vod_name)) {
      // 已有缓存，直接返回
      counters.hits++
      return cached
    }

    // 检查是否已有加载中的请求
    const pending = loadingPromises.get(key)
    if (pending) {
      // 去重省掉了一次请求，但界面仍然在等那趟网络，所以按未命中算。
      counters.misses++
      await pending
      return get(sourceKey, vodId)
    }

    // 控制并发数
    if (loadingPromises.size >= CONCURRENT_FETCH_LIMIT) {
      return cached
    }

    counters.misses++
    const generation = dropGeneration

    const promise = (async () => {
      try {
        const resp = (await GetVideoDetail({
          source_key: sourceKey,
          vod_id: vodId,
          global_id: 0,
          refresh: false,
        })) as { video?: Video | null } | null | undefined
        const v = resp?.video
        if (v && generation === dropGeneration) {
          counters.fetchOK++
          set(sourceKey, vodId, {
            vod_name: v.vod_name,
            vod_pic: v.vod_pic,
          })
        }
      } catch {
        counters.fetchFail++
        // 忽略失败
      } finally {
        loadingPromises.delete(key)
      }
    })()

    loadingPromises.set(key, promise)
    await promise
    return get(sourceKey, vodId)
  }

  /** 便利函数：仅获取名称（同步，会刷新访问时间） */
  function getName(sourceKey: string, vodId: string, fallback = tr('common.unnamedVideo')): string {
    const entry = get(sourceKey, vodId)
    return entry?.vod_name || fallback
  }

  /** 便利函数：仅获取图片 URL（同步，会刷新访问时间） */
  function getPic(sourceKey: string, vodId: string): string {
    const entry = get(sourceKey, vodId)
    return entry?.vod_pic || ''
  }

  // 暴露给外部的清理入口
  function clearAll(): void {
    dropGeneration++
    cache.value = {}
    // 待写的防抖任务必须撤掉，否则它会把清空前抓到的快照整张写回来。
    persist.cancel()
    removeStorage(STORAGE_KEY)
  }

  /**
   * 让某个源的海报缓存作废：vodIds 给定时只删这些条目，不给时删该源全部。
   * 与 Go 侧统一失效层的 video / source 两个作用域一一对应。
   */
  function dropSource(sourceKey: string, vodIds?: string[]): number {
    if (!sourceKey) return 0
    ensureInit()
    // 抬一代：正在飞的 ensureLoaded 会发现自己的结果已经不该落盘了。
    // 不抬这一代，删片或重采恰好撞上一次海报抓取时，旧响应会把刚清掉的条目写回来。
    dropGeneration++
    const keys = vodIds?.length
      ? vodIds.map((vodId) => cacheKey(sourceKey, vodId))
      : Object.keys(cache.value).filter((key) => key.startsWith(`${sourceKey}:`))
    let removed = 0
    for (const key of keys) {
      if (delete cache.value[key]) removed++
    }
    if (removed > 0) saveToStorage()
    return removed
  }

  // 诊断页读数：条目数按当前缓存算，计数按本次会话累计。
  function stats() {
    return {
      entries: Object.keys(cache.value).length,
      hits: counters.hits,
      misses: counters.misses,
      fetchOK: counters.fetchOK,
      fetchFail: counters.fetchFail,
    }
  }

  return {
    cache,
    initialized,
    loadFromStorage,
    cleanupExpired,
    trimToMax,
    get,
    set,
    recordClick,
    ensureLoaded,
    getName,
    getPic,
    dropSource,
    clearAll,
    stats,
  }
})
