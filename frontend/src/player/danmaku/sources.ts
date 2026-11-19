import { CCZJ } from '../../constants/cczj'
import { builtinDanmakuSources, isDanmakuProvider, type DanmakuProvider } from '../../constants/danmakuSources'

export const DANMAKU_SOURCE_SETTINGS_KEY = CCZJ.Settings.danmakuSources
export interface DanmakuSourceSettings { defaultProvider: string; disabled: DanmakuProvider[] }

export function sourceEnabled(settings: DanmakuSourceSettings, provider: string): boolean {
  return isDanmakuProvider(provider) && !settings.disabled.includes(provider)
}

export function normalizeSourceSettings(settings: DanmakuSourceSettings): DanmakuSourceSettings {
  const disabled = [...new Set(settings.disabled)]
  const first = builtinDanmakuSources.find(source => !disabled.includes(source.id))?.id ?? ''
  const defaultProvider = sourceEnabled(settings, settings.defaultProvider) ? settings.defaultProvider : first
  return { disabled, defaultProvider }
}

export function updateSourceEnabled(settings: DanmakuSourceSettings, provider: DanmakuProvider, enabled: boolean): DanmakuSourceSettings {
  const disabled = settings.disabled.filter(id => id !== provider)
  if (!enabled) disabled.push(provider)
  return normalizeSourceSettings({ ...settings, disabled })
}

export function decodeSourceSettings(raw: unknown): DanmakuSourceSettings {
  if (raw === '' || raw === undefined) return { defaultProvider: 'bilibili', disabled: [] }
  const value: unknown = typeof raw === 'string' ? JSON.parse(raw) : raw
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('Invalid danmaku source settings')
  const v = value as Record<string, unknown>
  if (v.defaultProvider !== undefined && v.defaultProvider !== '' && !isDanmakuProvider(v.defaultProvider)) throw new Error('Invalid default danmaku source')
  if (v.disabled !== undefined && (!Array.isArray(v.disabled) || !v.disabled.every(isDanmakuProvider))) throw new Error('Invalid disabled danmaku sources')
  if (v.mgtvEnabled !== undefined && typeof v.mgtvEnabled !== 'boolean') throw new Error('Invalid Mango TV source setting')
  const disabled = Array.isArray(v.disabled) ? v.disabled.filter(isDanmakuProvider) : []
  if (v.mgtvEnabled === false) disabled.push('mgtv')
  return normalizeSourceSettings({ defaultProvider: typeof v.defaultProvider === 'string' ? v.defaultProvider : 'bilibili', disabled })
}
