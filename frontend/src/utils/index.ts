// 通用工具函数
import { readStorage, writeStorage } from '../platform/storage'
import { tr } from '../locales'

// 格式化时间显示
export function formatTime(dateStr: string): string {
  if (!dateStr) return ''
  const d = new Date(dateStr.replace(' ', 'T'))
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

// 解析下载 URL（优先 down_url，其次 play_url）
export function resolveEpisodeUrl(ep: { ep_url: string; ep_down_url?: string }): string {
  return ep.ep_down_url || ep.ep_url || ''
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
const IMAGE_PROXY_TTL = 7 * 24 * 60 * 60 * 1000
const IMAGE_PROXY_MAX = 80
const IMAGE_PROXY_FAILURE_TTL = 5 * 60 * 1000

interface StoredImageProxy { value: string; accessed: number }

function readStoredImageProxy(url: string): string {
  try {
    const all = readStorage<Record<string, StoredImageProxy>>(IMAGE_PROXY_STORAGE_KEY, {})
    const item = all[url]
    if (!item || Date.now() - item.accessed > IMAGE_PROXY_TTL) return ''
    item.accessed = Date.now()
    writeStorage(IMAGE_PROXY_STORAGE_KEY, all)
    return item.value
  } catch { return '' }
}

function writeStoredImageProxy(url: string, value: string): void {
  try {
    const all = readStorage<Record<string, StoredImageProxy>>(IMAGE_PROXY_STORAGE_KEY, {})
    all[url] = { value, accessed: Date.now() }
    const keys = Object.keys(all)
    if (keys.length > IMAGE_PROXY_MAX) {
      keys.sort((a, b) => all[a].accessed - all[b].accessed)
        .slice(0, keys.length - IMAGE_PROXY_MAX)
        .forEach(key => delete all[key])
    }
    writeStorage(IMAGE_PROXY_STORAGE_KEY, all)
  } catch { /* localStorage is optional */ }
}

export async function getProxiedImageUrl(originalUrl: string): Promise<string> {
  if (!originalUrl) return ''
  if (imageProxyCache.has(originalUrl)) {
    return imageProxyCache.get(originalUrl)!
  }
  const stored = readStoredImageProxy(originalUrl)
  if (stored) {
    imageProxyCache.set(originalUrl, stored)
    return stored
  }
  const failedAt = imageProxyFallbackCache.get(originalUrl)
  if (failedAt && Date.now() - failedAt < IMAGE_PROXY_FAILURE_TTL) return ''
  if (failedAt) imageProxyFallbackCache.delete(originalUrl)
  const pending = imageProxyPending.get(originalUrl)
  if (pending) return pending

  const request = (async () => {
    try {
      const { ProxyImage } = await import('../api/app')
      const result = await ProxyImage(originalUrl)
      if (result && result.startsWith('data:')) {
        imageProxyCache.set(originalUrl, result)
        writeStoredImageProxy(originalUrl, result)
        return result
      }
    } catch { }
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
