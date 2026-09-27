import { defineStore } from 'pinia'
import { ref, computed, watch } from 'vue'
import { GetSetting, SetSetting } from '../api/app'
import { appEvent } from '../event'
import { tr } from '../locales'
import landingMoon from '../assets/theme_images/landingMoon.png'
import jqbg from '../assets/theme_images/jqbg.jpg'
import myzcbg from '../assets/theme_images/myzcbg.jpg'
import chinaInk from '../assets/theme_images/china_ink.jpg'
import xnkl from '../assets/theme_images/xnkl.png'

// ---- 预设资源的“指纹”映射 ----
// 每次构建 hash 会变，但文件名（如 jqbg.jpg）不变。
// 从 localStorage 读取自定义主题时，如果 backgroundImage 看起来像旧构建的
// "/assets/jqbg.<旧hash>.jpg"，我们把它替换成当前构建的正确 URL。
const PRESET_ASSET_FINGERPRINT: Record<string, string> = {
  'landingMoon.png': landingMoon,
  'jqbg.jpg': jqbg,
  'myzcbg.jpg': myzcbg,
  'china_ink.jpg': chinaInk,
  'xnkl.png': xnkl,
}

/**
 * 检查一个 URL 是否是 Vite 静态资源路径（/assets/<name>.<hash>.<ext> 或 src/assets/...）。
 * 如果命中预设资源，返回当前构建的 URL；否则原样返回。
 */
function resolvePresetAsset(url: string | undefined | null): string | undefined {
  if (!url) return undefined
  // data URL 或 http(s) URL 不需要处理 —— 它们是用户自己上传/粘贴的
  if (url.startsWith('data:') || /^https?:\/\//i.test(url)) return url
  // 抽取文件名（含扩展名）：最后一个 '/' 之后，取 "<name>.xxx" 部分
  // 形如 "/assets/jqbg.74fbe3b6.jpg" → 希望抽出 "jqbg.jpg"
  const clean = url.split('?')[0].split('#')[0]
  const lastSlash = clean.lastIndexOf('/')
  const basename = lastSlash >= 0 ? clean.substring(lastSlash + 1) : clean
  if (!basename) return url
  // basename 形如 "jqbg.74fbe3b6.jpg" / "landingMoon.png"（开发模式未 hash）
  // 尝试直接匹配
  if (PRESET_ASSET_FINGERPRINT[basename]) return PRESET_ASSET_FINGERPRINT[basename]
  // 否则尝试剥离中间的 hash：<name>.<hash>.<ext> → <name>.<ext>
  const m = basename.match(/^(.+)\.([a-zA-Z0-9]{4,})\.([a-zA-Z0-9]{2,5})$/)
  if (m) {
    const canonical = `${m[1]}.${m[3]}`
    if (PRESET_ASSET_FINGERPRINT[canonical]) return PRESET_ASSET_FINGERPRINT[canonical]
  }
  // 再兜底：basename 可能只是 "myzcbg"（无扩展）—— 这种情况不处理，非预设资源
  return url
}

/**
 * 对一个自定义主题进行“预设资源 URL 修复”，返回新的对象。
 */
function repairCustomThemeAssets(c: CustomTheme): CustomTheme {
  if (!c.backgroundImage) return c
  const resolved = resolvePresetAsset(c.backgroundImage)
  if (resolved === c.backgroundImage) return c
  return { ...c, backgroundImage: resolved }
}

// ============================================================================
// 颜色工具
// ============================================================================
function clamp255(n: number): number {
  if (n < 0) return 0
  if (n > 255) return 255
  return Math.round(n)
}

function hexToRgb(hex: string): [number, number, number] {
  const h = (hex || '#000000').replace('#', '')
  const full = h.length === 3 ? h.split('').map(c => c + c).join('') : h
  return [
    parseInt(full.substring(0, 2), 16),
    parseInt(full.substring(2, 4), 16),
    parseInt(full.substring(4, 6), 16),
  ]
}

function rgbToHex(r: number, g: number, b: number): string {
  const to = (n: number) => clamp255(n).toString(16).padStart(2, '0')
  return `#${to(r)}${to(g)}${to(b)}`
}

export function hexToRgba(hex: string, alpha: number): string {
  const [r, g, b] = hexToRgb(hex)
  return `rgba(${r}, ${g}, ${b}, ${alpha})`
}

// 往白/亮方向混合 amount ∈ [0,1]
function lighten(hex: string, amount: number): string {
  const [r, g, b] = hexToRgb(hex)
  return rgbToHex(r + (255 - r) * amount, g + (255 - g) * amount, b + (255 - b) * amount)
}

// 往黑/暗方向混合 amount ∈ [0,1]
function darken(hex: string, amount: number): string {
  const [r, g, b] = hexToRgb(hex)
  return rgbToHex(r * (1 - amount), g * (1 - amount), b * (1 - amount))
}

// 计算颜色亮度（0-255），用于判断对比度
function luminance(hex: string): number {
  const [r, g, b] = hexToRgb(hex)
  return 0.299 * r + 0.587 * g + 0.114 * b
}

function hslToHex(hue: number, saturation: number, lightness: number): string {
  const h = ((hue % 360) + 360) % 360
  const s = Math.max(0, Math.min(100, saturation)) / 100
  const l = Math.max(0, Math.min(100, lightness)) / 100
  const c = (1 - Math.abs(2 * l - 1)) * s
  const x = c * (1 - Math.abs((h / 60) % 2 - 1))
  const m = l - c / 2
  let r = 0, g = 0, b = 0
  if (h < 60) [r, g, b] = [c, x, 0]
  else if (h < 120) [r, g, b] = [x, c, 0]
  else if (h < 180) [r, g, b] = [0, c, x]
  else if (h < 240) [r, g, b] = [0, x, c]
  else if (h < 300) [r, g, b] = [x, 0, c]
  else [r, g, b] = [c, 0, x]
  return rgbToHex((r + m) * 255, (g + m) * 255, (b + m) * 255)
}

type SemanticRole = 'success' | 'warning' | 'danger' | 'info'

/**
 * Semantic hues deliberately do not depend on the brand colour.  Rotating a
 * green (or red) primary colour made status meanings change between themes.
 * Only the lightness changes with the theme mode, so each role stays legible
 * and recognisable while still fitting the active surface.
 */
function semanticColor(role: SemanticRole, dark: boolean): string {
  const tones: Record<SemanticRole, [number, number, number, number, number]> = {
    success: [145, 70, 29, 67, 47],
    warning: [42, 90, 37, 91, 54],
    danger: [4, 74, 45, 84, 67],
    info: [218, 76, 46, 86, 67],
  }
  const [hue, lightSaturation, lightLightness, darkSaturation, darkLightness] = tones[role]
  return hslToHex(hue, dark ? darkSaturation : lightSaturation, dark ? darkLightness : lightLightness)
}

function solidHover(hex: string, dark: boolean): string {
  return dark ? lighten(hex, 0.12) : darken(hex, 0.12)
}

function softText(hex: string, dark: boolean): string {
  return dark ? lighten(hex, 0.32) : darken(hex, 0.36)
}

function relativeLuminance(hex: string): number {
  const [r, g, b] = hexToRgb(hex).map((value) => {
    const channel = value / 255
    return channel <= 0.04045 ? channel / 12.92 : Math.pow((channel + 0.055) / 1.055, 2.4)
  })
  return 0.2126 * r + 0.7152 * g + 0.0722 * b
}

function contrastRatio(a: string, b: string): number {
  const [lighter, darker] = [relativeLuminance(a), relativeLuminance(b)].sort((x, y) => y - x)
  return (lighter + 0.05) / (darker + 0.05)
}

/** Choose an accessible label colour for a user-selected accent colour. */
function contrastText(background: string): string {
  const white = contrastRatio(background, '#ffffff')
  const nearBlack = contrastRatio(background, '#111827')
  if (nearBlack >= 4.5 && nearBlack >= white) return '#111827'
  if (white >= 4.5) return '#ffffff'
  // The crossover between white and a softened black is the only narrow
  // contrast gap. Pure black keeps arbitrary user-picked solid colours AA.
  return '#000000'
}

// ============================================================================
// 类型
// ============================================================================
export interface ColorPalette {
  bgApp: string          // 应用背景 / 整体页面背景
  bgCard: string         // 内容卡片背景
  bgSidebar: string      // 侧边栏背景
  bgHover: string        // hover 背景
  bgInput: string        // 输入框背景
  border: string
  borderStrong: string
  textPrimary: string
  textSecondary: string
  textMuted: string
  accent: string         // 主/强调色
  accentDim: string      // 暗色版强调色
  accentContrast: string // 强调色上的文字
  accentAlpha10: string
  accentAlpha5: string
  accentAlpha15: string
  accentAlpha20: string
  accentAlpha25: string
  accentAlpha30: string
  accentAlpha35: string
  accentAlpha40: string
  accentRGB: string
  bgTag: string
  borderLight: string
  btnSolid: string
  btnSolidText: string
  btnSolidHover: string
  btnSolidAlpha20: string
  btnSolidAlpha35: string
  btnSoft: string
  btnSoftText: string
  tagHighlightBg: string
  tagHighlightText: string
  episodeBg: string
  episodeText: string
  carouselControl: string
  carouselControlText: string
  danger: string
  dangerHover: string
  dangerContrast: string
  dangerAlpha10: string
  success: string
  successHover: string
  successContrast: string
  successAlpha10: string
  warning: string
  warningHover: string
  warningText: string
  warningContrast: string
  warningAlpha10: string
  info: string
  infoHover: string
  infoContrast: string
  infoAlpha10: string
  shadow: string
  overlay: string
  btnHide: string
  btnHideContrast: string
  btnMin: string
  btnMinContrast: string
  btnClose: string
  btnCloseContrast: string
}

export interface CustomTheme {
  id: string
  name: string
  primary: string
  text: string
  background: string
  sidebar: string
  content: string
  btnHide: string
  btnMin: string
  btnClose: string
  /** Explicit primary action colour. Replaces the ambiguous legacy actionColor. */
  primaryAction?: string
  successColor?: string
  warningColor?: string
  dangerColor?: string
  infoColor?: string
  /** @deprecated Kept only to read themes saved by older versions. */
  actionColor?: string
  tagColor?: string
  episodeColor?: string
  carouselColor?: string
  backgroundImage?: string
  dark: boolean
  sidebarAlpha: number   // 侧边栏/面板背景透明度 0~1（有背景图时生效）
  contentAlpha: number   // 内容卡片背景透明度 0~1（有背景图时生效）
}

export interface PresetTheme {
  id: string
  /** 当前语言下的展示名（由 buildPresetThemes() 内的 tr() 求值，勿硬编码） */
  name: string
  primary: string
  palette: ColorPalette
  mode: 'dark' | 'light'
  bgImage?: string
}

// ============================================================================
// 调色板模板（由主色派生）
// ============================================================================
// 浅色模式：应用背景 = 主色的"很浅很淡"的色调；卡片 = 白色/近白
function buildLightPalette(primary: string, overrides: Partial<ColorPalette> = {}): ColorPalette {
  const success = semanticColor('success', false)
  const warning = semanticColor('warning', false)
  const danger = semanticColor('danger', false)
  const info = semanticColor('info', false)
  const base: ColorPalette = {
    bgApp: lighten(primary, 0.88),
    bgCard: '#ffffff',
    bgSidebar: lighten(primary, 0.93),
    bgHover: lighten(primary, 0.82),
    bgInput: '#ffffff',
    border: lighten(primary, 0.75),
    borderStrong: lighten(primary, 0.55),
    textPrimary: '#1f2430',
    textSecondary: '#4a5166',
    textMuted: '#8a92a6',
    accent: primary,
    accentDim: darken(primary, 0.15),
    accentContrast: contrastText(primary),
    accentRGB: hexToRgb(primary).join(', '),
    accentAlpha5: hexToRgba(primary, 0.05),
    accentAlpha10: hexToRgba(primary, 0.1),
    accentAlpha15: hexToRgba(primary, 0.15),
    accentAlpha20: hexToRgba(primary, 0.2),
    accentAlpha25: hexToRgba(primary, 0.25),
    accentAlpha30: hexToRgba(primary, 0.3),
    accentAlpha35: hexToRgba(primary, 0.35),
    accentAlpha40: hexToRgba(primary, 0.4),
    bgTag: lighten(primary, 0.9),
    borderLight: lighten(primary, 0.8),
    btnSolid: primary,
    btnSolidText: contrastText(primary),
    btnSolidHover: solidHover(primary, false),
    btnSolidAlpha20: hexToRgba(primary, 0.2),
    btnSolidAlpha35: hexToRgba(primary, 0.35),
    btnSoft: lighten(primary, 0.82),
    btnSoftText: softText(primary, false),
    tagHighlightBg: hexToRgba(primary, 0.18),
    tagHighlightText: darken(primary, 0.24),
    episodeBg: lighten(primary, 0.9), episodeText: darken(primary, 0.26),
    carouselControl: primary, carouselControlText: contrastText(primary),
    danger, dangerHover: solidHover(danger, false), dangerContrast: contrastText(danger), dangerAlpha10: hexToRgba(danger, 0.1),
    success, successHover: solidHover(success, false), successContrast: contrastText(success), successAlpha10: hexToRgba(success, 0.1),
    warning, warningHover: solidHover(warning, false), warningText: softText(warning, false), warningContrast: contrastText(warning), warningAlpha10: hexToRgba(warning, 0.1),
    info, infoHover: solidHover(info, false), infoContrast: contrastText(info), infoAlpha10: hexToRgba(info, 0.1),
    shadow: '0 6px 22px rgba(31, 36, 48, 0.08)',
    overlay: 'rgba(31, 36, 48, 0.45)',
    btnHide: info, btnHideContrast: contrastText(info),
    btnMin: warning, btnMinContrast: contrastText(warning),
    btnClose: danger,
    btnCloseContrast: contrastText(danger),
  }
  return { ...base, ...overrides }
}

// 深色模式：应用背景 = 主色的"很深很暗"的色调；卡片 = 深灰带一点主色
function buildDarkPalette(primary: string, overrides: Partial<ColorPalette> = {}): ColorPalette {
  const success = semanticColor('success', true)
  const warning = semanticColor('warning', true)
  const danger = semanticColor('danger', true)
  const info = semanticColor('info', true)
  const base: ColorPalette = {
    bgApp: darken(primary, 0.82),
    bgCard: darken(primary, 0.65),
    bgSidebar: darken(primary, 0.72),
    bgHover: darken(primary, 0.55),
    bgInput: darken(primary, 0.7),
    border: darken(primary, 0.5),
    borderStrong: lighten(darken(primary, 0.82), 0.1),
    textPrimary: '#f0f2f5',
    textSecondary: '#c2c7d0',
    textMuted: '#8a92a6',
    accent: primary,
    accentDim: lighten(primary, 0.15),
    accentContrast: contrastText(primary),
    accentRGB: hexToRgb(primary).join(', '),
    accentAlpha5: hexToRgba(primary, 0.08),
    accentAlpha10: hexToRgba(primary, 0.15),
    accentAlpha15: hexToRgba(primary, 0.2),
    accentAlpha20: hexToRgba(primary, 0.25),
    accentAlpha25: hexToRgba(primary, 0.3),
    accentAlpha30: hexToRgba(primary, 0.35),
    accentAlpha35: hexToRgba(primary, 0.45),
    accentAlpha40: hexToRgba(primary, 0.52),
    bgTag: lighten(darken(primary, 0.72), 0.08),
    borderLight: lighten(darken(primary, 0.72), 0.16),
    btnSolid: lighten(primary, 0.1),
    btnSolidText: contrastText(lighten(primary, 0.1)),
    btnSolidHover: solidHover(lighten(primary, 0.1), true),
    btnSolidAlpha20: hexToRgba(lighten(primary, 0.1), 0.25),
    btnSolidAlpha35: hexToRgba(lighten(primary, 0.1), 0.45),
    btnSoft: hexToRgba(primary, 0.26),
    btnSoftText: softText(primary, true),
    tagHighlightBg: hexToRgba(primary, 0.32),
    tagHighlightText: lighten(primary, 0.38),
    episodeBg: lighten(darken(primary, 0.72), 0.1), episodeText: lighten(primary, 0.42),
    carouselControl: lighten(primary, 0.1), carouselControlText: contrastText(lighten(primary, 0.1)),
    danger, dangerHover: solidHover(danger, true), dangerContrast: contrastText(danger), dangerAlpha10: hexToRgba(danger, 0.15),
    success, successHover: solidHover(success, true), successContrast: contrastText(success), successAlpha10: hexToRgba(success, 0.15),
    warning, warningHover: solidHover(warning, true), warningText: softText(warning, true), warningContrast: contrastText(warning), warningAlpha10: hexToRgba(warning, 0.15),
    info, infoHover: solidHover(info, true), infoContrast: contrastText(info), infoAlpha10: hexToRgba(info, 0.15),
    shadow: '0 8px 24px rgba(0, 0, 0, 0.45)',
    overlay: 'rgba(0, 0, 0, 0.7)',
    btnHide: info, btnHideContrast: contrastText(info),
    btnMin: warning, btnMinContrast: contrastText(warning),
    btnClose: danger,
    btnCloseContrast: contrastText(danger),
  }
  return { ...base, ...overrides }
}

// ============================================================================
// 预设主题（12 浅色 + 8 深色 = 20，部分主题带背景图）
// ============================================================================
// 注意：`name` 用 tr() 惰性求值，因此本函数只能由 store 的 `themes` computed
// 调用（读取时才翻译）。不要在模块顶层把它展开成常量数组，否则语言切换后
// 主题名会停留在首次求值时的语言。
function buildPresetThemes(): PresetTheme[] { return [
  // ==================== 浅色 ====================
  { id: 'green', name: tr('theme.presetGreen'), primary: '#16a34a', palette: buildLightPalette('#16a34a'), mode: 'light' },
  { id: 'blue', name: tr('theme.presetBlue'), primary: '#2e7dd7', palette: buildLightPalette('#2e7dd7'), mode: 'light' },
  { id: 'sky', name: tr('theme.presetSky'), primary: '#0284c7', palette: buildLightPalette('#0284c7'), mode: 'light' },
  { id: 'indigo', name: tr('theme.presetIndigo'), primary: '#4f46e5', palette: buildLightPalette('#4f46e5'), mode: 'light' },
  { id: 'orange', name: tr('theme.presetOrange'), primary: '#f59e0b', palette: buildLightPalette('#f59e0b'), mode: 'light' },
  { id: 'red', name: tr('theme.presetRed'), primary: '#e63946', palette: buildLightPalette('#e63946'), mode: 'light' },
  { id: 'pink', name: tr('theme.presetPink'), primary: '#ec4899', palette: buildLightPalette('#ec4899'), mode: 'light' },
  { id: 'purple', name: tr('theme.presetPurple'), primary: '#8b5cf6', palette: buildLightPalette('#8b5cf6'), mode: 'light' },
  { id: 'teal', name: tr('theme.presetTeal'), primary: '#14b8a6', palette: buildLightPalette('#14b8a6'), mode: 'light' },
  { id: 'cyan', name: tr('theme.presetCyan'), primary: '#06b6d4', palette: buildLightPalette('#06b6d4'), mode: 'light' },
  { id: 'lime', name: tr('theme.presetLime'), primary: '#84cc16', palette: buildLightPalette('#84cc16'), mode: 'light' },
  { id: 'gray', name: tr('theme.presetGray'), primary: '#64748b', palette: buildLightPalette('#64748b'), mode: 'light' },

  // 月里嫦娥（中秋背景图）
  {
    id: 'mid_autumn', name: tr('theme.presetMidAutumn'), primary: '#4a3b52', mode: 'dark',
    palette: buildLightPalette('#6d4c5d', {
      bgApp: '#f0ebe0',                  // 暖米色
      bgCard: 'rgba(189, 174, 182, 0.95)',
      bgSidebar: 'rgba(122, 100, 112, 0.92)',
      border: '#d9cfc0',
    }),
    bgImage: jqbg
  },

  // 木叶之村（海浪/森林）
  {
    id: 'naruto', name: tr('theme.presetNaruto'), primary: '#5790a7', mode: 'light',
    palette: buildLightPalette('#5790a7', {
      bgApp: '#e8eef1',
      bgCard: 'rgba(179, 205, 215, 0.95)',
      bgSidebar: 'rgba(154, 188, 202, 0.92)',
      border: '#c9d4db',
    }),
    bgImage: myzcbg
  },

  // 新年快乐（中国红背景）
  {
    id: 'happy_new_year', name: tr('theme.presetHappyNewYear'), primary: '#c0392b', mode: 'light',
    palette: buildLightPalette('#c0392b', {
      bgApp: '#f3e3d3',
      bgCard: 'rgba(227, 166, 160, 0.93)',
      bgSidebar: 'rgba(217, 136, 128, 0.90)',
      border: '#e0c4a8',
      accentContrast: contrastText('#c0392b'),
    }),
    bgImage: xnkl
  },

  // ==================== 深色 ====================
  { id: 'd-green', name: tr('theme.presetDarkGreen'), primary: '#22c55e', palette: buildDarkPalette('#22c55e'), mode: 'dark' },
  { id: 'd-blue', name: tr('theme.presetDarkBlue'), primary: '#60a5fa', palette: buildDarkPalette('#60a5fa'), mode: 'dark' },
  { id: 'd-violet', name: tr('theme.presetDarkViolet'), primary: '#a78bfa', palette: buildDarkPalette('#a78bfa'), mode: 'dark' },
  { id: 'd-rose', name: tr('theme.presetDarkRose'), primary: '#f472b6', palette: buildDarkPalette('#f472b6'), mode: 'dark' },
  { id: 'd-cyan', name: tr('theme.presetDarkCyan'), primary: '#22d3ee', palette: buildDarkPalette('#22d3ee'), mode: 'dark' },
  { id: 'd-indigo', name: tr('theme.presetDarkIndigo'), primary: '#818cf8', palette: buildDarkPalette('#818cf8'), mode: 'dark' },

  // 黑灯瞎火（月光背景图）
  {
    id: 'black', name: tr('theme.presetBlack'), primary: '#969696', mode: 'dark',
    palette: buildDarkPalette('#969696', {
      bgApp: '#12131a',
      bgCard: 'rgba(60, 60, 68, 0.92)',
      bgSidebar: 'rgba(40, 40, 48, 0.94)',
      textPrimary: '#e5e5e5',
      textSecondary: '#b0b5bd',
      border: 'rgba(255,255,255,0.08)',
      borderStrong: 'rgba(255,255,255,0.18)',
    }),
    bgImage: landingMoon
  },

  // 近墨者黑（水墨山水）
  {
    id: 'china_ink', name: tr('theme.presetChinaInk'), primary: '#2f2f2f', mode: 'light',
    palette: buildLightPalette('#2f2f2f', {
      bgApp: '#e8e8e4',
      bgCard: 'rgba(200, 200, 198, 0.94)',
      bgSidebar: 'rgba(180, 180, 178, 0.92)',
      textPrimary: '#1f2430',
      textSecondary: '#4a5166',
      border: '#d5d5cf',
    }),
    bgImage: chinaInk
  },
] }

// ============================================================================
// Store
// ============================================================================
export const useThemeStore = defineStore('theme', () => {
  const currentId = ref<string>('green')
  const customThemes = ref<CustomTheme[]>([])
  const loaded = ref(false)

  // 预设主题清单：computed 里调用 buildPresetThemes()，主题名才会跟随语言切换。
  const themes = computed<PresetTheme[]>(() => buildPresetThemes())

  // ---- 自定义主题 → 调色板 ----
  function paletteFromCustom(c: CustomTheme): ColorPalette {
    const base = c.dark ? buildDarkPalette(c.primary) : buildLightPalette(c.primary)
    const hasBg = !!c.backgroundImage
    // 有背景图时：用主色派生色 + 透明度，让背景色"带着主题味道"
    // 浅色模式：主色向白混合 40~60%；深色模式：主色向黑混合 40~55%
    const sidebarBase = hasBg
      ? (c.dark ? darken(c.primary, 0.55) : lighten(c.primary, 0.4))
      : (c.sidebar || base.bgSidebar)
    const contentBase = hasBg
      ? (c.dark ? darken(c.primary, 0.4) : lighten(c.primary, 0.6))
      : (c.content || base.bgCard)
    // 有背景图时转成半透明 rgba；无背景图时直接用纯色
    const effectiveSidebar = hasBg
      ? hexToRgba(sidebarBase, Math.max(0, Math.min(1, c.sidebarAlpha ?? 0.65)))
      : sidebarBase
    const effectiveContent = hasBg
      ? hexToRgba(contentBase, Math.max(0, Math.min(1, c.contentAlpha ?? 0.88)))
      : contentBase
    const action = c.primaryAction || c.actionColor || base.btnSolid
    const tag = c.tagColor || base.bgTag
    const episode = c.episodeColor || base.episodeBg
    const carousel = c.carouselColor || base.carouselControl
    const success = c.successColor || base.success
    const warning = c.warningColor || base.warning
    const danger = c.dangerColor || base.danger
    const info = c.infoColor || base.info
    return {
      ...base,
      bgApp: c.background || base.bgApp,
      bgCard: effectiveContent,
      bgSidebar: effectiveSidebar,
      accent: c.primary,
      accentDim: c.dark ? lighten(c.primary, 0.15) : darken(c.primary, 0.15),
      btnSolid: action,
      btnSolidText: contrastText(action),
      btnSolidHover: solidHover(action, c.dark),
      btnSolidAlpha20: hexToRgba(action, c.dark ? 0.25 : 0.2),
      btnSolidAlpha35: hexToRgba(action, c.dark ? 0.45 : 0.35),
      btnSoft: c.dark ? hexToRgba(action, 0.26) : lighten(action, 0.82),
      btnSoftText: softText(action, c.dark),
      bgTag: tag,
      tagHighlightBg: tag,
      tagHighlightText: contrastText(tag),
      episodeBg: episode,
      episodeText: contrastText(episode),
      carouselControl: carousel,
      carouselControlText: contrastText(carousel),
      danger,
      dangerHover: solidHover(danger, c.dark),
      dangerContrast: contrastText(danger),
      dangerAlpha10: hexToRgba(danger, c.dark ? 0.15 : 0.1),
      success,
      successHover: solidHover(success, c.dark),
      successContrast: contrastText(success),
      successAlpha10: hexToRgba(success, c.dark ? 0.15 : 0.1),
      warning,
      warningHover: solidHover(warning, c.dark),
      warningText: softText(warning, c.dark),
      warningContrast: contrastText(warning),
      warningAlpha10: hexToRgba(warning, c.dark ? 0.15 : 0.1),
      info,
      infoHover: solidHover(info, c.dark),
      infoContrast: contrastText(info),
      infoAlpha10: hexToRgba(info, c.dark ? 0.15 : 0.1),
      accentContrast: contrastText(c.primary),
      textPrimary: c.text || base.textPrimary,
      textSecondary: c.dark ? '#c2c7d0' : '#4a5166',
      textMuted: c.dark ? '#8a92a6' : '#8a92a6',
      btnHide: c.btnHide || base.btnHide,
      btnHideContrast: contrastText(c.btnHide || base.btnHide),
      btnMin: c.btnMin || base.btnMin,
      btnMinContrast: contrastText(c.btnMin || base.btnMin),
      btnClose: c.btnClose || base.btnClose,
      btnCloseContrast: contrastText(c.btnClose || base.btnClose),
      accentAlpha10: hexToRgba(c.primary, c.dark ? 0.15 : 0.1),
      accentAlpha20: hexToRgba(c.primary, c.dark ? 0.25 : 0.2),
      accentAlpha35: hexToRgba(c.primary, c.dark ? 0.45 : 0.35),
    }
  }

  // ---- 当前主题（computed）----
  const current = computed(() => {
    const preset = themes.value.find(t => t.id === currentId.value)
    const rawCustom = customThemes.value.find(c => c.id === currentId.value)
    // 如果是对预设的自定义覆盖，需要把可能过时的资源 URL 指向当前构建
    const custom = rawCustom ? repairCustomThemeAssets(rawCustom) : null
    if (custom) {
      return {
        id: custom.id,
        name: custom.name,
        mode: custom.dark ? 'dark' as const : 'light' as const,
        accent: custom.primary,
        palette: paletteFromCustom(custom),
        bgImage: custom.backgroundImage,
        isCustom: true,
      }
    }
    if (preset) {
      return {
        id: preset.id,
        name: preset.name,
        mode: preset.mode,
        accent: preset.primary,
        palette: preset.palette,
        bgImage: preset.bgImage,
        isCustom: false,
      }
    }
    // fallback
    return {
      id: themes.value[0].id,
      name: themes.value[0].name,
      mode: 'light' as const,
      accent: themes.value[0].primary,
      palette: themes.value[0].palette,
      bgImage: themes.value[0].bgImage,
      isCustom: false,
    }
  })

  // ---- 生命周期 ----
  async function load(): Promise<void> {
    try {
      const id = await GetSetting('theme_id')
      const customs = await GetSetting('theme_customs')
      if (typeof customs === 'string' && customs) {
        try {
          const parsed: CustomTheme[] = JSON.parse(customs)
          let migrated = false
          customThemes.value = Array.isArray(parsed)
            ? parsed.map((theme) => {
                const fixed = repairCustomThemeAssets(theme)
                if (initializeComponentTokens(fixed)) migrated = true
                return fixed
              })
            : []
          // Persist the one-time migration so old themes never require users
          // to press the derive button merely to obtain the new UI tokens.
          if (migrated) await SetSetting('theme_customs', JSON.stringify(customThemes.value))
        } catch { /* ignore */ }
      }
      // Custom themes are loaded above, so their stored id can now be resolved.
      if (typeof id === 'string' && id && (themes.value.some(t => t.id === id) || customThemes.value.some(c => c.id === id))) {
        currentId.value = id
      }
    } catch { /* ignore */ }
    loaded.value = true
    apply()
  }

  async function setTheme(id: string): Promise<void> {
    currentId.value = id
    apply()
    try { await SetSetting('theme_id', id) } catch { /* ignore */ }
    // 桥接到前端事件总线
    appEvent.emit('theme:changed', id)
  }

  // ---- 把调色板写入 CSS 变量 ----
  function apply(): void {
    const { palette, bgImage, mode } = current.value
    const root = document.documentElement
    root.setAttribute('data-theme', current.value.id)
    root.setAttribute('data-theme-mode', mode)

    const set = (k: string, v: string) => root.style.setProperty(k, v)

    set('--bg-primary', palette.bgApp)
    set('--bg-secondary', palette.bgSidebar)
    set('--bg-card', palette.bgCard)
    set('--bg-hover', palette.bgHover)
    set('--bg-input', palette.bgInput)
    set('--border', palette.border)
    set('--border-strong', palette.borderStrong)

    set('--text-primary', palette.textPrimary)
    set('--text-secondary', palette.textSecondary)
    set('--text-muted', palette.textMuted)

    set('--accent', palette.accent)
    set('--accent-dim', palette.accentDim)
    set('--accent-contrast', palette.accentContrast)
    set('--accent-rgb', palette.accentRGB)
    set('--accent-alpha-5', palette.accentAlpha5)
    set('--accent-alpha-10', palette.accentAlpha10)
    set('--accent-alpha-15', palette.accentAlpha15)
    set('--accent-alpha-20', palette.accentAlpha20)
    set('--accent-alpha-25', palette.accentAlpha25)
    set('--accent-alpha-30', palette.accentAlpha30)
    set('--accent-alpha-35', palette.accentAlpha35)
    set('--accent-alpha-40', palette.accentAlpha40)
    set('--bg-tag', palette.bgTag)
    set('--border-light', palette.borderLight)
    set('--btn-solid', palette.btnSolid)
    set('--btn-solid-text', palette.btnSolidText)
    set('--btn-solid-hover', palette.btnSolidHover)
    set('--btn-solid-alpha-20', palette.btnSolidAlpha20)
    set('--btn-solid-alpha-35', palette.btnSolidAlpha35)
    set('--btn-soft', palette.btnSoft)
    set('--btn-soft-text', palette.btnSoftText)
    set('--tag-highlight-bg', palette.tagHighlightBg)
    set('--tag-highlight-text', palette.tagHighlightText)
    set('--episode-bg', palette.episodeBg)
    set('--episode-text', palette.episodeText)
    set('--carousel-control', palette.carouselControl)
    set('--carousel-control-text', palette.carouselControlText)

    set('--danger', palette.danger)
    set('--danger-hover', palette.dangerHover)
    set('--danger-contrast', palette.dangerContrast)
    set('--danger-alpha-10', palette.dangerAlpha10)
    set('--success', palette.success)
    set('--success-hover', palette.successHover)
    set('--success-contrast', palette.successContrast)
    set('--success-alpha-10', palette.successAlpha10)
    set('--warning', palette.warning)
    set('--warning-hover', palette.warningHover)
    set('--warning-text', palette.warningText)
    set('--warning-contrast', palette.warningContrast)
    set('--warning-alpha-10', palette.warningAlpha10)
    set('--info', palette.info)
    set('--info-hover', palette.infoHover)
    set('--info-contrast', palette.infoContrast)
    set('--info-alpha-10', palette.infoAlpha10)

    set('--shadow', palette.shadow)
    set('--overlay', palette.overlay)

    set('--btn-hide', palette.btnHide)
    set('--btn-hide-contrast', palette.btnHideContrast)
    set('--btn-min', palette.btnMin)
    set('--btn-min-contrast', palette.btnMinContrast)
    set('--btn-close', palette.btnClose)
    set('--btn-close-contrast', palette.btnCloseContrast)

    if (bgImage) {
      set('--bg-image', `url("${bgImage}")`)
    } else {
      root.style.removeProperty('--bg-image')
    }

    root.style.backgroundColor = palette.bgApp
    root.style.color = palette.textPrimary
    root.style.colorScheme = mode
    document.body.style.backgroundColor = palette.bgApp
    document.body.style.color = palette.textPrimary
  }

  // ---- 自定义主题 CRUD ----
  async function saveCustom(theme: CustomTheme): Promise<void> {
    // 保存前把可能是旧构建的预设资源 URL 替换为当前构建的正确 URL
    const fixed = repairCustomThemeAssets(theme)
    const i = customThemes.value.findIndex(c => c.id === fixed.id)
    if (i >= 0) customThemes.value[i] = fixed
    else customThemes.value.push(fixed)
    persistCustoms()
    await setTheme(fixed.id)
  }

  async function deleteCustom(id: string): Promise<void> {
    customThemes.value = customThemes.value.filter(c => c.id !== id)
    persistCustoms()
    if (currentId.value === id) await setTheme('green')
  }

  async function renameCustom(id: string, name: string): Promise<void> {
    const t = customThemes.value.find(c => c.id === id)
    if (!t) return
    t.name = name
    persistCustoms()
    apply()
  }

  function persistCustoms(): void {
    try { SetSetting('theme_customs', JSON.stringify(customThemes.value)) } catch { /* ignore */ }
  }

  function makeEmptyCustom(): CustomTheme {
    const palette = buildLightPalette('#16a34a')
    return {
      id: `custom_${Date.now()}`,
      name: tr('theme.myTheme'),
      primary: '#16a34a',
      text: '#1f2430',
      background: lighten('#16a34a', 0.88),
      sidebar: lighten('#16a34a', 0.93),
      content: '#ffffff',
      btnHide: palette.btnHide,
      btnMin: palette.btnMin,
      btnClose: palette.btnClose,
      primaryAction: palette.btnSolid,
      successColor: palette.success,
      warningColor: palette.warning,
      dangerColor: palette.danger,
      infoColor: palette.info,
      tagColor: palette.bgTag,
      episodeColor: palette.episodeBg,
      carouselColor: palette.carouselControl,
      dark: false,
      sidebarAlpha: 0.65,
      contentAlpha: 0.88,
    }
  }

  // 根据主色派生整套配色
  function deriveFromPrimary(primary: string, dark: boolean): CustomTheme {
    const palette = dark ? buildDarkPalette(primary) : buildLightPalette(primary)
    return {
      id: `custom_${Date.now()}`,
      name: dark ? tr('theme.darkDerived') : tr('theme.lightDerived'),
      primary,
      text: palette.textPrimary,
      background: palette.bgApp,
      sidebar: palette.bgSidebar,
      content: palette.bgCard,
      btnHide: palette.btnHide,
      btnMin: palette.btnMin,
      btnClose: palette.btnClose,
      primaryAction: palette.btnSolid,
      successColor: palette.success,
      warningColor: palette.warning,
      dangerColor: palette.danger,
      infoColor: palette.info,
      tagColor: palette.bgTag,
      episodeColor: palette.episodeBg,
      carouselColor: palette.carouselControl,
      dark,
      sidebarAlpha: 0.65,
      contentAlpha: 0.88,
    }
  }

  /** Add only newly introduced component tokens; never replace user choices. */
  function initializeComponentTokens(theme: CustomTheme): boolean {
    const derived = deriveFromPrimary(theme.primary, theme.dark)
    let changed = false
    const defaults: Pick<CustomTheme, 'primaryAction' | 'successColor' | 'warningColor' | 'dangerColor' | 'infoColor' | 'tagColor' | 'episodeColor' | 'carouselColor'> = {
      primaryAction: theme.primaryAction || theme.actionColor || derived.primaryAction,
      successColor: derived.successColor,
      warningColor: derived.warningColor,
      dangerColor: derived.dangerColor,
      infoColor: derived.infoColor,
      tagColor: derived.tagColor,
      episodeColor: derived.episodeColor,
      carouselColor: derived.carouselColor,
    }
    for (const [key, value] of Object.entries(defaults) as Array<[keyof typeof defaults, string | undefined]>) {
      if (!theme[key] && value) {
        theme[key] = value
        changed = true
      }
    }
    return changed
  }

  watch([currentId, customThemes], () => apply(), { deep: true })

  return {
    themes,
    currentId,
    current,
    customThemes,
    loaded,
    load,
    setTheme,
    apply,
    saveCustom,
    deleteCustom,
    renameCustom,
    makeEmptyCustom,
    deriveFromPrimary,
    resolvePresetAsset,
  }
})

export { luminance }
