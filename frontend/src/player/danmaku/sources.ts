export const DANMAKU_SOURCE_SETTINGS_KEY = 'danmaku_sources_v1'
export const builtinDanmakuSources = [
  { id: 'bilibili', name: 'B站', protocol: 'Protobuf' },
  { id: 'qq', name: '腾讯视频', protocol: 'JSON' },
  { id: 'iqiyi', name: '爱奇艺', protocol: 'Brotli · Protobuf / XML' },
] as const
export type DanmakuProvider = typeof builtinDanmakuSources[number]['id']
export interface DanmakuSourceSettings { defaultProvider: string; disabled: DanmakuProvider[] }
const known = (value: unknown): value is DanmakuProvider => builtinDanmakuSources.some(source => source.id === value)
export function sourceEnabled(settings: DanmakuSourceSettings, provider: string): boolean {
  return known(provider) && !settings.disabled.includes(provider)
}
export function normalizeSourceSettings(settings: DanmakuSourceSettings): DanmakuSourceSettings {
  return { disabled: [...new Set(settings.disabled)], defaultProvider: sourceEnabled(settings, settings.defaultProvider) ? settings.defaultProvider : builtinDanmakuSources.find(source => sourceEnabled(settings, source.id))?.id ?? '' }
}
export function decodeSourceSettings(raw: unknown): DanmakuSourceSettings {
  if (raw === '' || raw === undefined) return { defaultProvider: 'bilibili', disabled: [] }
  const value: unknown = typeof raw === 'string' ? JSON.parse(raw) : raw
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('Invalid danmaku source settings')
  const v = value as Record<string, unknown>
  if (v.defaultProvider !== undefined && v.defaultProvider !== '' && !known(v.defaultProvider)) throw new Error('Invalid default danmaku source')
  if (v.disabled !== undefined && (!Array.isArray(v.disabled) || !v.disabled.every(known))) throw new Error('Invalid disabled danmaku sources')
  return normalizeSourceSettings({ defaultProvider: typeof v.defaultProvider === 'string' ? v.defaultProvider : 'bilibili', disabled: Array.isArray(v.disabled) ? v.disabled.filter(known) : [] })
}
