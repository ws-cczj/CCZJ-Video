// 通用工具函数
import { readStorage } from '../platform/storage'
import { createDebouncedWriter } from '../platform/debouncedWrite'
import { tr } from '../locales'

// 格式化时间显示
// 数据库里的时间戳是 UTC 裸串（SQLite CURRENT_TIMESTAMP，形如 '2026-09-28 03:41:00'）。
// new Date() 对不带时区标识的 ISO 串按本地时区解析，相对时间会整体偏移，
// 因此这里给缺少时区标识的串补 'Z' 再解析；已自带 Z / ±hh:mm 的原样解析，不重复加。
export function formatTime(dateStr: string): string {
  if (!dateStr) return ''
  const normalized = dateStr.replace(' ', 'T')
  const hasTz = /(?:z|[+-]\d{2}:?\d{2})$/i.test(normalized)
  const hasTime = /\d{2}:\d{2}/.test(normalized)
  const d = new Date(!hasTz && hasTime ? `${normalized}Z` : normalized)
  if (isNaN(d.getTime())) return dateStr.replace('T', ' ').slice(0, 16)
  const now = new Date()
  const diff = now.getTime() - d.getTime()
  const minutes = Math.floor(diff / 60000)
  const hours = Math.floor(diff / 3600000)
  const days = Math.floor(diff / 86400000)

  if (minutes < 1) return tr('common.justNow')
  if (minutes < 60) return tr('common.minutesAgo', { n: minutes })
  if (hours < 24) return tr('common.hoursAgo', { n: hours })
  if (days < 7) return tr('common.daysAgo', { n: days })
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

// 获取视频的唯一 ID（优先使用 vod_id，兼容 vod_g_id）
export function getVideoId(video: { vod_g_id?: string | number; vod_id?: string | number; id?: number }): string | number {
  return (video.vod_id !== undefined && video.vod_id !== null && String(video.vod_id) !== '')
    ? video.vod_id
    : (video.vod_g_id !== undefined && video.vod_g_id !== null && String(video.vod_g_id) !== '')
      ? video.vod_g_id
      : (video.id ?? 0)
}

// 获取视频详情路由路径
export function getDetailPath(sourceKey: string, video: { global_id?: string | number; vod_g_id?: string | number; vod_id?: string | number; id?: number }): string {
  const globalId = video.global_id
  if (globalId !== undefined && globalId !== null && String(globalId) !== '') {
    return `/detail/${sourceKey}/${globalId}?vod=${encodeURIComponent(String(getVideoId(video)))}`
  }
  // The path slot is always globalId. A legacy item without one remains
  // addressable through the source+vod compatibility adapter.
  return `/detail/${sourceKey}/0?vod=${encodeURIComponent(String(getVideoId(video)))}`
}

// 获取播放器路由路径（独立播放页面）
export function getPlayerPath(
  sourceKey: string,
  video: { global_id?: string | number; vod_g_id?: string | number; vod_id?: string | number; id?: number },
  epIndex: number,
): string {
  const globalId = video.global_id
  const id = globalId !== undefined && globalId !== null && String(globalId) !== '' ? globalId : 0
  return `/player/${sourceKey}/${id}/${epIndex}?vod=${encodeURIComponent(String(getVideoId(video)))}`
}

// 解析 URL 获取域名部分
export function extractDomainKey(apiUrl: string): string {
  try {
    const url = new URL(apiUrl)
    return url.hostname.replace(/^api\./, '').split('.')[0].replace(/[^a-z0-9]/g, '_')
  } catch {
    return ''
  }
}

// 清理文件名中的非法字符
export function sanitizeFilename(name: string): string {
  if (!name) return 'file'
  return name
    .replace(/[\\/:*?"<>|\r\n\t]+/g, '_')
    .replace(/\s+/g, ' ')
    .replace(/^[.\s]+|[.\s]+$/g, '')
    .slice(0, 120) || 'file'
}

// 从 URL 推断扩展名（失败返回空）
export function guessExtFromUrl(url: string): string {
  try {
    const u = new URL(url)
    const last = u.pathname.split('/').pop() || ''
    const dot = last.lastIndexOf('.')
    if (dot > 0) {
      const ext = last.slice(dot + 1).toLowerCase()
      if (/^[a-z0-9]{1,6}$/.test(ext)) return '.' + ext
    }
  } catch { }
  return ''
}

// 构造剧集文件名（带扩展名推断）
export function buildEpisodeFilename(
  vodName: string,
  epNum: number,
  epName?: string,
  url?: string,
): string {
  const base = sanitizeFilename(vodName || tr('downloads.video'))
  const ep = epName ? ` - ${sanitizeFilename(epName)}` : ` - ${tr('detail.episode', { num: epNum })}`
  const ext = url ? guessExtFromUrl(url) : ''
  return base + ep + ext
}

// 构造单集电影文件名
export function buildSingleFilename(vodName: string, url?: string): string {
  const base = sanitizeFilename(vodName || tr('downloads.video'))
  const ext = url ? guessExtFromUrl(url) : ''
  return base + ext
}

// 取一集的播放地址。后端已经把 $$$ 多线路拆成 PlayLine，每集只剩一个 ep_url，
// 这里不再猜下载字段。
export function resolveEpisodeUrl(ep: { ep_url?: string }): string {
  return ep.ep_url || ''
}

// 构造搜索路由路径（用于类型/导演/演员/年份标签跳转）
export function getSearchPath(keyword: string, sourceKey?: string): string {
  const params: string[] = []
  const kw = encodeURIComponent(keyword || '')
  params.push(`keyword=${kw}`)
  if (sourceKey) params.push(`source=${encodeURIComponent(sourceKey)}`)
  return `/search?${params.join('&')}`
}

// 字节数转可读字符串 (B/KB/MB/GB)
export function humanizeBytes(bytes: number): string {
  if (!bytes || bytes <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let i = 0
  let n = bytes
  while (n >= 1024 && i < units.length - 1) {
    n = n / 1024
    i++
  }
  return `${n.toFixed(n >= 10 || i === 0 ? 0 : 1)} ${units[i]}`
}

const imageProxyCache = new Map<string, string>()
const imageProxyPending = new Map<string, Promise<string>>()
// A failed proxy attempt should not fall back to the remote URL: doing so lets
// every <img> trigger another CDN request and surface a noisy 418. Keep a
// short failure cooldown so a later mount can recover from transient outages.
const imageProxyFallbackCache = new Map<string, number>()
const IMAGE_PROXY_STORAGE_KEY = 'cczj_image_proxy_cache_v1'
// 与海报缓存同寿命：键是原始 URL，同一个 URL 的代理结果不会变，过期只会带来一次重取。
// 而且重取现在不出网：Go 侧还有一层按 URL 记账的磁盘副本（app/proxy/imagestore.go），
// 这里被 LRU 挤掉的那 80 条只是省掉一次 IPC，不再是省掉一次 CDN 请求。
const IMAGE_PROXY_TTL = 30 * 24 * 60 * 60 * 1000
const IMAGE_PROXY_MAX = 80
// 内存层以前不设上限，整场会话看过的 data URL 全留着（单条几十~几百 KB），是实打实的内存泄漏。
// 这里同样封顶：被挤掉的条目还能从 IMAGE_PROXY_MAX 条持久副本读回，最多回 Go 磁盘缓存重取一次。
const IMAGE_PROXY_MEMORY_MAX = 80
const IMAGE_PROXY_FAILURE_TTL = 5 * 60 * 1000

interface StoredImageProxy { value: string; accessed: number }

// 这张 map 存的是 data URL，80 条就是好几 MB。以前每读一个新 URL 都要把整张 parse 一遍、
// 再为了刷新 accessed 把整张 stringify 写回去——首屏铺几十张海报就是几十轮全量序列化。
// 现在常驻内存一份，落盘合并成防抖一次（见 createDebouncedWriter）。
// imageProxyCache 与这份持久副本各自封顶（IMAGE_PROXY_MEMORY_MAX / IMAGE_PROXY_MAX），内存有上界。
let memStoredProxy: Record<string, StoredImageProxy> | null = null

function storedProxies(): Record<string, StoredImageProxy> {
  if (!memStoredProxy) {
    const all = readStorage<Record<string, StoredImageProxy>>(IMAGE_PROXY_STORAGE_KEY, {})
    memStoredProxy = all && typeof all === 'object' ? all : {}
  }
  return memStoredProxy
}

const persistStoredProxy = createDebouncedWriter(IMAGE_PROXY_STORAGE_KEY, storedProxies)

// 诊断页要回答「海报到底有没有走缓存」，所以这里记下三类读数：内存命中、持久命中、真的出网。
// Go 侧的出网账本只看得见最后那一类（命中缓存时根本不发请求），两边合起来才是完整画面。
const imageProxyCounters = { memoryHits: 0, storedHits: 0, fetches: 0, failures: 0 }

export function imageProxyStats(): {
  entries: number; memoryHits: number; storedHits: number; fetches: number; failures: number;
} {
  return {
    entries: imageProxyCache.size,
    memoryHits: imageProxyCounters.memoryHits,
    storedHits: imageProxyCounters.storedHits,
    fetches: imageProxyCounters.fetches,
    failures: imageProxyCounters.failures,
  }
}

function readStoredImageProxy(url: string): string {
  const all = storedProxies()
  const item = all[url]
  if (!item || Date.now() - item.accessed > IMAGE_PROXY_TTL) return ''
  item.accessed = Date.now()
  persistStoredProxy.schedule()
  return item.value
}

function writeStoredImageProxy(url: string, value: string): void {
  const all = storedProxies()
  all[url] = { value, accessed: Date.now() }
  const keys = Object.keys(all)
  if (keys.length > IMAGE_PROXY_MAX) {
    keys.sort((a, b) => all[a].accessed - all[b].accessed)
      .slice(0, keys.length - IMAGE_PROXY_MAX)
      .forEach(key => delete all[key])
  }
  persistStoredProxy.schedule()
}

// Map 插入序即访问序：新写入落尾部，命中时由调用方 delete+set 挪回尾部，队首永远是最久没用的。
// 超过 IMAGE_PROXY_MEMORY_MAX 就从队首挤掉，把内存层也钉在固定条数上。
function rememberProxiedImage(url: string, value: string): void {
  imageProxyCache.set(url, value)
  while (imageProxyCache.size > IMAGE_PROXY_MEMORY_MAX) {
    const oldest = imageProxyCache.keys().next().value
    if (oldest === undefined) break
    imageProxyCache.delete(oldest)
  }
}

export async function getProxiedImageUrl(originalUrl: string): Promise<string> {
  if (!originalUrl) return ''
  const cached = imageProxyCache.get(originalUrl)
  if (cached !== undefined) {
    imageProxyCounters.memoryHits++
    // 命中即移到尾部，维持「队首=最久未用」，让上面的容量上限淘汰的是真正的冷条目
    imageProxyCache.delete(originalUrl)
    imageProxyCache.set(originalUrl, cached)
    return cached
  }
  const stored = readStoredImageProxy(originalUrl)
  if (stored) {
    imageProxyCounters.storedHits++
    rememberProxiedImage(originalUrl, stored)
    return stored
  }
  const failedAt = imageProxyFallbackCache.get(originalUrl)
  if (failedAt && Date.now() - failedAt < IMAGE_PROXY_FAILURE_TTL) return ''
  if (failedAt) imageProxyFallbackCache.delete(originalUrl)
  const pending = imageProxyPending.get(originalUrl)
  if (pending) return pending

  const request = (async () => {
    imageProxyCounters.fetches++
    try {
      const { ProxyImage } = await import('../api/app')
      const result = await ProxyImage(originalUrl)
      if (result && result.startsWith('data:')) {
        rememberProxiedImage(originalUrl, result)
        writeStoredImageProxy(originalUrl, result)
        return result
      }
    } catch { }
    imageProxyCounters.failures++
    imageProxyFallbackCache.set(originalUrl, Date.now())
    return ''
  })()
  imageProxyPending.set(originalUrl, request)
  try {
    return await request
  } finally {
    imageProxyPending.delete(originalUrl)
  }
}

/**
 * HTML 实体引用表（用于 stripHtmlTags 中的一次替换）
 */
const _HTML_ENTITIES: Record<string, string> = {
  '&nbsp;': ' ',
  '&amp;': '&',
  '&lt;': '<',
  '&gt;': '>',
  '&quot;': '"',
  '&#39;': "'",
  '&apos;': "'",
  '&#34;': '"',
  '&ldquo;': '"',
  '&rdquo;': '"',
  '&hellip;': '…',
  '&mdash;': '—',
  '&ndash;': '–',
}

/** 编译一次的正则：匹配命名实体 + 数字实体 + 十六进制实体 */
const _ENTITY_RE = /&(?:#(\d+)|#x([0-9a-fA-F]+)|([a-z]+));/g

// 去除 HTML 标签（采集站返回的内容常夹杂 <p> 等标签）
export function stripHtmlTags(html: string | null | undefined): string {
  if (!html) return ''
  // 1) 去掉 <script>/<style> 整段
  let s = html.replace(/<(script|style)\b[\s\S]*?<\/\1>/gi, '')
  // 2) 去掉所有 HTML 标签
  s = s.replace(/<[^>]+>/g, '')
  // 3) ⭐ 优化：一次正则替换所有实体，避免多次字符串拼接
  s = s.replace(_ENTITY_RE, (_m, dec: string, hex: string, name: string) => {
    if (dec) return String.fromCharCode(parseInt(dec, 10))
    if (hex) return String.fromCharCode(parseInt(hex, 16))
    if (name) {
      const named = `&${name};`
      return _HTML_ENTITIES[named] ?? _m
    }
    return _m
  })
  // 4) 清理多余空白
  s = s.replace(/\s+/g, ' ').trim()
  return s
}
