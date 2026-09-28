import type { Episode, PlayLine } from '../types'

/**
 * 把详情响应归一成线路数组。
 *
 * 后端正常情况下给 lines（$$$ 拆出来的多条线路）；旧缓存条目或目录兜底只有 episodes，
 * 此时把单份集表当成一条无名线路，调用方就不必为「有没有 lines」写两套分支。
 * 没有集表的线路直接丢掉：它们只会让选集网格出现一条点了没反应的空白线路。
 */
export function normalizePlayLines(resp: { episodes?: Episode[] | null; lines?: PlayLine[] | null }): PlayLine[] {
  const parsed = (resp?.lines || []).filter((line) => !!line && Array.isArray(line.episodes) && line.episodes.length > 0)
  if (parsed.length > 0) return parsed
  const episodes = Array.isArray(resp?.episodes) ? resp.episodes : []
  return episodes.length > 0 ? [{ index: 0, name: '', episodes }] : []
}

/**
 * 线路的展示名。
 *
 * 源站常常不给 vod_play_from，Name 就是空串；占位文案留给界面翻译，Go 侧不下发中文。
 */
export function playLineLabel(line: PlayLine, ordinal: string): string {
  return line.name && line.name.trim() ? line.name.trim() : ordinal
}
