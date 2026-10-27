import { GetSetting } from '../api/app'

export const FONT_SIZE_PRESETS = [
  { key:'xs', px:12 }, { key:'sm', px:13 }, { key:'md', px:14 },
  { key:'lg', px:16 }, { key:'xl', px:18 }, { key:'xxl', px:20 },
] as const

/** All interface text derives from rem; reader content owns its separate size. */
export function applyFontSize(key: string): void {
  const preset = FONT_SIZE_PRESETS.find(value => value.key === key) ?? FONT_SIZE_PRESETS[2]
  document.documentElement.style.fontSize = `${preset.px}px`
}

export function applyFontFamily(family: string): void {
  if (family) document.documentElement.style.fontFamily = family
  else document.documentElement.style.removeProperty('font-family')
}

/** Apply before mounting, so late startup reads cannot undo an edit in settings. */
export async function loadTypography(): Promise<void> {
  const [size, family] = await Promise.all([
    GetSetting('font_size_key').catch(() => 'md'), GetSetting('font_family').catch(() => ''),
  ])
  applyFontSize(size)
  applyFontFamily(family)
}
