import { defineStore } from 'pinia'
import { ref, computed, watch } from 'vue'
import { GetSetting, SetSetting } from '../api/app'
import { tr } from '../locales'
import { usePluginStore } from './plugins'
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

// ---- 扩展包（kind: theme）背景图 ----
// 主题包里的背景图绝不能变成 data URL 存进数据库：自定义主题是 theme_customs 一整行 JSON，
// 塞进几 MB 的 base64 会让每次读写都拖着这坨字节。所以持久化的永远是引用串
// `pack://<包id>/<包内路径>`，只有渲染到 --bg-image 那一刻才换成 ReadPluginAsset 的 data URL。
const PACK_REF_PREFIX = 'pack://'

/**
 * manifest 里的包内相对路径 → 引用串。顺手归一化（反斜杠、开头的 ./），
 * 让 `bg/a.png`、`./bg/a.png`、`bg\a.png` 三种写法指向同一张图，
 * 也让「存进 theme_customs 的串」和「预热缓存里的键」完全一致。
 */
export function packBgRef(packId: string, path: string): string {
  const clean = (path || '').trim().replace(/\\/g, '/').replace(/^\.?\//, '')
  return `${PACK_REF_PREFIX}${packId}/${clean}`
}

/** 这个背景图值是扩展包引用吗？（区别于打包资源路径与用户自己粘贴的 data URL / http URL） */
export function isPackAssetRef(url: string | undefined | null): boolean {
  return !!url && url.startsWith(PACK_REF_PREFIX)
}

/**
 * 引用串 → 已解析的 data URL。由 warmPackBackgrounds() 填充（读包是异步的），
 * 空串表示读过但失败了。包被删掉、被停用或文件读不出来时这里就是空的，
 * 主题按「无背景图」降级。
 * 放在模块层而不是 store 里：resolvePresetAsset 也要读它，两份缓存只会让其中一份变成死代码。
 */
const packAssets = ref<Record<string, string>>({})

/**
 * 检查一个 URL 是否是 Vite 静态资源路径（/assets/<name>.<hash>.<ext> 或 src/assets/...）。
 * 如果命中预设资源，返回当前构建的 URL；否则原样返回。
 * 扩展包引用（pack://）走另一条路：只查已缓存的 data URL，查不到就返回 undefined，
 * 绝不把引用串本身交给 url()，那样只会画出一张破图。
 */
function resolvePresetAsset(url: string | undefined | null): string | undefined {
  if (!url) return undefined
  if (isPackAssetRef(url)) return packAssets.value[url] || undefined
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
 * 扩展包引用不参与修复：它不是构建产物路径，下面那套「剥掉中间 hash」的规则会把
 * `theme-dusk/dusk.png` 读成「name=theme-dusk/dusk、ext=png」并改写它，一条引用串
 * 就此作废。它只在渲染时经 resolvePresetAsset 换成 data URL。
 */
function repairCustomThemeAssets(c: CustomTheme): CustomTheme {
  if (!c.backgroundImage || isPackAssetRef(c.backgroundImage)) return c
  const resolved = resolvePresetAsset(c.backgroundImage)
  if (resolved === c.backgroundImage) return c
  return { ...c, backgroundImage: resolved }
}

/**
 * 一个主题最终能画出来的那张背景图：没有背景图、或者扩展包引用还没解析出来时返回
 * undefined —— 调用方据此走「无背景图」那条路，绝不拼出 `url("pack://…")` 这种破图。
 * 非引用值（预设资源路径、用户上传的 data URL、http URL）交给 resolvePresetAsset 处理。
 */
export function resolveThemeBg(value: string | undefined | null): string | undefined {
  return value ? resolvePresetAsset(value) : undefined
}

/**
 * 把扩展包声明的背景图预热进 packAssets：读包是异步的，先把键占成空串免得重复戳同一个
 * 坏文件，读回来再填 data URL。由 plugin store 的 themePresets 变化驱动（注册表首次加载、
 * 重扫、包被启停都会变），所以启动时不需要谁手动调；打开扩展面板时也可以催一次。
 * 已经不在册的引用要清掉，否则包改了背景路径或被人删掉后，主题还会画着那张过期的图。
 */
export async function warmPackBackgrounds(): Promise<void> {
  const pluginStore = usePluginStore()
  const live = new Set<string>()
  for (const { packId, preset } of pluginStore.themePresets) {
    if (!preset.bg_image) continue
    const ref = packBgRef(packId, preset.bg_image)
    live.add(ref)
    if (ref in packAssets.value) continue
    packAssets.value[ref] = ''
    void pluginStore.readAsset(packId, preset.bg_image).then((url) => {
      packAssets.value[ref] = url || ''
    })
  }
  for (const key of Object.keys(packAssets.value)) {
    if (!live.has(key)) delete packAssets.value[key]
  }
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

/** 读取 `#rgb` / `#rrggbb` / `rgb()` / `rgba()` 三通道，方便对半透明表面算对比度。 */
function readRgb(color: string): [number, number, number] {
  const m = color.match(/rgba?\(\s*(\d+),\s*(\d+),\s*(\d+)/)
  if (m) return [Number(m[1]), Number(m[2]), Number(m[3])]
  return hexToRgb(color)
}

/** 存进编辑器的表面色必须是纯色：rgba 串绑到 `<input type="color">` 会显示成黑色。 */
function toSolidHex(color: string): string {
  const [r, g, b] = readRgb(color)
  return rgbToHex(r, g, b)
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

/** 色相角（0-359）；无彩色（灰/黑/白）返回 -1。 */
function hueOf(color: string): number {
  const [r, g, b] = hexToRgb(color).map((v) => v / 255)
  const max = Math.max(r, g, b)
  const min = Math.min(r, g, b)
  const delta = max - min
  if (delta === 0) return -1
  let hue: number
  if (max === r) hue = 60 * (((g - b) / delta) % 6)
  else if (max === g) hue = 60 * ((b - r) / delta + 2)
  else hue = 60 * ((r - g) / delta + 4)
  return ((hue % 360) + 360) % 360
}

/** 角色色相与主色相在色环上的最短夹角（0-180）；任一色为无彩色时视为 180（不会撞色）。 */
function hueGap(hue: number, primary: string): number {
  const other = primary ? hueOf(primary) : -1
  if (other < 0) return 180
  const normalized = ((hue % 360) + 360) % 360
  const distance = Math.abs(other - normalized) % 360
  return distance > 180 ? 360 - distance : distance
}

/**
 * Semantic hues deliberately do not depend on the brand colour.  Rotating a
 * green (or red) primary colour made status meanings change between themes.
 * Only the lightness changes with the theme mode, so each role stays legible
 * and recognisable while still fitting the active surface.
 *
 * `primary` is used for separation only: when a role colour sits within 18° of
 * the brand colour (the default green theme collides head-on with `success`)
 * the role keeps its hue — meaning must never rotate — and is pulled apart
 * along lightness/saturation instead: brighter on dark themes, deeper on light
 * ones.  Fully deterministic: the closer the hues, the larger the capped shift.
 */
function semanticColor(role: SemanticRole, dark: boolean, primary = ''): string {
  const tones: Record<SemanticRole, [number, number, number, number, number]> = {
    success: [145, 70, 29, 67, 47],
    warning: [42, 90, 37, 91, 54],
    danger: [4, 74, 45, 84, 67],
    info: [218, 76, 46, 86, 67],
  }
  const [hue, lightSaturation, lightLightness, darkSaturation, darkLightness] = tones[role]
  const overlap = 18 - hueGap(hue, primary)
  if (overlap > 0) {
    const strength = overlap / 18
    if (dark) return hslToHex(hue, darkSaturation - strength * 10, darkLightness + strength * 16)
    return hslToHex(hue, lightSaturation + strength * 8, lightLightness - strength * 12)
  }
  return hslToHex(hue, dark ? darkSaturation : lightSaturation, dark ? darkLightness : lightLightness)
}

function solidHover(hex: string, dark: boolean): string {
  return dark ? lighten(hex, 0.12) : darken(hex, 0.12)
}

function softText(hex: string, dark: boolean): string {
  return dark ? lighten(hex, 0.32) : darken(hex, 0.36)
}

function relativeLuminance(color: string): number {
  const [r, g, b] = readRgb(color).map((value) => {
    const channel = value / 255
    return channel <= 0.04045 ? channel / 12.92 : Math.pow((channel + 0.055) / 1.055, 2.4)
  })
  return 0.2126 * r + 0.7152 * g + 0.0722 * b
}

function contrastRatio(a: string, b: string): number {
  const [lighter, darker] = [relativeLuminance(a), relativeLuminance(b)].sort((x, y) => y - x)
  return (lighter + 0.05) / (darker + 0.05)
}

/**
 * `textMuted` can never be one fixed grey: a mid grey that looks right on a
 * dark card fails WCAG AA on a white one (and vice versa).  The seed follows
 * the mode, the walk follows the *actual* card surface — brighter when the
 * surface is dark, deeper when it is light — until it clears 4.5:1.  Bounded
 * and monotonic, so the same inputs always yield the same output, and a
 * mismatched surface (a bright card picked inside a dark theme) can never be
 * pushed the wrong way into invisibility.
 */
function mutedOn(surface: string, dark: boolean): string {
  const towardsDark = contrastRatio('#000000', surface) > contrastRatio('#ffffff', surface)
  let candidate = dark ? '#8a92a6' : '#7c8598'
  for (let step = 0; step < 24 && contrastRatio(surface, candidate) < 4.5; step += 1) {
    candidate = towardsDark ? darken(candidate, 0.05) : lighten(candidate, 0.05)
  }
  return candidate
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
  shadowSm: string
  shadow: string
  shadowLg: string
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
  /** Explicit primary action colour. Replaces the ambiguous legacy actionColor. */
  primaryAction?: string
  successColor?: string
  warningColor?: string
  dangerColor?: string
  infoColor?: string
  /** @deprecated Kept only to read themes saved by older versions. */
  actionColor?: string
  tagColor?: string
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
  /**
   * 预设手工调过的那层表面色。自定义覆盖走 paletteFromCustom 时只会按 primary
   * 重算，拿不到这层，编辑一次暖灰就退化成通用灰 —— 所以单独存出来重新垫回去。
   */
  tint?: Partial<ColorPalette>
  /** 来自扩展包的预设才有：所属包 id，设置页用它标注「来自扩展包」。 */
  packId?: string
}

/**
 * 扩展包主题的稳定标识。theme_id 里存 `pack:<包>/<预设>`，
 * theme_customs 的背景字段存 `pack://<包>/<包内相对路径>` —— 两者都只是引用，
 * 图片本体留在盘上按需读，绝不做进 SQLite 那一行的 data URL。
 */
export function packThemeId(packId: string, presetId: string): string {
  return `pack:${packId}/${presetId}`
}

// ============================================================================
// 调色板模板（由主色派生）
// ============================================================================
// 浅色模式：应用背景 = 主色的"很浅很淡"的色调；卡片 = 白色/近白
function buildLightPalette(primary: string, overrides: Partial<ColorPalette> = {}): ColorPalette {
  const success = semanticColor('success', false, primary)
  const warning = semanticColor('warning', false, primary)
  const danger = semanticColor('danger', false, primary)
  const info = semanticColor('info', false, primary)
  // 标签和剧集底色本来就同值（都是 lighten 0.9），合成一份，编辑器里也只剩一个框。
  const bgTag = lighten(primary, 0.9)
  // 轮播控件压在海报图上，要比主操作按钮再深一档，否则和主色完全同值、取色框形同虚设。
  const carousel = darken(primary, 0.18)
  const base: ColorPalette = {
    bgApp: lighten(primary, 0.88),
    bgCard: '#ffffff',
    bgSidebar: lighten(primary, 0.93),
    // bgHover 与 btnSoft 必须是两档：soft 按钮/色片 hover 到 bgHover 时要看得出变化。
    bgHover: lighten(primary, 0.86),
    bgInput: '#ffffff',
    border: lighten(primary, 0.75),
    borderStrong: lighten(primary, 0.55),
    textPrimary: '#1f2430',
    textSecondary: '#4a5166',
    // textMuted 在下面的 finalize 里按真实卡片表面补足 AA 对比度。
    textMuted: '#7c8598',
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
    bgTag,
    borderLight: lighten(primary, 0.8),
    btnSolid: primary,
    btnSolidText: contrastText(primary),
    btnSolidHover: solidHover(primary, false),
    btnSolidAlpha20: hexToRgba(primary, 0.2),
    btnSolidAlpha35: hexToRgba(primary, 0.35),
    btnSoft: lighten(primary, 0.7),
    btnSoftText: softText(primary, false),
    tagHighlightBg: hexToRgba(primary, 0.18),
    tagHighlightText: darken(primary, 0.24),
    episodeBg: bgTag, episodeText: darken(primary, 0.26),
    carouselControl: carousel, carouselControlText: contrastText(carousel),
    danger, dangerHover: solidHover(danger, false), dangerContrast: contrastText(danger), dangerAlpha10: hexToRgba(danger, 0.1),
    success, successHover: solidHover(success, false), successContrast: contrastText(success), successAlpha10: hexToRgba(success, 0.1),
    warning, warningHover: solidHover(warning, false), warningText: softText(warning, false), warningContrast: contrastText(warning), warningAlpha10: hexToRgba(warning, 0.1),
    info, infoHover: solidHover(info, false), infoContrast: contrastText(info), infoAlpha10: hexToRgba(info, 0.1),
    // 浅色主题的阴影带主色倾向，避免纯黑阴影在浅底上发脏。
    shadowSm: `0 1px 3px ${hexToRgba(darken(primary, 0.4), 0.14)}`,
    shadow: '0 6px 22px rgba(31, 36, 48, 0.08)',
    shadowLg: `0 12px 48px ${hexToRgba(darken(primary, 0.6), 0.24)}`,
    overlay: 'rgba(31, 36, 48, 0.45)',
    btnHide: info, btnHideContrast: contrastText(info),
    btnMin: warning, btnMinContrast: contrastText(warning),
    btnClose: danger,
    btnCloseContrast: contrastText(danger),
  }
  return finalizePalette({ ...base, ...overrides }, overrides, false)
}

// 深色模式：应用背景 = 主色的"很深很暗"的色调；卡片 = 深灰带一点主色
function buildDarkPalette(primary: string, overrides: Partial<ColorPalette> = {}): ColorPalette {
  const success = semanticColor('success', true, primary)
  const warning = semanticColor('warning', true, primary)
  const danger = semanticColor('danger', true, primary)
  const info = semanticColor('info', true, primary)
  const border = darken(primary, 0.5)
  const bgTag = lighten(darken(primary, 0.72), 0.08)
  // 深色下轮播控件要比主操作按钮更亮一档，压在暗海报上才分得开。
  const carousel = lighten(primary, 0.22)
  const base: ColorPalette = {
    bgApp: darken(primary, 0.82),
    bgCard: darken(primary, 0.65),
    bgSidebar: darken(primary, 0.72),
    bgHover: darken(primary, 0.55),
    bgInput: darken(primary, 0.7),
    border,
    // borderStrong 必须比 border 更亮更醒目（滚动条、强调描边都吃这个值）。
    // 旧实现 lighten(darken(primary, .82), .1) 反而比 border 更暗，层级倒挂。
    borderStrong: lighten(border, 0.22),
    textPrimary: '#f0f2f5',
    textSecondary: '#c2c7d0',
    // textMuted 在下面的 finalize 里按真实卡片表面补足 AA 对比度。
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
    bgTag,
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
    episodeBg: bgTag, episodeText: lighten(primary, 0.42),
    carouselControl: carousel, carouselControlText: contrastText(carousel),
    danger, dangerHover: solidHover(danger, true), dangerContrast: contrastText(danger), dangerAlpha10: hexToRgba(danger, 0.15),
    success, successHover: solidHover(success, true), successContrast: contrastText(success), successAlpha10: hexToRgba(success, 0.15),
    warning, warningHover: solidHover(warning, true), warningText: softText(warning, true), warningContrast: contrastText(warning), warningAlpha10: hexToRgba(warning, 0.15),
    info, infoHover: solidHover(info, true), infoContrast: contrastText(info), infoAlpha10: hexToRgba(info, 0.15),
    // 深色主题的阴影用更深的黑，浅阴影在黑底上等于没有。
    shadowSm: '0 1px 3px rgba(0, 0, 0, 0.5)',
    shadow: '0 8px 24px rgba(0, 0, 0, 0.45)',
    shadowLg: '0 12px 48px rgba(0, 0, 0, 0.72)',
    overlay: 'rgba(0, 0, 0, 0.7)',
    btnHide: info, btnHideContrast: contrastText(info),
    btnMin: warning, btnMinContrast: contrastText(warning),
    btnClose: danger,
    btnCloseContrast: contrastText(danger),
  }
  return finalizePalette({ ...base, ...overrides }, overrides, true)
}

/**
 * 覆盖合并后再算 muted：预设常常把 bgCard 改成带色调的半透明表面，
 * 在 base 上算出来的灰在那种表面上并不达标。显式覆盖 muted 的主题优先。
 */
function finalizePalette(palette: ColorPalette, overrides: Partial<ColorPalette>, dark: boolean): ColorPalette {
  if (overrides.textMuted !== undefined) return palette
  return { ...palette, textMuted: mutedOn(palette.bgCard, dark) }
}

// 扩展包 manifest 里的 tint 只允许改写上表真实存在的通道：Go 侧的键名正则只管字符集
// （docs §5），不认识通道名，照单全收会让一个拼错的通道一路混进 palette、混进
// paletteFromCustom 的再垫底，最后变成一个谁也读不到的脏字段。
const PALETTE_KEY_SET: ReadonlySet<string> = new Set(Object.keys(buildLightPalette('#808080')))

/** 只留下认识的通道名与非空色值；一个都不认识时返回 undefined，等价于「这个预设没有 tint」。 */
function sanitizeTint(tint?: Record<string, string>): Partial<ColorPalette> | undefined {
  if (!tint) return undefined
  const out: Record<string, string> = {}
  for (const [key, value] of Object.entries(tint)) {
    if (value && PALETTE_KEY_SET.has(key)) out[key] = value
  }
  return Object.keys(out).length > 0 ? (out as Partial<ColorPalette>) : undefined
}

// ============================================================================
// 预设主题（12 浅色 + 8 深色 = 20，部分主题带背景图）
// ============================================================================
// 带背景图的 5 个预设手工调过表面色。这份 tint 既喂给 buildXxxPalette 当 overrides，
// 也留给 paletteFromCustom 重新垫底 —— 两份必须是同一份，否则编辑前后不是同一个主题。
const TINT_MOON_CAKE: Partial<ColorPalette> = {
  bgApp: '#f0ebe0',                  // 暖米色
  bgCard: 'rgba(189, 174, 182, 0.95)',
  bgSidebar: 'rgba(122, 100, 112, 0.92)',
  border: '#d9cfc0',
}
const TINT_LEAF_VILLAGE: Partial<ColorPalette> = {
  bgApp: '#e8eef1',
  bgCard: 'rgba(179, 205, 215, 0.95)',
  bgSidebar: 'rgba(154, 188, 202, 0.92)',
  border: '#c9d4db',
}
const TINT_NEW_YEAR: Partial<ColorPalette> = {
  bgApp: '#f3e3d3',
  bgCard: 'rgba(227, 166, 160, 0.93)',
  bgSidebar: 'rgba(217, 136, 128, 0.90)',
  border: '#e0c4a8',
  accentContrast: contrastText('#c0392b'),
}
const TINT_MOONLIT: Partial<ColorPalette> = {
  bgApp: '#12131a',
  bgCard: 'rgba(60, 60, 68, 0.92)',
  bgSidebar: 'rgba(40, 40, 48, 0.94)',
  textPrimary: '#e5e5e5',
  textSecondary: '#b0b5bd',
  border: 'rgba(255,255,255,0.08)',
  borderStrong: 'rgba(255,255,255,0.18)',
}
const TINT_INK: Partial<ColorPalette> = {
  bgApp: '#e8e8e4',
  bgCard: 'rgba(200, 200, 198, 0.94)',
  bgSidebar: 'rgba(180, 180, 178, 0.92)',
  textPrimary: '#1f2430',
  textSecondary: '#4a5166',
  border: '#d5d5cf',
}

/** 从 tint 里那层 rgba 表面读出作者用的透明度，编辑器据此播种透明度滑块。 */
export function readSurfaceAlpha(color: string | undefined): number | undefined {
  if (!color) return undefined
  const m = color.match(/rgba\(\s*[\d.]+,\s*[\d.]+,\s*[\d.]+,\s*([\d.]+)\s*\)/)
  return m ? Number(m[1]) : undefined
}

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
    id: 'mid_autumn', name: tr('theme.presetMidAutumn'), primary: '#6d4c5d', mode: 'light',
    tint: TINT_MOON_CAKE,
    palette: buildLightPalette('#6d4c5d', TINT_MOON_CAKE),
    bgImage: jqbg
  },

  // 木叶之村（海浪/森林）
  {
    id: 'naruto', name: tr('theme.presetNaruto'), primary: '#5790a7', mode: 'light',
    tint: TINT_LEAF_VILLAGE,
    palette: buildLightPalette('#5790a7', TINT_LEAF_VILLAGE),
    bgImage: myzcbg
  },

  // 新年快乐（中国红背景）
  {
    id: 'happy_new_year', name: tr('theme.presetHappyNewYear'), primary: '#c0392b', mode: 'light',
    tint: TINT_NEW_YEAR,
    palette: buildLightPalette('#c0392b', TINT_NEW_YEAR),
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
    tint: TINT_MOONLIT,
    palette: buildDarkPalette('#969696', TINT_MOONLIT),
    bgImage: landingMoon
  },

  // 近墨者黑（水墨山水）
  {
    id: 'china_ink', name: tr('theme.presetChinaInk'), primary: '#2f2f2f', mode: 'light',
    tint: TINT_INK,
    palette: buildLightPalette('#2f2f2f', TINT_INK),
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
  const pluginStore = usePluginStore()
  /** 启动时扩展包还没扫到、当前查不到的 pack 主题 id，注册表到货后认回来。 */
  const deferredThemeId = ref('')

  // 扩展包预设：manifest 已由 Go 侧校验过，这里只按同一套派生规则算出调色板。
  function buildPackThemes(): PresetTheme[] {
    const out: PresetTheme[] = []
    for (const { packId, preset } of pluginStore.themePresets) {
      const tint = sanitizeTint(preset.tint)
      const palette = preset.mode === 'dark'
        ? buildDarkPalette(preset.primary, tint ?? {})
        : buildLightPalette(preset.primary, tint ?? {})
      out.push({
        id: packThemeId(packId, preset.id),
        name: pluginStore.localized(preset.name) || preset.id,
        primary: preset.primary,
        mode: preset.mode === 'dark' ? 'dark' : 'light',
        palette,
        tint,
        bgImage: preset.bg_image ? packBgRef(packId, preset.bg_image) : undefined,
        packId,
      })
    }
    return out
  }

  // 预设主题清单：computed 里调用 buildPresetThemes()，主题名才会跟随语言切换。
  const themes = computed<PresetTheme[]>(() => [...buildPresetThemes(), ...buildPackThemes()])

  // ---- 自定义主题 → 调色板 ----
  function paletteFromCustom(c: CustomTheme): ColorPalette {
    // 预设手工调的那层 tint 先垫回派生结果：border、textSecondary 这些没有取色框，
    // 只按 primary 重算一遍就会把暖灰手感抹成通用灰。
    const tint = themes.value.find(t => t.id === c.id)?.tint
    const base: ColorPalette = {
      ...(c.dark ? buildDarkPalette(c.primary) : buildLightPalette(c.primary)),
      ...(tint || {}),
    }
    const hasBg = !!resolveThemeBg(c.backgroundImage)
    // 用户挑过的颜色永远优先；没挑过才退回"按主色染一层底"（浅色向白 40~60%，深色向黑 40~55%）。
    const sidebarBase = c.sidebar || (hasBg ? (c.dark ? darken(c.primary, 0.55) : lighten(c.primary, 0.4)) : base.bgSidebar)
    const contentBase = c.content || (hasBg ? (c.dark ? darken(c.primary, 0.4) : lighten(c.primary, 0.6)) : base.bgCard)
    // 有背景图时转成半透明 rgba；无背景图时直接用纯色
    const effectiveSidebar = hasBg
      ? hexToRgba(sidebarBase, Math.max(0, Math.min(1, c.sidebarAlpha ?? 0.65)))
      : sidebarBase
    const effectiveContent = hasBg
      ? hexToRgba(contentBase, Math.max(0, Math.min(1, c.contentAlpha ?? 0.88)))
      : contentBase
    const action = c.primaryAction || c.actionColor || base.btnSolid
    const tag = c.tagColor || base.bgTag
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
      btnSoft: c.dark ? hexToRgba(action, 0.26) : lighten(action, 0.7),
      btnSoftText: softText(action, c.dark),
      bgTag: tag,
      tagHighlightBg: tag,
      tagHighlightText: contrastText(tag),
      episodeBg: tag,
      episodeText: contrastText(tag),
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
      textSecondary: base.textSecondary,
      textMuted: mutedOn(effectiveContent, c.dark),
      // 窗口三颗按钮就是语义三色：以前各存一份，改语义色时标题栏不跟，会静默分叉。
      btnHide: info, btnHideContrast: contrastText(info),
      btnMin: warning, btnMinContrast: contrastText(warning),
      btnClose: danger, btnCloseContrast: contrastText(danger),
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
      } else if (typeof id === 'string' && id.startsWith('pack:')) {
        // 扩展包主题：注册表可能还没扫完，先记下 id，等 packs 到货再认回来。
        deferredThemeId.value = id
      }
    } catch { /* ignore */ }
    loaded.value = true
    void pluginStore.ensureLoaded()
    apply()
  }

  async function setTheme(id: string): Promise<void> {
    currentId.value = id
    // 用户手动换过主题后，先前那个还没认出来的 pack id 就作废了。
    deferredThemeId.value = ''
    apply()
    try { await SetSetting('theme_id', id) } catch { /* ignore */ }
  }

  // ---- 把调色板写入 CSS 变量 ----
  function apply(): void {
    const { palette, mode } = current.value
    // pack:// 引用换成盘上读出的 data URL；还没读到（或包已被删）就当作没有背景图，
    // 表面退回纯色，等缓存到货时由 watch 再重绘一次。
    const bgImage = resolveThemeBg(current.value.bgImage)
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

    set('--shadow-sm', palette.shadowSm)
    set('--shadow', palette.shadow)
    set('--shadow-lg', palette.shadowLg)
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
      primaryAction: palette.btnSolid,
      successColor: palette.success,
      warningColor: palette.warning,
      dangerColor: palette.danger,
      infoColor: palette.info,
      tagColor: palette.bgTag,
      carouselColor: palette.carouselControl,
      dark: false,
      sidebarAlpha: 0.65,
      contentAlpha: 0.88,
    }
  }

  /**
   * 根据主色派生整套配色。
   *
   * `tint` 传预设作者那层手工表面色：不带它的话，在带背景图的预设里按一次「派生」
   * 就会把暖灰底换成通用灰 `#f0f0f0 / #ffffff`，和预设本身的样子对不上。
   */
  function deriveFromPrimary(primary: string, dark: boolean, tint?: Partial<ColorPalette>): CustomTheme {
    const palette = { ...(dark ? buildDarkPalette(primary) : buildLightPalette(primary)), ...(tint || {}) }
    return {
      id: `custom_${Date.now()}`,
      name: dark ? tr('theme.darkDerived') : tr('theme.lightDerived'),
      primary,
      text: palette.textPrimary,
      background: toSolidHex(palette.bgApp),
      sidebar: toSolidHex(palette.bgSidebar),
      content: toSolidHex(palette.bgCard),
      primaryAction: palette.btnSolid,
      successColor: palette.success,
      warningColor: palette.warning,
      dangerColor: palette.danger,
      infoColor: palette.info,
      tagColor: palette.bgTag,
      carouselColor: palette.carouselControl,
      dark,
      sidebarAlpha: readSurfaceAlpha(tint?.bgSidebar) ?? 0.65,
      contentAlpha: readSurfaceAlpha(tint?.bgCard) ?? 0.88,
    }
  }

  /** Add only newly introduced component tokens; never replace user choices. */
  function initializeComponentTokens(theme: CustomTheme): boolean {
    const derived = deriveFromPrimary(theme.primary, theme.dark)
    let changed = false
    const defaults: Pick<CustomTheme, 'primaryAction' | 'successColor' | 'warningColor' | 'dangerColor' | 'infoColor' | 'tagColor' | 'carouselColor'> = {
      primaryAction: theme.primaryAction || theme.actionColor || derived.primaryAction,
      successColor: derived.successColor,
      warningColor: derived.warningColor,
      dangerColor: derived.dangerColor,
      infoColor: derived.infoColor,
      tagColor: derived.tagColor,
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

  // 扩展包背景图按需读成 data URL 存进内存缓存：theme_customs 那一行只留 pack:// 引用。
  // themePresets 由 readyPacks 派生，注册表加载完/重扫完/启停一个包都会让它变，
  // 所以预热跟着它走，比单独盯 loaded 更准（也更快一步）。
  watch(() => pluginStore.themePresets, () => { void warmPackBackgrounds() }, { immediate: true })

  // 注册表到货后才认得出的 pack 主题，在这里补认一次。
  watch(themes, (list) => {
    if (!deferredThemeId.value) return
    if (!list.some(t => t.id === deferredThemeId.value)) return
    currentId.value = deferredThemeId.value
    deferredThemeId.value = ''
  })

  watch([currentId, customThemes, packAssets], () => apply(), { deep: true })
  // 扩展包被禁用或删除时 current 整体回落，但上面三个源都没动。
  watch(() => current.value.id, () => apply())

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
    resolveThemeBg,
    paletteFromCustom,
  }
})

export { luminance, hslToHex }
