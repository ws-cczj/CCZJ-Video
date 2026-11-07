export type DanmakuFontFamily = 'sans' | 'serif' | 'mono'
export type DanmakuFontWeight = 400 | 500 | 600 | 700
export type DanmakuTextStyle = 'stroke' | 'shadow' | 'none'
export interface DanmakuPreferences {
  enabled: boolean; opacity: number; fontSize: number; speed: number; area: number; rows: number
  fixed: boolean; scrolling: boolean; colored: boolean; freshDays: number
  fontFamily: DanmakuFontFamily; fontWeight: DanmakuFontWeight; textStyle: DanmakuTextStyle
  strokeWidth: number; density: number
}
export const defaultDanmakuPreferences: DanmakuPreferences = {
  enabled: true, opacity: .85, fontSize: 24, speed: 100, area: .5, rows: 12,
  fixed: true, scrolling: true, colored: true, freshDays: 1,
  fontFamily: 'sans', fontWeight: 400, textStyle: 'stroke', strokeWidth: 1.5, density: 1,
}
export function decodeDanmakuPreferences(raw: unknown): DanmakuPreferences {
  let value: unknown = raw
  if (typeof value === 'string') { try { value = JSON.parse(value) } catch { return { ...defaultDanmakuPreferences } } }
  if (!value || typeof value !== 'object' || Array.isArray(value)) return { ...defaultDanmakuPreferences }
  const v = value as Record<string, unknown>
  const range = (key: keyof DanmakuPreferences, min: number, max: number) => typeof v[key] === 'number' && Number.isFinite(v[key]) ? Math.min(max, Math.max(min, Number(v[key]))) : Number(defaultDanmakuPreferences[key])
  return {
    enabled: typeof v.enabled === 'boolean' ? v.enabled : true,
    opacity: range('opacity', .1, 1), fontSize: range('fontSize', 12, 48), speed: range('speed', 50, 180),
    area: range('area', .25, 1), rows: Math.round(range('rows', 2, 24)),
    fixed: typeof v.fixed === 'boolean' ? v.fixed : true,
    scrolling: typeof v.scrolling === 'boolean' ? v.scrolling : true,
    colored: typeof v.colored === 'boolean' ? v.colored : true,
    freshDays: Math.round(range('freshDays', 1, 30)),
    fontFamily: v.fontFamily === 'serif' || v.fontFamily === 'mono' ? v.fontFamily : 'sans',
    fontWeight: v.fontWeight === 500 || v.fontWeight === 600 || v.fontWeight === 700 ? v.fontWeight : 400,
    textStyle: v.textStyle === 'shadow' || v.textStyle === 'none' ? v.textStyle : 'stroke',
    strokeWidth: range('strokeWidth', 0, 4), density: range('density', .25, 1),
  }
}
