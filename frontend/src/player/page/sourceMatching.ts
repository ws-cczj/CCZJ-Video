import * as AppMod from '../../api/app'

/** 模糊匹配视频名称（去除空格、大小写统一、允许包含关系） */
function fuzzyMatchVod(name1: string, name2: string): boolean {
  const n1 = name1.replace(/\s+/g, '').toLowerCase()
  const n2 = name2.replace(/\s+/g, '').toLowerCase()
  if (n1 === n2) return true
  if (n1.includes(n2) || n2.includes(n1)) return true
  // 允许去掉常见后缀（第一季/第二季等）后匹配
  const seasonRe = /第[一二三四五六七八九十\d]+季$/
  const s1 = n1.replace(seasonRe, '')
  const s2 = n2.replace(seasonRe, '')
  if (s1 && s2 && (s1 === s2 || s1.includes(s2) || s2.includes(s1))) return true
  return false
}

export function findBestMatch(list: any[], vodName: string): any | null {
  if (!list.length) return null
  // 精确匹配优先
  const exact = list.find((v: any) => v.vod_name === vodName)
  if (exact) return exact
  // 模糊匹配
  const fuzzy = list.find((v: any) => fuzzyMatchVod(v.vod_name || '', vodName))
  if (fuzzy) return fuzzy
  // 兆底第一条
  return list[0]
}

/** 搜索源并自动回退：原名 → 去空格版 → 去季后缀版 */
// SearchSource 的第 3 个参数是页码、第 4 个才是条数，这里只要第 1 页 10 条。
const PROBE_PAGE_SIZE = 10

// 源站标题常带清晰度/压制尾巴（"雷神4：爱与雷霆_1080P_"），切到那种源之后再用它去问
// 别的源必然搜不到，于是探测会把明明有的源标成"未收录"。探测只认清洗过的名字。
const PROBE_JUNK_TAIL_RE = /(?:[\s_\-]+(?:\d{3,4}[pPiI]|hd|bd|uhd|web[\s_-]?rip|x26[45]|bluray)[\s_\-]*)+$/i

export function probeNameOf(raw: string): string {
  const cleaned = raw.replace(PROBE_JUNK_TAIL_RE, '').trim()
  return cleaned || raw
}

export interface SourceSearchScope {
  current: () => boolean
  request: (request: Promise<any> & {cancel?:()=>void|Promise<void>}) => Promise<any>
}
export async function searchSourceWithFallback(sk: string, vodName: string, scope?: SourceSearchScope): Promise<any[]> {
  const current = scope?.current ?? (()=>true)
  const run = scope?.request ?? ((request:Promise<any>)=>request)
  if (!current()) return []
  let failed = false
  let failure: unknown
  // 第一次：原名搜索
  try {
    const resp = await run((AppMod as any).SearchSource(sk, vodName, 1, PROBE_PAGE_SIZE)) as any
    const list = Array.isArray(resp?.videos) ? resp.videos : []
    if (list.length > 0) return list
  } catch (error) { failed = true; failure = error }

  if (!current()) return []
  // 第二次：去空格版搜索（解决“权力的游戏 第一季”vs“权力的游戏第一季”问题）
  const noSpaceName = vodName.replace(/\s+/g, '')
  if (noSpaceName !== vodName) {
    try {
      const resp2 = await run((AppMod as any).SearchSource(sk, noSpaceName, 1, PROBE_PAGE_SIZE)) as any
      const list2 = Array.isArray(resp2?.videos) ? resp2.videos : []
      if (list2.length > 0) {
        console.log(`[Player] ✔ 去空格搜索 "${noSpaceName}" 找到 ${list2.length} 条结果`)
        return list2
      }
    } catch (error) { failed = true; failure = error }
  }

  if (!current()) return []
  // 第三次：去掉季后缀搜索（如“权力的游戏 第一季”→“权力的游戏”）
  const seasonRe = /第[一二三四五六七八九十\d]+季\s*$/
  const baseName = vodName.replace(seasonRe, '').trim()
  if (baseName && baseName !== vodName && baseName !== noSpaceName) {
    try {
      const resp3 = await run((AppMod as any).SearchSource(sk, baseName, 1, PROBE_PAGE_SIZE)) as any
      const list3 = Array.isArray(resp3?.videos) ? resp3.videos : []
      if (list3.length > 0) {
        console.log(`[Player] ✔ 基名搜索 "${baseName}" 找到 ${list3.length} 条结果`)
        return list3
      }
    } catch (error) { failed = true; failure = error }
  }

  // A failed transport cannot prove that the source has no matching title.
  if (failed && current()) throw failure
  return []
}

