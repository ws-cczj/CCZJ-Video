export interface DanmakuPreferences { enabled: boolean; opacity: number; fontSize: number; speed: number; area: number; rows: number; fixed: boolean; scrolling: boolean; colored: boolean; freshDays: number }
export const defaultDanmakuPreferences: DanmakuPreferences = { enabled: true, opacity: .85, fontSize: 24, speed: 100, area: .5, rows: 6, fixed: true, scrolling: true, colored: true, freshDays: 1 }
export function decodeDanmakuPreferences(raw: unknown): DanmakuPreferences {
  let value: unknown = raw
  if (typeof value === 'string') { try { value = JSON.parse(value) } catch { return { ...defaultDanmakuPreferences } } }
  if (!value || typeof value !== 'object' || Array.isArray(value)) return { ...defaultDanmakuPreferences }
  const v = value as Record<string, unknown>
  const range = (key: keyof DanmakuPreferences, min: number, max: number) => typeof v[key] === 'number' && Number.isFinite(v[key]) ? Math.min(max, Math.max(min, Number(v[key]))) : Number(defaultDanmakuPreferences[key])
  return { enabled: typeof v.enabled === 'boolean' ? v.enabled : true, opacity: range('opacity', .25, 1), fontSize: Math.round(range('fontSize', 16, 36)), speed: range('speed', 50, 180), area: range('area', .25, .9), rows: Math.round(range('rows', 2, 12)), fixed: typeof v.fixed === 'boolean' ? v.fixed : true, scrolling: typeof v.scrolling === 'boolean' ? v.scrolling : true, colored: typeof v.colored === 'boolean' ? v.colored : true, freshDays: Math.round(range('freshDays', 1, 30)) }
}
