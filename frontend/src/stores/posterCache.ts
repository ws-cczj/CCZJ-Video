import { defineStore } from 'pinia'
import { ref } from 'vue'
import { GetVideoDetail } from '../../bindings/cczjVideo/app'
import type { Video } from '../types'

/**
 * 单个海报缓存项
 * key 格式: "source_key:vod_id"
 */
export interface PosterCacheEntry {
  vod_name?: string
  vod_pic?: string
  vod_pic_proxied?: string
  cached_at: number      // 首次缓存时间 (ms)
  last_accessed: number  // 最后访问/点击时间 (ms)
  click_count: number    // 点击频次
}

const STORAGE_KEY = 'poster_cache_v1'
const SEVEN_DAYS_MS = 7 * 24 * 60 * 60 * 1000
const MAX_CACHED_ITEMS = 500   // 最大缓存条目数
const CONCURRENT_FETCH_LIMIT = 6

// 正在加载中的请求，防止重复请求
const loadingPromises = new Map<string, Promise<void>>()

function cacheKey(sourceKey: string, vodId: string): string {
  return `${sourceKey}:${vodId}`
}

function nowMs(): number {
  return Date.now()
}

export const usePosterCacheStore = defineStore('posterCache', () => {
  const cache = ref<Record<string, PosterCacheEntry>>({})
  const initialized = ref(false)

  // ------- 持久化读写 -------
  function loadFromStorage(): void {
    try {
      const raw = localStorage.getItem(STORAGE_KEY)
      if (raw) {
        cache.value = JSON.parse(raw)
      }
    } catch {
      cache.value = {}
    }
    cleanupExpired()
    trimToMax()
    initialized.value = true
  }

  function saveToStorage(): void {
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(cache.value))
    } catch {
      // 存储满或失败，静默忽略
    }
  }

  // ------- 清理策略 -------
  /** 清理 7 天未访问的条目 */
  function cleanupExpired(): void {
    const cutoff = nowMs() - SEVEN_DAYS_MS
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

    // 检查是否已过期
    if (nowMs() - entry.last_accessed > SEVEN_DAYS_MS) {
      delete cache.value[key]
      saveToStorage()
      return null
    }
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
      if (data.vod_pic) {
        existing.vod_pic = data.vod_pic
        existing.vod_pic_proxied = undefined
      }
      existing.last_accessed = now
    } else {
      cache.value[key] = {
        vod_name: data.vod_name,
        vod_pic: data.vod_pic,
        vod_pic_proxied: undefined,
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
      return cached
    }

    // 检查是否已有加载中的请求
    const pending = loadingPromises.get(key)
    if (pending) {
      await pending
      return get(sourceKey, vodId)
    }

    // 控制并发数
    if (loadingPromises.size >= CONCURRENT_FETCH_LIMIT) {
      return cached
    }

    const promise = (async () => {
      try {
        const resp = (await GetVideoDetail({
          source_key: sourceKey,
          vod_id: vodId,
          refresh: false,
        })) as { video?: Video | null } | null | undefined
        const v = resp?.video
        if (v) {
          set(sourceKey, vodId, {
            vod_name: v.vod_name,
            vod_pic: v.vod_pic,
          })
        }
      } catch {
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
  function getName(sourceKey: string, vodId: string, fallback = '未命名视频'): string {
    const entry = get(sourceKey, vodId)
    return entry?.vod_name || fallback
  }

  /** 便利函数：仅获取图片 URL（同步，会刷新访问时间） */
  function getPic(sourceKey: string, vodId: string): string {
    const entry = get(sourceKey, vodId)
    return entry?.vod_pic || ''
  }

  /**
   * 获取代理后的图片 URL（异步，会缓存代理结果）
   */
  async function getProxiedPic(sourceKey: string, vodId: string): Promise<string> {
    ensureInit()
    const entry = get(sourceKey, vodId)
    if (!entry?.vod_pic) return ''
    
    if (entry.vod_pic_proxied) {
      return entry.vod_pic_proxied
    }

    try {
      const { ProxyImage } = await import('../../bindings/cczjVideo/app')
      const proxied = await ProxyImage(entry.vod_pic)
      if (proxied && proxied.startsWith('data:')) {
        entry.vod_pic_proxied = proxied
        saveToStorage()
        return proxied
      }
    } catch { }
    
    return entry.vod_pic
  }

  // 暴露给外部的清理入口
  function clearAll(): void {
    cache.value = {}
    try { localStorage.removeItem(STORAGE_KEY) } catch { /* 忽略 */ }
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
    getProxiedPic,
    clearAll,
  }
})
