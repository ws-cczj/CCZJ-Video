import { defineStore } from 'pinia'
import { ref, computed, watch } from 'vue'
import { GetSetting, SetSetting } from '../../bindings/cczjVideo/app'
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
  accentAlpha20: string
  accentAlpha35: string
  danger: string
  success: string
  warning: string
  shadow: string
  overlay: string
  btnHide: string
  btnMin: string
  btnClose: string
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
  backgroundImage?: string
  dark: boolean
  sidebarAlpha: number   // 侧边栏/面板背景透明度 0~1（有背景图时生效）
  contentAlpha: number   // 内容卡片背景透明度 0~1（有背景图时生效）
}

export interface PresetTheme {
  id: string
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
    accentContrast: '#ffffff',
    accentAlpha10: hexToRgba(primary, 0.1),
    accentAlpha20: hexToRgba(primary, 0.2),
    accentAlpha35: hexToRgba(primary, 0.35),
    danger: '#e53935',
    success: '#16a34a',
    warning: '#f59e0b',
    shadow: '0 6px 22px rgba(31, 36, 48, 0.08)',
    overlay: 'rgba(31, 36, 48, 0.45)',
    btnHide: '#3bc2b2',
    btnMin: '#85c43b',
    btnClose: '#fab4a0',
  }
  return { ...base, ...overrides }
}

// 深色模式：应用背景 = 主色的"很深很暗"的色调；卡片 = 深灰带一点主色
function buildDarkPalette(primary: string, overrides: Partial<ColorPalette> = {}): ColorPalette {
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
    accentContrast: '#ffffff',
    accentAlpha10: hexToRgba(primary, 0.15),
    accentAlpha20: hexToRgba(primary, 0.25),
    accentAlpha35: hexToRgba(primary, 0.45),
    danger: '#ef5350',
    success: '#4ade80',
    warning: '#fbbf24',
    shadow: '0 8px 24px rgba(0, 0, 0, 0.45)',
    overlay: 'rgba(0, 0, 0, 0.7)',
    btnHide: '#3bc2b2',
    btnMin: '#85c43b',
    btnClose: '#fab4a0',
  }
  return { ...base, ...overrides }
}

// ============================================================================
// 预设主题（12 浅色 + 8 深色 = 20，部分主题带背景图）
// ============================================================================
export const PRESET_THEMES: PresetTheme[] = [
  // ==================== 浅色 ====================
  { id: 'green', name: '绿意盎然', primary: '#16a34a', palette: buildLightPalette('#16a34a'), mode: 'light' },
  { id: 'blue', name: '蓝田生玉', primary: '#2e7dd7', palette: buildLightPalette('#2e7dd7'), mode: 'light' },
  { id: 'sky', name: '晴空万里', primary: '#0284c7', palette: buildLightPalette('#0284c7'), mode: 'light' },
  { id: 'indigo', name: '靛青之韵', primary: '#4f46e5', palette: buildLightPalette('#4f46e5'), mode: 'light' },
  { id: 'orange', name: '橙黄橘绿', primary: '#f59e0b', palette: buildLightPalette('#f59e0b'), mode: 'light' },
  { id: 'red', name: '热情似火', primary: '#e63946', palette: buildLightPalette('#e63946'), mode: 'light' },
  { id: 'pink', name: '粉装玉琢', primary: '#ec4899', palette: buildLightPalette('#ec4899'), mode: 'light' },
  { id: 'purple', name: '重斤球紫', primary: '#8b5cf6', palette: buildLightPalette('#8b5cf6'), mode: 'light' },
  { id: 'teal', name: '青青子衿', primary: '#14b8a6', palette: buildLightPalette('#14b8a6'), mode: 'light' },
  { id: 'cyan', name: '沧海一粟', primary: '#06b6d4', palette: buildLightPalette('#06b6d4'), mode: 'light' },
  { id: 'lime', name: '柠檬初上', primary: '#84cc16', palette: buildLightPalette('#84cc16'), mode: 'light' },
  { id: 'gray', name: '灰常美丽', primary: '#64748b', palette: buildLightPalette('#64748b'), mode: 'light' },

  // 月里嫦娥（中秋背景图）
  {
    id: 'mid_autumn', name: '月里嫦娥', primary: '#4a3b52', mode: 'dark',
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
    id: 'naruto', name: '木叶之村', primary: '#5790a7', mode: 'light',
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
    id: 'happy_new_year', name: '新年快乐', primary: '#c0392b', mode: 'light',
    palette: buildLightPalette('#c0392b', {
      bgApp: '#f3e3d3',
      bgCard: 'rgba(227, 166, 160, 0.93)',
      bgSidebar: 'rgba(217, 136, 128, 0.90)',
      border: '#e0c4a8',
      accentContrast: '#fff8dc',
    }),
    bgImage: xnkl
  },

  // ==================== 深色 ====================
  { id: 'd-green', name: '青出于黑', primary: '#22c55e', palette: buildDarkPalette('#22c55e'), mode: 'dark' },
  { id: 'd-blue', name: '清热板蓝', primary: '#60a5fa', palette: buildDarkPalette('#60a5fa'), mode: 'dark' },
  { id: 'd-violet', name: '紫罗兰夜', primary: '#a78bfa', palette: buildDarkPalette('#a78bfa'), mode: 'dark' },
  { id: 'd-rose', name: '暗夜玫瑰', primary: '#f472b6', palette: buildDarkPalette('#f472b6'), mode: 'dark' },
  { id: 'd-cyan', name: '深空碧波', primary: '#22d3ee', palette: buildDarkPalette('#22d3ee'), mode: 'dark' },
  { id: 'd-indigo', name: '靛蓝星夜', primary: '#818cf8', palette: buildDarkPalette('#818cf8'), mode: 'dark' },

  // 黑灯瞎火（月光背景图）
  {
    id: 'black', name: '黑灯瞎火', primary: '#969696', mode: 'dark',
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
    id: 'china_ink', name: '近墨者黑', primary: '#2f2f2f', mode: 'light',
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
]

// ============================================================================
// Store
// ============================================================================
export const useThemeStore = defineStore('theme', () => {
  const currentId = ref<string>('green')
  const customThemes = ref<CustomTheme[]>([])
  const loaded = ref(false)

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
    return {
      ...base,
      bgApp: c.background || base.bgApp,
      bgCard: effectiveContent,
      bgSidebar: effectiveSidebar,
      accent: c.primary,
      accentDim: c.dark ? lighten(c.primary, 0.15) : darken(c.primary, 0.15),
      textPrimary: c.text || base.textPrimary,
      textSecondary: c.dark ? '#c2c7d0' : '#4a5166',
      textMuted: c.dark ? '#8a92a6' : '#8a92a6',
      btnHide: c.btnHide || base.btnHide,
      btnMin: c.btnMin || base.btnMin,
      btnClose: c.btnClose || base.btnClose,
      accentAlpha10: hexToRgba(c.primary, c.dark ? 0.15 : 0.1),
      accentAlpha20: hexToRgba(c.primary, c.dark ? 0.25 : 0.2),
      accentAlpha35: hexToRgba(c.primary, c.dark ? 0.45 : 0.35),
    }
  }

  // ---- 当前主题（computed）----
  const current = computed(() => {
    const preset = PRESET_THEMES.find(t => t.id === currentId.value)
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
      id: PRESET_THEMES[0].id,
      name: PRESET_THEMES[0].name,
      mode: 'light' as const,
      accent: PRESET_THEMES[0].primary,
      palette: PRESET_THEMES[0].palette,
      bgImage: PRESET_THEMES[0].bgImage,
      isCustom: false,
    }
  })

  // ---- 生命周期 ----
  async function load(): Promise<void> {
    try {
      const id = await GetSetting('theme_id')
      if (typeof id === 'string' && id) {
        if (PRESET_THEMES.some(t => t.id === id) || customThemes.value.some(c => c.id === id)) {
          currentId.value = id
        }
      }
      const customs = await GetSetting('theme_customs')
      if (typeof customs === 'string' && customs) {
        try {
          const parsed: CustomTheme[] = JSON.parse(customs)
          // 修复每个自定义主题中可能过期的预设资源 URL
          customThemes.value = Array.isArray(parsed) ? parsed.map(repairCustomThemeAssets) : []
        } catch { /* ignore */ }
      }
    } catch { /* ignore */ }
    loaded.value = true
    apply()
  }

  async function setTheme(id: string): Promise<void> {
    currentId.value = id
    apply()
    try { await SetSetting('theme_id', id) } catch { /* ignore */ }
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
    set('--accent-alpha-10', palette.accentAlpha10)
    set('--accent-alpha-20', palette.accentAlpha20)
    set('--accent-alpha-35', palette.accentAlpha35)

    set('--danger', palette.danger)
    set('--success', palette.success)
    set('--warning', palette.warning)

    set('--shadow', palette.shadow)
    set('--overlay', palette.overlay)

    set('--btn-hide', palette.btnHide)
    set('--btn-min', palette.btnMin)
    set('--btn-close', palette.btnClose)

    if (bgImage) {
      set('--bg-image', `url("${bgImage}")`)
    } else {
      root.style.removeProperty('--bg-image')
    }

    root.style.backgroundColor = palette.bgApp
    root.style.color = palette.textPrimary
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
    return {
      id: `custom_${Date.now()}`,
      name: '我的主题',
      primary: '#16a34a',
      text: '#1f2430',
      background: lighten('#16a34a', 0.88),
      sidebar: lighten('#16a34a', 0.93),
      content: '#ffffff',
      btnHide: '#3bc2b2',
      btnMin: '#85c43b',
      btnClose: '#fab4a0',
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
      name: dark ? '深色派生' : '浅色派生',
      primary,
      text: palette.textPrimary,
      background: palette.bgApp,
      sidebar: palette.bgSidebar,
      content: palette.bgCard,
      btnHide: palette.btnHide,
      btnMin: palette.btnMin,
      btnClose: palette.btnClose,
      dark,
      sidebarAlpha: 0.65,
      contentAlpha: 0.88,
    }
  }

  watch([currentId, customThemes], () => apply(), { deep: true })

  return {
    themes: PRESET_THEMES,
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
