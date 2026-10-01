/**
 * 分片缓存键归一化 ——
 *
 * CDN 给同一个分片每次下发的 URL 都换一个时效签名（?sign=…&t=…&token=…）。
 * 用原始 URL 当缓存键的话，播放列表每刷新一次就集体未命中：同一份字节在内存里
 * 存 N 份、在 IndexedDB 里存 N 份，彼此把对方挤出上限——缓存越大命中率反而越低。
 *
 * 因此缓存键只保留「资源身份」：协议+主机+路径+那些无法证明与内容无关的参数。
 * 拿不准的参数一律留在键里（宁可漏命中，也不能把两个不同分片当成同一个返回内容）。
 *
 * 播放链路上 hls.js 看到的其实是本机代理地址（/__cczj/hls?u=<base64 上游 URL>），
 * 时效签名藏在 base64 里面，所以必须先把上游 URL 解出来再归一化，否则归一化对
 * 真实播放流量完全不起作用。解不出来就退回原始 URL —— 只是没有命中收益，不会错命中。
 */
const VOLATILE_SEGMENT_PARAMS: string[] = [
  'sign', 'signature', 'token', 'access_token', 'expires', 'expire', 'expiry',
  'timestamp', 'time', 't', 'ts', 'auth_key', 'wssecret', 'wstime', 'deadline',
  'userticket',
]

const HLS_PROXY_PATH = '/__cczj/hls'

/** 从本机 HLS 代理地址里取出真正的上游 URL；不是代理地址则返回 null。 */
function decodeHlsProxyUpstream(url: string): string | null {
  const pathAt = url.indexOf(HLS_PROXY_PATH + '?')
  if (pathAt < 0) return null
  const query = url.slice(pathAt + HLS_PROXY_PATH.length + 1)
  const encoded = new URLSearchParams(query).get('u')
  if (!encoded) return null
  try {
    const b64 = encoded.replace(/-/g, '+').replace(/_/g, '/')
    const binary = atob(b64.padEnd(Math.ceil(b64.length / 4) * 4, '='))
    const bytes = Uint8Array.from(binary, (ch) => ch.charCodeAt(0))
    const upstream = new TextDecoder().decode(bytes)
    return /^https?:\/\//i.test(upstream) ? upstream : null
  } catch {
    return null
  }
}

/**
 * 分片缓存键。`range` 来自 hls.js 的 #EXT-X-BYTERANGE 片段：同一个文件的
 * 不同字节段必须分开缓存，否则第二段会拿到第一段的内容。
 */
export function segmentCacheKey(url: string, range?: string | null): string {
  const upstream = decodeHlsProxyUpstream(url) ?? url
  let parsed: URL
  try {
    parsed = new URL(upstream)
  } catch {
    return range ? `${upstream}#r=${range}` : upstream
  }
  for (const param of VOLATILE_SEGMENT_PARAMS) parsed.searchParams.delete(param)
  const query = parsed.searchParams.toString()
  const base = `${parsed.origin}${parsed.pathname}` + (query ? `?${query}` : '')
  return range ? `${base}#r=${range}` : base
}

/**
 * hls.js 把 #EXT-X-BYTERANGE 的区间放在 `context.rangeStart / rangeEnd`（end 是开区间），
 * 同一个文件的多个字节段共用一个 URL。这里既用来分开缓存键，也用来发 Range 头，
 * 语义与 hls.js 自带 loader 一致：`bytes=start-(end-1)`。
 */
export function byteRangeOf(context: any): string | null {
  const start: number = context?.rangeStart || 0
  const end: number = context?.rangeEnd || 0
  if (end <= start) return null
  return `${start}-${end - 1}`
}

/** `bytes=start-end`（闭区间）的长度；解析不出来时返回 0。 */
export function rangeLength(range: string): number {
  const [start, end] = range.split('-').map(Number)
  if (!Number.isFinite(start) || !Number.isFinite(end) || end < start) return 0
  return end - start + 1
}

/**
 * 在片段清单里定位当前播放到的那一片。
 * 精确匹配失败时退化到"末两段路径"比对：CDN 换签名会让 URL 全串对不上。
 */
export function findSegmentIndex(segments: string[], target: string): number {
  const i = segments.indexOf(target)
  if (i >= 0) return i
  const cleanTail = (s: string) => { try { return new URL(s).pathname.split('/').slice(-2).join('/') } catch { return s.split('?')[0].split('/').slice(-2).join('/') } }
  const t = cleanTail(target)
  for (let k = 0; k < segments.length; k++) { if (cleanTail(segments[k]) === t) return k }
  return -1
}
