// 通用工具函数

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

  if (minutes < 1) return '刚刚'
  if (minutes < 60) return `${minutes} 分钟前`
  if (hours < 24) return `${hours} 小时前`
  if (days < 7) return `${days} 天前`
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
export function getDetailPath(sourceKey: string, video: { vod_g_id?: string | number; vod_id?: string | number; id?: number }): string {
  return `/detail/${sourceKey}/${getVideoId(video)}`
}

// 获取播放器路由路径（独立播放页面）
export function getPlayerPath(
  sourceKey: string,
  video: { vod_g_id?: string | number; vod_id?: string | number; id?: number },
  epIndex: number,
): string {
  return `/player/${sourceKey}/${getVideoId(video)}/${epIndex}`
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
  const base = sanitizeFilename(vodName || '视频')
  const ep = epName ? ` - ${sanitizeFilename(epName)}` : ` - 第${epNum}集`
  const ext = url ? guessExtFromUrl(url) : ''
  return base + ep + ext
}

// 构造单集电影文件名
export function buildSingleFilename(vodName: string, url?: string): string {
  const base = sanitizeFilename(vodName || '视频')
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

export async function getProxiedImageUrl(originalUrl: string): Promise<string> {
  if (!originalUrl) return ''
  if (imageProxyCache.has(originalUrl)) {
    return imageProxyCache.get(originalUrl)!
  }
  try {
    const { ProxyImage } = await import('../../bindings/cczjVideo/app')
    const result = await ProxyImage(originalUrl)
    if (result && result.startsWith('data:')) {
      imageProxyCache.set(originalUrl, result)
      return result
    }
  } catch { }
  return originalUrl
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
