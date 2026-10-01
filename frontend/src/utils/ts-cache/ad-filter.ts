import { readStorage, writeStorage } from '../../platform/storage'

/** 广告域名黑名单 localStorage 键 */
const AD_BLACKLIST_STORAGE_KEY = 'cczj_ad_domain_blacklist'

/** 内置广告域名黑名单（匹配 hostname 子串） */
const _BUILTIN_AD_DOMAINS: string[] = [
  'dcs-vod.', 'vod-dcs.',
  'ads.', 'ad.', 'advert',
  'dsp.', 'doubleclick',
  'googlesyndication', 'googleads',
]

/** 当前生效的广告域名黑名单（内置 + 用户上报） */
let AD_DOMAIN_BLACKLIST: string[] = (() => {
  try {
    const userDomains = readStorage<string[] | null>(AD_BLACKLIST_STORAGE_KEY, null)
    if (userDomains) {
      return [..._BUILTIN_AD_DOMAINS, ...userDomains]
    }
  } catch {}
  return [..._BUILTIN_AD_DOMAINS]
})()

/** 用户上报的广告域名列表（不含内置域名） */
function _getUserAdDomains(): string[] {
  return AD_DOMAIN_BLACKLIST.filter(d => !_BUILTIN_AD_DOMAINS.includes(d))
}

/** 将域名加入广告黑名单（持久化到 localStorage） */
export function addAdDomain(domain: string): boolean {
  if (!domain || AD_DOMAIN_BLACKLIST.includes(domain)) return false
  AD_DOMAIN_BLACKLIST.push(domain)
  try {
    writeStorage(AD_BLACKLIST_STORAGE_KEY, _getUserAdDomains())
  } catch {}
  return true
}

/** 获取当前所有广告黑名单域名 */
export function getAdDomains(): string[] {
  return [...AD_DOMAIN_BLACKLIST]
}

/** 从 URL 字符串提取 hostname */
function _hostname(u: string): string {
  try { return new URL(u).hostname } catch { return '' }
}

/** 将 m3u8 中的片段路径解析为绝对 URL */
function _resolveSegUrl(seg: string, base: string): string {
  try { return new URL(seg, base).href } catch { return base + seg }
}

/**
 * 从 m3u8 文本中移除广告片段（双层过滤）
 * 第一层：域名黑名单 — .ts 片段域名命中黑名单则视为广告
 * 第二层（兜底）：DISCONTINUITY 分组，保留片段数最多的组（主内容）
 * @param text  m3u8 原始文本
 * @param m3u8Url m3u8 自身的 URL，用于解析相对路径
 */
export function stripAdFromM3u8Text(text: string, m3u8Url: string): string {
  const base = m3u8Url.substring(0, m3u8Url.lastIndexOf('/') + 1)
  const lines = text.split('\n')

  // ── 第一层：域名黑名单过滤 ──
  // 先扫描所有 .ts 片段，统计黑名单命中比例
  const segLines: { idx: number; raw: string; absUrl: string }[] = []
  for (let i = 0; i < lines.length; i++) {
    const trimmed = lines[i].trim()
    if (!trimmed || trimmed.startsWith('#')) continue
    // 跳过 #EXT-X-* 之后的值行（如 #EXT-X-MAP 的 URI 等），只收集真正的片段行
    // 片段行不以 # 开头，且上一行通常是 #EXTINF 或 #EXT-X-BYTERANGE 等
    const absUrl = _resolveSegUrl(trimmed, base)
    segLines.push({ idx: i, raw: trimmed, absUrl })
  }

  // 如果黑名单能过滤掉部分片段（但不是全部），直接用黑名单
  if (segLines.length > 0) {
    const adIndices = new Set<number>()
    for (const s of segLines) {
      const host = _hostname(s.absUrl)
      if (host && AD_DOMAIN_BLACKLIST.some(d => host.includes(d))) {
        adIndices.add(s.idx)
      }
    }
    // 黑名单命中了部分片段（非全部）→ 移除广告片段及其关联标签
    if (adIndices.size > 0 && adIndices.size < segLines.length) {
      const removeLines = new Set<number>()
      for (const idx of adIndices) {
        removeLines.add(idx)
        // 向上移除该片段关联的 #EXTINF / #EXT-X-* 标签行
        for (let j = idx - 1; j >= 0; j--) {
          const t = lines[j].trim()
          if (!t) continue
          if (t.startsWith('#EXTINF') || t.startsWith('#EXT-X-BYTERANGE') ||
              t.startsWith('#EXT-X-PROGRAM-DATE-TIME') || t.startsWith('#EXT-X-MAP')) {
            removeLines.add(j)
            continue
          }
          break // 遇到 DISCONTINUITY 或其他非关联标签停止
        }
      }
      // 同时移除孤立的 #EXT-X-DISCONTINUITY（前后片段都被删了）
      const remaining = lines.filter((_, i) => !removeLines.has(i))
      return _cleanOrphanDiscontinuity(remaining).join('\n')
    }
    // 黑名单未命中任何片段 → 进入第二层
  }

  // ── 第二层（兜底）：DISCONTINUITY 分组，保守移除小组（广告） ──
  return _stripAdByDiscontinuityGroup(lines)
}

/** 广告片段最大数量阈值（广告一般 10~25s，片段 2~8 个，放宽到 12 兜底） */
const MAX_AD_SEGMENT_COUNT = 12
/** 广告最大总时长（秒）—— 25s 的广告组也能被识别 */
const MAX_AD_TOTAL_DURATION = 30

/**
 * DISCONTINUITY 分组兜底：保守策略
 * 只移除同时满足以下条件的小组：
 *   1. 片段数 ≤ MAX_AD_SEGMENT_COUNT
 *   2. 总时长 ≤ MAX_AD_TOTAL_DURATION
 *   3. 不是唯一的内容组（避免把所有内容当广告删掉）
 *   4. 【新增】时长占比兜底：若某组片段数远小于最大组（< 10%），且
 *      平均片段时长明显小于内容组（广告常 3.5s/片 vs 正片 4.0s/片），也判为广告
 * 其余所有组保留，组间用 #EXT-X-DISCONTINUITY 连接
 */
function _stripAdByDiscontinuityGroup(lines: string[]): string {
  interface Group { lines: string[]; segCount: number; totalDuration: number }
  const groups: Group[] = []
  let cur: Group = { lines: [], segCount: 0, totalDuration: 0 }

  const header: string[] = []
  const footer: string[] = []
  let inFooter = false

  for (const line of lines) {
    const trimmed = line.trim()

    // 收集头部（全局标签）
    if (cur.segCount === 0 && groups.length === 0 &&
        (trimmed.startsWith('#EXTM3U') || trimmed.startsWith('#EXT-X-VERSION') ||
         trimmed.startsWith('#EXT-X-TARGETDURATION') || trimmed.startsWith('#EXT-X-MEDIA-SEQUENCE') ||
         trimmed.startsWith('#EXT-X-PLAYLIST-TYPE') || trimmed.startsWith('#EXT-X-INDEPENDENT-SEGMENTS'))) {
      header.push(line)
      continue
    }

    // 尾部标签
    if (trimmed === '#EXT-X-ENDLIST') {
      inFooter = true
      footer.push(line)
      continue
    }
    if (inFooter) { footer.push(line); continue }

    if (trimmed === '#EXT-X-DISCONTINUITY') {
      groups.push(cur)
      cur = { lines: [], segCount: 0, totalDuration: 0 }
      continue
    }

    cur.lines.push(line)
    if (trimmed && !trimmed.startsWith('#')) {
      cur.segCount++
    } else {
      // 提取 #EXTINF 时长
      const m = trimmed.match(/^#EXTINF:([\d.]+)/)
      if (m) cur.totalDuration += parseFloat(m[1])
    }
  }
  groups.push(cur)

  // 无分组或只有一组 → 原样返回
  if (groups.length <= 1) {
    return [...header, ...(groups[0]?.lines || []), ...footer].join('\n')
  }

  // 计算内容组参考值：最大片段数、内容组平均片段时长
  const maxSegCount = Math.max(...groups.map(g => g.segCount))
  // 收集"大组"（片段数 > maxSegCount * 30%）的平均片段时长，作为正片参考
  const largeGroups = groups.filter(g => g.segCount > maxSegCount * 0.3 && g.totalDuration > 0)
  const contentAvgDuration = largeGroups.length > 0
    ? largeGroups.reduce((s, g) => s + g.totalDuration / g.segCount, 0) / largeGroups.length
    : 0

  // 判断哪些组是广告（小组）
  const isAdGroup = (g: Group): boolean => {
    if (g.segCount <= 0) return false
    // 基本阈值判定
    if (g.segCount <= MAX_AD_SEGMENT_COUNT && g.totalDuration > 0 && g.totalDuration <= MAX_AD_TOTAL_DURATION) {
      return true
    }
    // 【增强】时长占比兜底：片段数远小于最大组 + 平均片段时长明显偏短
    if (maxSegCount > 20 && g.segCount < maxSegCount * 0.1 && contentAvgDuration > 0) {
      const avgDur = g.totalDuration / g.segCount
      // 广告片段平均时长比正片短 10% 以上
      if (avgDur > 0 && avgDur < contentAvgDuration * 0.9) {
        return true
      }
    }
    return false
  }

  // 统计非广告组数量
  const contentGroups = groups.filter(g => !isAdGroup(g))

  // 如果所有内容都被判定为广告（不应该发生）→ 全部保留，不做任何删除
  if (contentGroups.length === 0) {
    const result: string[] = [...header]
    for (let i = 0; i < groups.length; i++) {
      if (i > 0) result.push('#EXT-X-DISCONTINUITY')
      result.push(...groups[i].lines)
    }
    result.push(...footer)
    return result.join('\n')
  }

  // 正常情况：只移除广告小组，保留其余所有组
  const result: string[] = [...header]
  let firstKept = true
  for (const g of groups) {
    if (isAdGroup(g)) continue // 跳过广告组
    if (!firstKept) result.push('#EXT-X-DISCONTINUITY')
    result.push(...g.lines)
    firstKept = false
  }
  result.push(...footer)
  return result.join('\n')
}

/** 清理孤立的 #EXT-X-DISCONTINUITY（前后无片段时移除） */
function _cleanOrphanDiscontinuity(lines: string[]): string[] {
  const result: string[] = []
  for (let i = 0; i < lines.length; i++) {
    if (lines[i].trim() === '#EXT-X-DISCONTINUITY') {
      // 检查后面是否紧跟有效片段（跳过空行和标签）
      let hasSegAfter = false
      for (let j = i + 1; j < lines.length; j++) {
        const t = lines[j].trim()
        if (!t) continue
        if (!t.startsWith('#')) { hasSegAfter = true; break }
        if (t === '#EXT-X-DISCONTINUITY') break
        if (t === '#EXT-X-ENDLIST') break
      }
      if (hasSegAfter) result.push(lines[i])
      // 否则丢弃（孤立 DISCONTINUITY）
    } else {
      result.push(lines[i])
    }
  }
  return result
}
