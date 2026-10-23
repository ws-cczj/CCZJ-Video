import type { DanmakuEpisode } from '../../api/danmaku'

const extras = /预告|花絮|解说|剪辑|reaction|trailer|teaser|behind.the.scenes|特别篇|番外|合集|抢先看/i
const digits: Record<string, number> = { 零: 0, 〇: 0, 一: 1, 二: 2, 两: 2, 三: 3, 四: 4, 五: 5, 六: 6, 七: 7, 八: 8, 九: 9 }
function ordinal(raw: string): number | null {
  if (/^\d{1,4}$/.test(raw)) return Number(raw)
  let total = 0, digit = 0
  for (const char of raw) {
    if (char in digits) digit = digits[char]
    else if (char === '十' || char === '百') { total += (digit || 1) * (char === '十' ? 10 : 100); digit = 0 }
    else return null
  }
  return raw ? total + digit : null
}
export function normalizeWorkTitle(title: string): string {
  return title.normalize('NFKC').toLowerCase().replace(/<[^>]*>/g, '')
    .replace(/[【\[(（](?:国语|粤语|普通话|中字|高清|超清|\d{3,4}p|\d{4})[】\])）]/gi, '')
    .replace(/第([零〇一二两三四五六七八九十百\d]+)季/g, (_, value: string) => `第${ordinal(value) ?? value}季`)
    .replace(/[\s\p{P}\p{S}]/gu, '')
}
export function episodeNumber(label: string): number | null {
  const value = label.normalize('NFKC').trim()
  if (extras.test(value) || /^(?:第\s*)?\d+[.\-~至]\d/.test(value)) return null
  const match = /^(?:第\s*)?([零〇一二两三四五六七八九十百\d]+)(?:\s*[集话話期回]|\s*$|\s*[:：.、 -])/.exec(value)
    || /^(?:ep(?:isode)?\.?\s*)(\d{1,4})(?:\b|\s)/i.exec(value)
  return match ? ordinal(match[1]) : null
}
export function matchEpisode(rows: DanmakuEpisode[], label: string, durationSeconds = 0): DanmakuEpisode | null {
  const number = episodeNumber(label)
  const movie = number === null && /^(?:正片|HD|BD|高清|超清|蓝光|1080P|720P)(?:中字|国语|粤语)?$/i.test(label.trim())
  if (number === null && !movie) return null
  const exact = rows.filter(row => !extras.test(row.title) && (movie ? rows.length === 1 : episodeNumber(row.number || row.title) === number))
  const unique = [...new Map(exact.map(row => [`${row.provider}:${row.episodeId}`, row])).values()]
  // The production work matcher lives in Go. This compatibility binding check
  // also accepts matching episode numbers; duration only ranks duplicates.
  const score = (row: DanmakuEpisode) => durationSeconds > 0 && row.durationMs > 0
    ? 1000 - Math.floor(Math.abs(row.durationMs - durationSeconds * 1000) * 1000 / Math.max(row.durationMs, durationSeconds * 1000)) : 0
  unique.sort((a, b) => score(b) - score(a) || (`${a.provider}:${a.episodeId}` < `${b.provider}:${b.episodeId}` ? -1 : 1))
  return unique[0] ?? null
}
