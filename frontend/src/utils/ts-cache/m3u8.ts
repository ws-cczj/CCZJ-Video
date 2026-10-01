import { stripAdFromM3u8Text } from './ad-filter'

// ====== m3u8 文本级缓存（避免重复请求同一个 m3u8）
// 上限：最多保留 16 条（每个 m3u8 文本很小，但 URL 无限多，不加限会长期泄漏）。
// 用 Map 的插入有序特性做简易 LRU：命中时 delete+set 重新插入到末尾，超限时删头部最旧。
const M3U8_CACHE_MAX = 16
const m3u8TextCache = new Map<string, string>()
export function getM3u8FromCache(url: string): string | null {
  const v = m3u8TextCache.get(url)
  if (v == null) return null
  // LRU：命中则移到末尾（最近使用）
  m3u8TextCache.delete(url)
  m3u8TextCache.set(url, v)
  return v
}
export function setM3u8Cache(url: string, text: string): void {
  if (m3u8TextCache.has(url)) m3u8TextCache.delete(url)
  m3u8TextCache.set(url, text)
  // 超限淘汰最旧（Map 迭代顺序 = 插入顺序，第一个即最久未用）
  while (m3u8TextCache.size > M3U8_CACHE_MAX) {
    const oldest = m3u8TextCache.keys().next().value
    if (oldest === undefined) break
    m3u8TextCache.delete(oldest)
  }
}

/** 缓存/失效入口要的是一次清空，而不是让调用方自己去摸这张 Map。 */
export function clearM3u8TextCache(): void { m3u8TextCache.clear() }

// ====== 解析 m3u8 -> 片段 URL 列表 + targetduration
// 关键区分：
//   master playlist → 含 #EXT-X-STREAM-INF，列出的是 variant m3u8 URL（多码率）
//   media playlist → 含 #EXTINF，列出的是真正的 TS 片段 URL
export interface M3u8ParseResult {
  urls: string[];          // media playlist: TS 片段 URL；master playlist: 空数组
  targetduration: number;
  text: string;
  isMaster: boolean;       // 是否为 master playlist（多码率）
  variantUrls: string[];   // master playlist 时的 variant m3u8 URL
  streamInfo: StreamVariantInfo[];  // master playlist 时每个 variant 的元数据
}

/** master playlist 中 #EXT-X-STREAM-INF 提取的 variant 元数据 */
export interface StreamVariantInfo {
  bandwidth: number;       // 码率 (bps)
  resolution: string;      // 分辨率 e.g. "1920x1080"
  codecs: string;          // 编解码器 e.g. "avc1.640028,mp4a.40.2"
  url: string;             // variant URL
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

export async function fetchAndParseM3u8(url: string): Promise<M3u8ParseResult> {
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
