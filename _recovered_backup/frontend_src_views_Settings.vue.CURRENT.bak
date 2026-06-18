<script setup lang="ts">
defineOptions({ name: 'Settings' })
import { ref, reactive, onMounted, onUnmounted, computed, watch } from 'vue'
import { useRoute } from 'vue-router'
import {
  GetSetting, SetSetting, GetCloseBehavior, SetCloseBehavior, RestartApp,
  WindowSetResizable, WindowGetResizable, WindowSetSize, WindowGetSize,
} from '../../bindings/cczjVideo/app'
import { useThemeStore, type CustomTheme, type ColorPalette } from '../stores/theme'
import { useErrorStore } from '../stores/error'
import { useConfirmStore } from '../stores/confirm'
import { useDownloadStore } from '../stores/download'
import { useDevMode } from '../stores/devMode'
import Icon from '../components/Icon.vue'
import SelectDropdown from '../components/SelectDropdown.vue'
import { Button, Modal, Segment } from '../components/ui'

// ---------- 分组配置（顶部 tab） ----------
interface GroupItem {
  id: string
  label: string
  icon: string
}
const GROUPS: GroupItem[] = [
  { id: 'basic',  label: '基本设置', icon: 'sliders' },
  { id: 'theme',  label: '主题外观', icon: 'palette' },
  { id: 'play',   label: '播放设置', icon: 'play' },
  { id: 'search', label: '搜索设置', icon: 'search' },
  { id: 'about',  label: '关于应用', icon: 'info' },
]

const themeStore = useThemeStore()
const errorStore = useErrorStore()
const confirmStore = useConfirmStore()
const downloadStore = useDownloadStore()
const devMode = useDevMode()
const route = useRoute()

const activeGroup = ref<string>('basic')

const speedOptions = [0.5, 0.75, 1, 1.25, 1.5, 2].map(s => ({ value: s, label: `${s}×` }))

// 通用设置
const gridColumns = ref<number>(5)
const layoutDensity = ref<'comfortable' | 'compact'>('comfortable')
const playbackAutoPlay = ref(true)
const playbackAutoNext = ref(true)
const playbackSpeed = ref<number>(1)
const searchHistoryKeep = ref<number>(100)

// 窗口设置
const windowResizable = ref(true)
const windowWidth = ref<number>(1280)
const windowHeight = ref<number>(800)
const applyingWindowSize = ref(false)

// 字体设置
const fontSize = ref<number>(14)
const fontFamily = ref<string>('system')
const language = ref<string>('zh-CN')

const fontFamilyOptions = [
  { value: 'system', label: '系统默认' },
  { value: '"Microsoft YaHei", "微软雅黑", sans-serif', label: '微软雅黑' },
  { value: '"Source Han Sans SC", "思源黑体", sans-serif', label: '思源黑体' },
  { value: '"SimSun", "宋体", serif', label: '宋体' },
  { value: '"KaiTi", "楷体", serif', label: '楷体' },
]

const languageOptions = [
  { value: 'zh-CN', label: '简体中文' },
  { value: 'zh-TW', label: '繁體中文' },
  { value: 'en', label: 'English' },
]

async function loadWindowResizable(): Promise<void> {
  try { windowResizable.value = await WindowGetResizable() } catch { /* 忽略 */ }
}

async function saveWindowResizable(): Promise<void> {
  try { await WindowSetResizable(windowResizable.value) } catch { /* 忽略 */ }
}

async function loadWindowSize(): Promise<void> {
  try {
    const size = await WindowGetSize()
    if (!size) return
    windowWidth.value = size.width
    windowHeight.value = size.height
  } catch { /* 忽略 */ }
}

async function applyWindowSize(): Promise<void> {
  if (applyingWindowSize.value) return
  applyingWindowSize.value = true
  try {
    await WindowSetSize(windowWidth.value, windowHeight.value)
  } catch { /* 忽略 */ }
  finally { applyingWindowSize.value = false }
}

function applyFontFamily(): void {
  document.documentElement.style.fontFamily = fontFamily.value
}

function applyFontSize(): void {
  document.documentElement.style.fontSize = fontSize.value + 'px'
}

// 关闭行为 & 重启
const closeToTray = ref(false)
const restarting = ref(false)

async function loadCloseBehavior(): Promise<void> {
  try {
    closeToTray.value = await GetCloseBehavior()
  } catch { /* 忽略 */ }
}

async function saveCloseBehavior(): Promise<void> {
  try {
    await SetCloseBehavior(closeToTray.value)
  } catch { /* 忽略 */ }
}

async function restartApp(): Promise<void> {
  const hasActiveDl = downloadStore.hasActive
  const message = hasActiveDl
    ? '确定要重启应用吗？正在进行的下载可能会中断。'
    : '确定要重启应用吗？'
  const yes = await confirmStore.confirm({
    title: '重启应用',
    message,
    okText: '重启',
    level: 'warn',
  })
  if (!yes) return
  restarting.value = true
  try {
    RestartApp()
  } catch (e: any) {
    errorStore.fromError('重启失败', e, 'Settings.restartApp')
  } finally {
    restarting.value = false
  }
}

async function onDeleteTheme(id: string, name: string): Promise<void> {
  const yes = await confirmStore.confirm({
    title: '删除主题',
    message: `确定要删除主题"${name}"吗？此操作无法撤销。`,
    okText: '删除',
    level: 'danger',
  })
  if (!yes) return
  themeStore.deleteCustom(id)
}

// ---------- 主题编辑器 ----------
const editorOpen = ref(false)
const editing = reactive<CustomTheme & { __mode: 'create' | 'edit' }>({
  ...themeStore.makeEmptyCustom(),
  __mode: 'create',
})

const backgroundImageUrl = ref<string | null>(null)
const fileInputRef = ref<HTMLInputElement | null>(null)
let previousThemeId = ''

function colorToHex(c: string): string {
  const m = c.match(/rgba?\((\d+),\s*(\d+),\s*(\d+)/)
  if (m) {
    const r = parseInt(m[1]).toString(16).padStart(2, '0')
    const g = parseInt(m[2]).toString(16).padStart(2, '0')
    const b = parseInt(m[3]).toString(16).padStart(2, '0')
    return `#${r}${g}${b}`
  }
  return c
}

function openCreate(): void {
  previousThemeId = themeStore.currentId
  const cur = themeStore.current
  Object.assign(editing, {
    id: `custom_${Date.now()}`,
    name: cur.name + '（副本）',
    primary: cur.accent,
    text: colorToHex(cur.palette.textPrimary),
    background: colorToHex(cur.palette.bgApp),
    sidebar: colorToHex(cur.palette.bgSidebar),
    content: colorToHex(cur.palette.bgCard),
    btnHide: colorToHex(cur.palette.btnHide),
    btnMin: colorToHex(cur.palette.btnMin),
    btnClose: colorToHex(cur.palette.btnClose),
    backgroundImage: cur.bgImage,
    dark: cur.mode === 'dark',
    sidebarAlpha: 0.65,
    contentAlpha: 0.88,
    __mode: 'create' as const,
  })
  backgroundImageUrl.value = cur.bgImage || null
  editorOpen.value = true
  applyPreview()
}

function openEditPreset(preset: { id: string; name: string; primary: string; palette: ColorPalette; mode: 'dark' | 'light'; bgImage?: string }): void {
  previousThemeId = themeStore.currentId
  const existingOverride = themeStore.customThemes.find((c) => c.id === preset.id)
  Object.keys(editing).forEach((k) => { delete (editing as any)[k] })
  if (existingOverride) {
    // 如果已有 override，但它的 backgroundImage 是旧构建的预设资源路径，
    // 则强制替换为当前构建的正确 URL，避免显示破图。
    const fixed: CustomTheme & { __mode: 'edit' } = {
      ...JSON.parse(JSON.stringify(existingOverride)),
      __mode: 'edit',
    }
    if (preset.bgImage) {
      const candidate = themeStore.resolvePresetAsset(fixed.backgroundImage)
      if (candidate && candidate !== fixed.backgroundImage) {
        fixed.backgroundImage = preset.bgImage
      }
    }
    Object.assign(editing, fixed)
  } else {
    Object.assign(editing, {
      id: preset.id,
      name: preset.name,
      primary: preset.primary,
      text: colorToHex(preset.palette.textPrimary),
      background: colorToHex(preset.palette.bgApp),
      sidebar: colorToHex(preset.palette.bgSidebar),
      content: colorToHex(preset.palette.bgCard),
      btnHide: colorToHex(preset.palette.btnHide),
      btnMin: colorToHex(preset.palette.btnMin),
      btnClose: colorToHex(preset.palette.btnClose),
      backgroundImage: preset.bgImage || null,
      dark: preset.mode === 'dark',
      sidebarAlpha: 0.65,
      contentAlpha: 0.88,
      __mode: 'edit' as const,
    })
  }
  backgroundImageUrl.value = editing.backgroundImage || null
  editorOpen.value = true
  applyPreview()
}

function openEdit(existing: CustomTheme): void {
  previousThemeId = themeStore.currentId
  const copy: any = JSON.parse(JSON.stringify(existing))
  // 先清理 editing 所有字段，再合并现有主题字段 + __mode: 'edit'，确保编辑模式正确
  Object.keys(editing).forEach((k) => { delete (editing as any)[k] })
  copy.__mode = 'edit'
  // 对独立自定义主题（非预设覆盖）也修一次资源 URL
  if (copy.backgroundImage) {
    const resolved = themeStore.resolvePresetAsset(copy.backgroundImage)
    if (resolved && resolved !== copy.backgroundImage) copy.backgroundImage = resolved
  }
  Object.assign(editing, copy)
  backgroundImageUrl.value = editing.backgroundImage || null
  editorOpen.value = true
  applyPreview()
}

function resolvePreset(t: { id: string; name: string; primary: string; palette: ColorPalette; mode: 'dark' | 'light'; bgImage?: string }) {
  const override = themeStore.customThemes.find((c) => c.id === t.id)
  if (!override) return { data: t, isOverride: false, bg: t.bgImage ? t.bgImage : undefined, textPrimary: t.palette.textPrimary, sidebarBg: t.palette.bgSidebar }
  // 判断 override 的背景图是否仍是"预设资源"：若是，用当前构建的 URL；
  // 如果是用户上传的 data URL / http URL，则用用户自己的值。
  let bg: string | undefined = override.backgroundImage
  if (bg && !bg.startsWith('data:') && !/^https?:\/\//i.test(bg)) {
    const resolved = themeStore.resolvePresetAsset(bg)
    if (resolved && resolved !== bg) bg = t.bgImage || resolved
  }
  // 有背景图时，改用半透明的 tint 让图片展示出来（纯颜色主题仍然使用纯色）
  const effectiveSidebarBg = bg
    ? hexToRgbaCss(override.sidebar || t.palette.bgSidebar, 0.55)
    : (override.sidebar || t.palette.bgSidebar)
  return {
    data: override,
    isOverride: true,
    bg,
    textPrimary: override.text || t.palette.textPrimary,
    sidebarBg: effectiveSidebarBg,
  }
}

function hexToRgbaCss(color: string, alpha: number): string {
  if (!color) return `rgba(0,0,0,${alpha})`
  // 已是 rgba / rgb
  if (color.startsWith('rgb')) return color
  const hex = color.replace('#', '').trim()
  const full = hex.length === 3
    ? hex.split('').map((c) => c + c).join('')
    : hex
  const r = parseInt(full.substring(0, 2), 16)
  const g = parseInt(full.substring(2, 4), 16)
  const b = parseInt(full.substring(4, 6), 16)
  if (Number.isNaN(r) || Number.isNaN(g) || Number.isNaN(b)) return color
  return `rgba(${r}, ${g}, ${b}, ${alpha})`
}

function closeEditor(): void {
  // 取消编辑：清理 __preview__ 临时主题，并还原之前的主题
  themeStore.customThemes = themeStore.customThemes.filter(c => c.id !== '__preview__')
  if (previousThemeId && themeStore.currentId === '__preview__') {
    themeStore.setTheme(previousThemeId)
  }
  editorOpen.value = false
}

// 预览：把 editing 作为临时主题推入并切换
function applyPreview(): void {
  const preview: CustomTheme = {
    id: '__preview__',
    name: editing.name || '预览主题',
    primary: editing.primary,
    text: editing.text,
    background: editing.background,
    sidebar: editing.sidebar,
    content: editing.content,
    btnHide: editing.btnHide,
    btnMin: editing.btnMin,
    btnClose: editing.btnClose,
    backgroundImage: editing.backgroundImage,
    dark: editing.dark,
    sidebarAlpha: editing.sidebarAlpha ?? 0.65,
    contentAlpha: editing.contentAlpha ?? 0.88,
  }
  const others = themeStore.customThemes.filter(c => c.id !== '__preview__')
  themeStore.customThemes = [...others, preview]
  themeStore.currentId = '__preview__'
  themeStore.apply()
}

async function saveEditing(): Promise<void> {
  const name = (editing.name || '我的主题').trim()
  const isEdit = editing.__mode === 'edit'
  const targetId = isEdit ? editing.id : `custom_${Date.now()}`

  // 先清理预览临时主题
  themeStore.customThemes = themeStore.customThemes.filter((c) => c.id !== '__preview__')

  const themeToSave: CustomTheme = {
    id: targetId,
    name,
    primary: editing.primary,
    text: editing.text,
    background: editing.background,
    sidebar: editing.sidebar,
    content: editing.content,
    btnHide: editing.btnHide,
    btnMin: editing.btnMin,
    btnClose: editing.btnClose,
    backgroundImage: editing.backgroundImage,
    dark: editing.dark,
    sidebarAlpha: editing.sidebarAlpha ?? 0.65,
    contentAlpha: editing.contentAlpha ?? 0.88,
  }
  await themeStore.saveCustom(themeToSave)
  editorOpen.value = false
}

async function deleteEditing(): Promise<void> {
  if (editing.__mode !== 'edit') return
  const id = editing.id
  editorOpen.value = false
  // 先清理 __preview__ 临时主题
  themeStore.customThemes = themeStore.customThemes.filter((c) => c.id !== '__preview__')
  // 用 deleteCustom 来删除并持久化
  await themeStore.deleteCustom(id)
  // 如果当前主题就是被删除的，deleteCustom 会切到默认主题
}

// 派生：按主色 + 深浅模式一键生成全套配色
function deriveFromPrimary(): void {
  const derived = themeStore.deriveFromPrimary(editing.primary, editing.dark)
  editing.text = derived.text
  editing.background = derived.background
  editing.sidebar = derived.sidebar
  editing.content = derived.content
  editing.btnHide = derived.btnHide
  editing.btnMin = derived.btnMin
  editing.btnClose = derived.btnClose
  applyPreview()
}

// 图片选择
function onImageSelected(evt: Event): void {
  const input = evt.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  const reader = new FileReader()
  reader.onload = () => {
    const url = reader.result as string
    backgroundImageUrl.value = url
    editing.backgroundImage = url
    applyPreview()
  }
  reader.readAsDataURL(file)
}

function clearBackgroundImage(): void {
  backgroundImageUrl.value = null
  editing.backgroundImage = undefined
  applyPreview()
}

function onDropZoneClick(): void {
  if (backgroundImageUrl.value) return
  fileInputRef.value?.click()
}
function onDragOver(): void { /* hover state handled by CSS via :hover */ }
function onDragLeave(): void { /* hover state handled by CSS */ }
function onDrop(e: DragEvent): void {
  const file = e.dataTransfer?.files?.[0]
  if (!file || !file.type.startsWith('image/')) return
  const reader = new FileReader()
  reader.onload = () => {
    const url = reader.result as string
    backgroundImageUrl.value = url
    editing.backgroundImage = url
    applyPreview()
  }
  reader.readAsDataURL(file)
}

// ---------- 列表：把预设 + 自定义分别按 light/dark 分组 ----------
const lightPresets = computed(() => themeStore.themes.filter(t => t.mode === 'light'))
const darkPresets  = computed(() => themeStore.themes.filter(t => t.mode === 'dark'))

// 自定义主题中，id 与预设重合的视为“对该预设的覆盖/修改”，不再作为独立卡片显示
const presetIdsSet = computed(() => new Set(themeStore.themes.map((t) => t.id)))
const pureCustomThemes = computed(() =>
  themeStore.customThemes.filter((c) => !presetIdsSet.value.has(c.id))
)

function isActive(id: string): boolean {
  return themeStore.currentId === id
}

function pickTheme(id: string): void {
  themeStore.setTheme(id)
}

// ---------- 启动 ----------
onMounted(async () => {
  const hash = (route.hash || '').replace('#', '').trim()
  const validIds = GROUPS.map(g => g.id)
  if (hash && validIds.includes(hash)) {
    activeGroup.value = hash
  }

  try { if (!themeStore.loaded) await themeStore.load() } catch { /* 忽略 */ }
  try { await downloadStore.init() } catch { /* 忽略 */ }

  const col = await safeGet('grid_columns', '5')
  gridColumns.value = parseInt(col, 10) || 5
  const den = await safeGet('layout_density', 'comfortable')
  layoutDensity.value = den === 'compact' ? 'compact' : 'comfortable'
  playbackAutoPlay.value = (await safeGet('playback_auto_play', '1')) !== '0'
  playbackAutoNext.value = (await safeGet('playback_auto_next', '1')) !== '0'
  playbackSpeed.value = parseFloat(await safeGet('playback_speed', '1')) || 1
  searchHistoryKeep.value = parseInt(await safeGet('search_history_keep', '100'), 10) || 100

  // 加载窗口设置
  await loadWindowResizable()
  await loadWindowSize()

  // 加载字体设置
  const fs = await safeGet('font_size', '14')
  fontSize.value = parseInt(fs, 10) || 14
  const ff = await safeGet('font_family', 'system')
  fontFamily.value = ff || 'system'
  const lang = await safeGet('language', 'zh-CN')
  language.value = lang || 'zh-CN'
  applyFontSize()
  applyFontFamily()

  // 加载关闭行为设置
  await loadCloseBehavior()
})

onUnmounted(() => {})

async function safeGet(key: string, fallback: string): Promise<string> {
  try { const v = await GetSetting(key); return v || fallback } catch { return fallback }
}
async function save(key: string, val: string | number | boolean): Promise<void> {
  try { await SetSetting(key, String(val)) } catch { /* 忽略 */ }
}
</script>

<template>
  <div class="settings-page">
    <!-- 顶部分组切换 tab -->
    <div class="tabs">
      <button
        v-for="g in GROUPS"
        :key="g.id"
        class="tab"
        :class="{ active: activeGroup === g.id }"
        @click="activeGroup = g.id"
      >
        <Icon :name="g.icon" :size="14" />
        <span>{{ g.label }}</span>
      </button>
    </div>

    <div class="content">
      <!-- ========== 基本设置 ========== -->
      <div v-if="activeGroup === 'basic'" class="panel">
        <!-- 窗口尺寸 -->
        <section class="block">
          <h3>窗口尺寸</h3>
          <div class="row window-size-row">
            <div class="input-group">
              <label>宽度</label>
              <input type="number" v-model.number="windowWidth" min="800" max="3840" step="10" class="num-input" />
              <span class="unit">px</span>
            </div>
            <span class="size-sep">×</span>
            <div class="input-group">
              <label>高度</label>
              <input type="number" v-model.number="windowHeight" min="500" max="2160" step="10" class="num-input" />
              <span class="unit">px</span>
            </div>
            <button class="apply-btn" :disabled="applyingWindowSize" @click="applyWindowSize">
              {{ applyingWindowSize ? '应用中…' : '应用' }}
            </button>
          </div>
          <div class="row" style="margin-top: 10px;">
            <label class="toggle">
              <input type="checkbox" v-model="windowResizable" @change="saveWindowResizable" />
              <span>允许拖动调整窗口大小</span>
            </label>
          </div>
        </section>

        <!-- 字体大小 -->
        <section class="block">
          <h3>字体大小</h3>
          <div class="row">
            <input type="range" v-model.number="fontSize" min="12" max="22" step="1"
              @change="applyFontSize(); save('font_size', fontSize)" />
            <span class="value">{{ fontSize }}px</span>
          </div>
          <div class="font-preview" :style="{ fontSize: fontSize + 'px' }">示例文字 AaBbCc 123</div>
        </section>

        <!-- 字体类型 -->
        <section class="block">
          <h3>字体类型</h3>
          <div class="row">
            <SelectDropdown
              :model-value="fontFamily"
              :options="fontFamilyOptions"
              @update:model-value="(v: any) => { fontFamily = v; applyFontFamily(); save('font_family', v) }"
            />
          </div>
        </section>

        <!-- 语言 -->
        <section class="block">
          <h3>语言</h3>
          <div class="row">
            <SelectDropdown
              :model-value="language"
              :options="languageOptions"
              @update:model-value="(v: any) => { language = v; save('language', v) }"
            />
          </div>
        </section>

        <!-- 首页网格列数 -->
        <section class="block">
          <h3>首页网格列数</h3>
          <div class="row">
            <input type="range" v-model.number="gridColumns" min="3" max="8" @change="save('grid_columns', gridColumns)" />
            <span class="value">{{ gridColumns }} 列</span>
          </div>
        </section>

        <!-- 卡片密度 -->
        <section class="block">
          <h3>卡片密度</h3>
          <div class="row">
            <Segment
              :model-value="layoutDensity"
              :options="[{ value: 'comfortable', label: '舒适' }, { value: 'compact', label: '紧凑' }]"
              @update:model-value="(v: any) => { layoutDensity = v as 'comfortable'|'compact'; save('layout_density', String(v)) }"
            />
          </div>
        </section>

        <!-- 关闭行为 -->
        <section class="block">
          <h3>关闭行为</h3>
          <div class="row">
            <label class="toggle">
              <input type="checkbox" v-model="closeToTray" @change="saveCloseBehavior" />
              <span>点击关闭按钮时最小化到托盘</span>
            </label>
          </div>
        </section>

        <!-- 开发者模式开关（密码解锁后可见） -->
        <section v-if="devMode.unlocked" class="block block-dev">
          <h3>开发者模式</h3>
          <div class="row">
            <label class="toggle">
              <input type="checkbox" :checked="devMode.enabled" @change="(e: any) => devMode.setEnabled(e.target.checked)" />
              <span>打开开发者模式</span>
            </label>
          </div>
          <small class="desc">开启后在侧边栏显示"开发者模式"栏目，提供后台管理功能</small>
        </section>

      </div>

      <!-- ========== 主题外观 ========== -->
      <div v-else-if="activeGroup === 'theme'" class="panel">
        <section class="block">
          <h3>主题颜色</h3>

          <h4 class="sub-title">浅色主题</h4>
          <div class="theme-grid">
            <!-- 预设 -->
            <button
              v-for="t in lightPresets"
              :key="t.id"
              class="theme-card"
              :class="{ active: isActive(t.id), hasBg: !!resolvePreset(t).bg, 'is-override': resolvePreset(t).isOverride }"
              :style="[
                resolvePreset(t).bg
                  ? {
                      backgroundColor: resolvePreset(t).isOverride ? resolvePreset(t).sidebarBg : 'transparent',
                      backgroundImage: `url(${resolvePreset(t).bg})`,
                      backgroundSize: 'cover',
                      backgroundPosition: 'center',
                      color: resolvePreset(t).textPrimary
                    }
                  : { background: resolvePreset(t).isOverride ? resolvePreset(t).sidebarBg : t.palette.bgSidebar, color: resolvePreset(t).textPrimary }
              ]"
              @click="pickTheme(t.id)"
            >
              <span class="card-actions-top" @click.stop>
                <button class="mini-btn" @click="openEditPreset(t)" title="编辑主题">
                  <Icon name="pencil" :size="12" />
                </button>
              </span>
              <span v-if="!resolvePreset(t).bg" class="swatch" :style="{ background: resolvePreset(t).data.primary }"></span>
              <span v-if="resolvePreset(t).bg" class="swatch small" :style="{ background: resolvePreset(t).data.primary }"></span>
              <span class="label" :style="{ color: resolvePreset(t).textPrimary }">{{ resolvePreset(t).data.name }}</span>
              <span v-if="isActive(t.id)" class="check"><Icon name="check" :size="12" /></span>
            </button>

            <!-- 自定义（排除与预设同名的覆盖项，那些已通过上方预设卡显示） -->
            <button
              v-for="c in pureCustomThemes.filter((c) => !c.dark)"
              :key="c.id"
              class="theme-card custom"
              :class="{ active: isActive(c.id), hasBg: !!c.backgroundImage }"
              :style="[
                c.backgroundImage
                  ? {
                      backgroundColor: hexToRgbaCss(c.sidebar || c.background, 0.55),
                      backgroundImage: `url(${c.backgroundImage})`,
                      backgroundSize: 'cover',
                      backgroundPosition: 'center',
                      color: c.text
                    }
                  : { background: c.sidebar, color: c.text }
              ]"
              @click="pickTheme(c.id)"
            >
              <span class="card-actions-top" @click.stop>
                <button class="mini-btn" @click="openEdit(c)" title="编辑主题">
                  <Icon name="pencil" :size="12" />
                </button>
              </span>
              <span v-if="!c.backgroundImage" class="swatch" :style="{ background: c.primary }"></span>
              <span v-if="c.backgroundImage" class="swatch small" :style="{ background: c.primary }"></span>
              <span class="label" :style="{ color: c.text }">{{ c.name }}</span>
              <span class="card-actions" @click.stop>
                <button class="mini-btn danger" @click="onDeleteTheme(c.id, c.name)" title="删除">
                  <Icon name="x" :size="12" />
                </button>
              </span>
            </button>

            <!-- 添加（浅色区） -->
            <button class="theme-card add" @click="openCreate(); applyPreview()">
              <span class="swatch plus"><Icon name="plus" :size="22" /></span>
              <span class="label">添加主题</span>
            </button>
          </div>

          <h4 class="sub-title">深色主题</h4>
          <div class="theme-grid">
            <button
              v-for="t in darkPresets"
              :key="t.id"
              class="theme-card"
              :class="{ active: isActive(t.id), hasBg: !!resolvePreset(t).bg, 'is-override': resolvePreset(t).isOverride }"
              :style="[
                resolvePreset(t).bg
                  ? {
                      backgroundColor: resolvePreset(t).isOverride ? resolvePreset(t).sidebarBg : 'transparent',
                      backgroundImage: `url(${resolvePreset(t).bg})`,
                      backgroundSize: 'cover',
                      backgroundPosition: 'center',
                      color: resolvePreset(t).textPrimary
                    }
                  : { background: resolvePreset(t).isOverride ? resolvePreset(t).sidebarBg : t.palette.bgSidebar, color: resolvePreset(t).textPrimary }
              ]"
              @click="pickTheme(t.id)"
            >
              <span class="card-actions-top" @click.stop>
                <button class="mini-btn" @click="openEditPreset(t)" title="编辑主题">
                  <Icon name="pencil" :size="12" />
                </button>
              </span>
              <span v-if="!resolvePreset(t).bg" class="swatch" :style="{ background: resolvePreset(t).data.primary }"></span>
              <span v-if="resolvePreset(t).bg" class="swatch small" :style="{ background: resolvePreset(t).data.primary }"></span>
              <span class="label" :style="{ color: resolvePreset(t).textPrimary }">{{ resolvePreset(t).data.name }}</span>
              <span v-if="isActive(t.id)" class="check"><Icon name="check" :size="12" /></span>
            </button>

            <button
              v-for="c in pureCustomThemes.filter((c) => c.dark)"
              :key="c.id"
              class="theme-card custom"
              :class="{ active: isActive(c.id), hasBg: !!c.backgroundImage }"
              :style="[
                c.backgroundImage
                  ? {
                      backgroundColor: hexToRgbaCss(c.sidebar || c.background, 0.55),
                      backgroundImage: `url(${c.backgroundImage})`,
                      backgroundSize: 'cover',
                      backgroundPosition: 'center',
                      color: c.text
                    }
                  : { background: c.sidebar, color: c.text }
              ]"
              @click="pickTheme(c.id)"
            >
              <span class="card-actions-top" @click.stop>
                <button class="mini-btn" @click="openEdit(c)" title="编辑主题">
                  <Icon name="pencil" :size="12" />
                </button>
              </span>
              <span v-if="!c.backgroundImage" class="swatch" :style="{ background: c.primary }"></span>
              <span v-if="c.backgroundImage" class="swatch small" :style="{ background: c.primary }"></span>
              <span class="label" :style="{ color: c.text }">{{ c.name }}</span>
              <span class="card-actions" @click.stop>
                <button class="mini-btn danger" @click="onDeleteTheme(c.id, c.name)" title="删除">
                  <Icon name="x" :size="12" />
                </button>
              </span>
            </button>
          </div>
        </section>
      </div>

      <!-- ========== 播放设置 ========== -->
      <div v-else-if="activeGroup === 'play'" class="panel">
        <section class="block">
          <h3>播放行为</h3>
          <div class="row toggles">
            <label class="toggle">
              <input type="checkbox" v-model="playbackAutoPlay" @change="save('playback_auto_play', playbackAutoPlay?'1':'0')" />
              <span>自动开始播放</span>
            </label>
            <label class="toggle">
              <input type="checkbox" v-model="playbackAutoNext" @change="save('playback_auto_next', playbackAutoNext?'1':'0')" />
              <span>播放完自动下一集</span>
            </label>
          </div>
        </section>

        <section class="block">
          <h3>默认播放速度</h3>
          <div class="row">
            <Segment
              :model-value="playbackSpeed"
              :options="speedOptions"
              @update:model-value="(v: any) => { playbackSpeed = Number(v); save('playback_speed', String(v)) }"
            />
          </div>
        </section>
      </div>

      <!-- ========== 搜索设置 ========== -->
      <div v-else-if="activeGroup === 'search'" class="panel">
        <section class="block">
          <h3>搜索历史</h3>
          <div class="row">
            <input type="range" v-model.number="searchHistoryKeep" min="0" max="500" step="10" @change="save('search_history_keep', searchHistoryKeep)" />
            <span class="value">保留 {{ searchHistoryKeep }} 条</span>
          </div>
        </section>
      </div>

      <!-- ========== 关于 ========== -->
      <div v-else-if="activeGroup === 'about'" class="panel">
        <section class="block">
          <div class="about-card">
            <div class="about-icon"><Icon name="film" :size="22" /></div>
            <div>
              <h3 class="app-name-clickable" @click="devMode.clickAppName" title="点击 3 次以激活开发者模式">CCZJ Video</h3>
              <p>版本 <strong>1.1.0</strong> · Wails + Vue 3</p>
              <small>当前生效主题：<em>{{ themeStore.current.name }}</em>（{{ themeStore.current.mode === 'dark' ? '深色' : '浅色' }}）</small>
            </div>
          </div>

          <div class="about-actions">
            <Button
              variant="primary"
              size="md"
              :disabled="restarting"
              :loading="restarting"
              @click="restartApp"
            >
              <Icon name="refresh" :size="14" /> 重启应用
            </Button>
          </div>
        </section>

        <section class="block">
          <h3>使用声明</h3>
          <div class="disclaimer-card">
            <div class="disclaimer-icon"><Icon name="shield" :size="20" /></div>
            <div class="disclaimer-content">
              <p><strong>本软件仅供学习与研究使用</strong>，不保留任何网络资源。</p>
              <p>所有通过本软件访问或下载的内容，观看后请自行删除，版权归原作者/原版权方所有。</p>
              <p>请勿将本软件用于商业用途或违反当地法律法规的场景。</p>
            </div>
          </div>
        </section>
      </div>
    </div>

    <!-- ========== 开发者模式密码弹窗 ========== -->
    <teleport to="body">
      <div v-if="devMode.showPasswordModal" class="dev-password-overlay" @click.self="devMode.closePasswordModal">
        <div class="dev-password-modal">
          <h2>开发者模式验证</h2>
          <p class="dev-password-desc">请输入6位数字密码以启用开发者模式</p>
          <input
            ref="devPwdInput"
            v-model="devMode.passwordInput"
            type="password"
            maxlength="6"
            class="dev-password-input"
            placeholder="••••••"
            autocomplete="off"
            @keyup.enter="devMode.verifyPassword"
          />
          <p v-if="devMode.passwordError" class="dev-password-error">{{ devMode.passwordError }}</p>
          <div class="dev-password-actions">
            <button class="dev-password-btn dev-password-btn--cancel" @click="devMode.closePasswordModal">取消</button>
            <button class="dev-password-btn dev-password-btn--confirm" @click="devMode.verifyPassword">验证</button>
          </div>
        </div>
      </div>
    </teleport>

    <!-- ========== 主题编辑器 弹窗 ========== -->
    <Modal
      :model-value="editorOpen"
      :title="editing.__mode === 'edit' ? '修改主题' : '新增主题'"
      width="min(980px, 94vw)"
      :show-footer="true"
      @update:model-value="(v: boolean) => !v && closeEditor()"
    >
      <div class="modal-body">
        <div class="edit-row">
          <div class="field">
            <label>主题名称</label>
            <input type="text" v-model="editing.name" placeholder="我的主题" @input="applyPreview" />
          </div>
          <div class="flags">
            <label class="toggle">
              <input type="checkbox" v-model="editing.dark" @change="applyPreview" />
              <span>暗色主题</span>
            </label>
            <Button variant="primary" size="sm" @click="deriveFromPrimary">
              <Icon name="sparkle" :size="13" /> 按主色派生整套配色
            </Button>
          </div>
        </div>

        <h4 class="group-title">主要颜色</h4>
        <div class="picker-grid">
          <div class="picker-item">
            <label>主题色</label>
            <div class="picker-cell"><input type="color" v-model="editing.primary" @input="applyPreview" /><span>{{ editing.primary }}</span></div>
          </div>
          <div class="picker-item">
            <label>字体颜色</label>
            <div class="picker-cell"><input type="color" v-model="editing.text" @input="applyPreview" /><span>{{ editing.text }}</span></div>
          </div>
          <div class="picker-item">
            <label>应用背景</label>
            <div class="picker-cell"><input type="color" v-model="editing.background" @input="applyPreview" /><span>{{ editing.background }}</span></div>
          </div>
          <div class="picker-item">
            <label>侧边栏背景</label>
            <div class="picker-cell"><input type="color" v-model="editing.sidebar" @input="applyPreview" /><span>{{ editing.sidebar }}</span></div>
          </div>
          <div class="picker-item">
            <label>内容区域背景</label>
            <div class="picker-cell"><input type="color" v-model="editing.content" @input="applyPreview" /><span>{{ editing.content }}</span></div>
          </div>
        </div>

        <h4 class="group-title">背景透明度</h4>
        <div class="picker-grid">
          <div class="picker-item">
            <label>侧边栏透明度</label>
            <div class="picker-cell range-cell">
              <input type="range" v-model.number="editing.sidebarAlpha" min="0" max="1" step="0.05" @input="applyPreview" />
              <span>{{ Math.round((editing.sidebarAlpha ?? 0.65) * 100) }}%</span>
            </div>
          </div>
          <div class="picker-item">
            <label>卡片透明度</label>
            <div class="picker-cell range-cell">
              <input type="range" v-model.number="editing.contentAlpha" min="0" max="1" step="0.05" @input="applyPreview" />
              <span>{{ Math.round((editing.contentAlpha ?? 0.88) * 100) }}%</span>
            </div>
          </div>
        </div>

        <h4 class="group-title">窗口控制按钮颜色</h4>
        <div class="picker-grid small">
          <div class="picker-item">
            <label>关闭按钮</label>
            <div class="picker-cell"><input type="color" v-model="editing.btnClose" @input="applyPreview" /><span>{{ editing.btnClose }}</span></div>
          </div>
          <div class="picker-item">
            <label>最小化按钮</label>
            <div class="picker-cell"><input type="color" v-model="editing.btnMin" @input="applyPreview" /><span>{{ editing.btnMin }}</span></div>
          </div>
          <div class="picker-item">
            <label>隐藏按钮</label>
            <div class="picker-cell"><input type="color" v-model="editing.btnHide" @input="applyPreview" /><span>{{ editing.btnHide }}</span></div>
          </div>
        </div>

        <div
          class="bg-drop-zone"
          :class="{ 'has-image': !!backgroundImageUrl }"
          @click="onDropZoneClick"
          @dragover.prevent="onDragOver"
          @dragleave="onDragLeave"
          @drop.prevent="onDrop"
        >
          <button
            v-if="backgroundImageUrl"
            class="bg-remove"
            title="移除图片"
            @click.stop="clearBackgroundImage"
          >
            <Icon name="x" :size="14" />
          </button>
          <img v-if="backgroundImageUrl" :src="backgroundImageUrl" alt="背景预览" />
          <div v-else class="bg-drop-hint">
            <Icon name="plus" :size="42" />
            <span>点击或拖拽图片到此处</span>
          </div>
          <input ref="fileInputRef" type="file" accept="image/*" @change="onImageSelected" hidden />
        </div>
      </div>

      <template #footer>
        <Button v-if="editing.__mode === 'edit'" variant="danger" size="md" @click="deleteEditing">
          <Icon name="trash" :size="14" /> 删除
        </Button>
        <span style="flex: 1"></span>
        <Button variant="secondary" size="md" @click="closeEditor">取消</Button>
        <Button variant="primary" size="md" @click="saveEditing">
          <Icon name="save" :size="14" /> 保存主题
        </Button>
      </template>
    </Modal>
  </div>
</template>

<style scoped>
.settings-page {
  color: var(--text-primary);
  padding-bottom: 48px;
  margin-top: -20px;
}
.page-head {
  padding: 12px 14px 6px;
}
.page-head h1 {
  font-size: 20px;
  font-weight: 700;
  margin: 0 0 4px;
}
.page-desc {
  font-size: 12px;
  color: var(--text-muted);
  margin: 0;
}

/* ============ 顶部 tab ============ */
.tabs {
  display: flex;
  gap: 6px;
  padding: 28px 24px 12px;
  margin: 0 -24px;
  background-image: linear-gradient(var(--bg-secondary), var(--bg-secondary));
  position: sticky;
  top: -20px;
  z-index: 5;
  box-shadow: 0 1px 0 rgba(127, 127, 127, 0.12);
    border-bottom-left-radius: 20%;
  border-bottom-right-radius: 20%;
}
.tab {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 8px 14px;
  border: 1px solid transparent;
  background: transparent;
  color: var(--text-secondary);
  border-radius: 8px;
  font-size: 13px;
  font-weight: 500;
  cursor: pointer;
  transition: all 0.15s ease;
}
.tab:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}
.tab.active {
  background: var(--accent);
  color: var(--accent-contrast);
  box-shadow: 0 4px 14px var(--accent-alpha-20);
}

/* ============ 内容面板 ============ */
.content {
  padding: 12px 14px;
}
.panel {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.block {
  padding: 18px 20px;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 12px;
  margin-bottom: 12px;
}
.block h3 {
  font-size: 14px;
  font-weight: 700;
  margin: 0 0 14px;
  letter-spacing: 0.3px;
}

/* 窗口尺寸设置 */
.window-size-row {
  display: flex;
  align-items: flex-end;
  gap: 8px;
  flex-wrap: wrap;
}
.input-group {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.input-group label {
  font-size: 11px;
  color: var(--text-muted);
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}
.num-input {
  width: 90px;
  padding: 6px 10px;
  background: var(--bg-secondary);
  border: 1px solid var(--border);
  border-radius: 6px;
  color: var(--text-primary);
  font-size: 13px;
  text-align: center;
}
.num-input:focus {
  outline: none;
  border-color: var(--accent);
  box-shadow: 0 0 0 2px rgba(var(--accent-rgb, 99, 102, 241), 0.15);
}
.input-group .unit {
  font-size: 11px;
  color: var(--text-muted);
  margin-top: 2px;
  text-align: center;
}
.size-sep {
  color: var(--text-muted);
  font-size: 16px;
  padding-bottom: 6px;
}
.apply-btn {
  padding: 6px 16px;
  background: var(--accent);
  color: #fff;
  border: none;
  border-radius: 6px;
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
  transition: opacity 0.15s;
  margin-bottom: 2px;
}
.apply-btn:hover { opacity: 0.85; }
.apply-btn:disabled { opacity: 0.5; cursor: not-allowed; }

/* 字体预览 */
.font-preview {
  margin-top: 10px;
  padding: 10px 14px;
  background: var(--bg-secondary);
  border: 1px solid var(--border);
  border-radius: 8px;
  color: var(--text-primary);
  line-height: 1.6;
}
.sub-title {
  font-size: 13px;
  color: var(--text-secondary);
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 1px;
  margin: 0 0 10px;
  padding-top: 6px;
  border-top: 1px dashed var(--border);
}
.sub-title:first-of-type {
  border-top: none;
  padding-top: 0;
}

.row {
  display: flex;
  align-items: center;
  gap: 14px;
}
.row.toggles {
  gap: 22px;
  flex-wrap: wrap;
}
.row input[type='range'] {
  flex: 1;
  accent-color: var(--accent);
  max-width: 360px;
}
.value {
  font-weight: 600;
  color: var(--text-secondary);
  font-variant-numeric: tabular-nums;
}

.toggle {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  cursor: pointer;
  font-size: 13px;
  color: var(--text-primary);
}
.toggle input[type='checkbox'] {
  -webkit-appearance: none;
  appearance: none;
  width: 18px;
  height: 18px;
  border: 1.5px solid var(--border-strong);
  border-radius: 5px;
  background: var(--bg-card);
  cursor: pointer;
  position: relative;
  transition: all 0.15s ease;
  flex-shrink: 0;
}
.toggle input[type='checkbox']:hover {
  border-color: var(--accent);
}
.toggle input[type='checkbox']:checked {
  background: var(--accent);
  border-color: var(--accent);
}
.toggle input[type='checkbox']:checked::after {
  content: '';
  position: absolute;
  top: 3px;
  left: 5px;
  width: 4px;
  height: 8px;
  border: 2px solid var(--accent-contrast);
  border-top: 0;
  border-left: 0;
  transform: rotate(45deg);
}

.seg {
  display: inline-flex;
  background: var(--bg-secondary);
  border: 1px solid var(--border);
  border-radius: 10px;
  padding: 4px;
  gap: 2px;
}
.seg button {
  padding: 6px 14px;
  border-radius: 8px;
  border: none;
  background: transparent;
  color: var(--text-secondary);
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  transition: all 0.15s ease;
}
.seg button:hover { color: var(--text-primary); }
.seg button.active {
  background: var(--accent);
  color: var(--accent-contrast);
}

/* ============ 主题网格 ============ */
.theme-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(128px, 1fr));
  gap: 14px;
  margin-bottom: 18px;
}
.theme-card {
  position: relative;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
  padding: 14px 10px 14px;
  min-height: 160px;
  background: var(--bg-secondary);
  border: 2px solid transparent;
  border-radius: 12px;
  cursor: pointer;
  transition: all 0.15s ease;
  overflow: hidden;
}
.theme-card::before {
  /* 背景层：用 inset 提供一个浅阴影，让卡片更有质感 */
  content: '';
  position: absolute;
  inset: 0;
  box-shadow: inset 0 0 0 1px rgba(0, 0, 0, 0.04);
  border-radius: 10px;
  pointer-events: none;
}
.theme-card:hover {
  transform: translateY(-2px);
  box-shadow: 0 6px 20px rgba(0, 0, 0, 0.10);
}
.theme-card.active {
  border-color: var(--accent);
  box-shadow: 0 6px 24px var(--accent-alpha-35);
}
.theme-card .card-overlay {
  position: absolute;
  inset: 0;
  border-radius: 10px;
  z-index: 0;
  pointer-events: none;
  opacity: 1;
}
.theme-card.hasBg .swatch,
.theme-card.hasBg .label {
  position: relative;
  z-index: 1;
}
.theme-card .swatch {
  width: 60px;
  height: 60px;
  border-radius: 50%;
  box-shadow: 0 4px 14px rgba(0, 0, 0, 0.15), inset 0 -4px 10px rgba(0, 0, 0, 0.10);
  flex-shrink: 0;
}
.theme-card.add {
  background: #f5f5f5;
  border: 2px dashed transparent;
  color: #8a8a8a;
}
.theme-card.add:hover {
  background: #ededed;
  border-color: #cfcfcf;
}
.theme-card.add .label { color: #8a8a8a; }
.theme-card .swatch.plus {
  background: #ececec;
  color: #8a8a8a;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: 2px dashed #cfcfcf;
  box-shadow: none;
}
.theme-card .swatch.plus svg { color: #8a8a8a; }
.theme-card .swatch.small {
  width: 18px;
  height: 18px;
  margin-top: 4px;
  box-shadow: 0 2px 6px rgba(0, 0, 0, 0.2), inset 0 0 0 2px rgba(255,255,255,0.3);
}
.theme-card .label {
  font-size: 12px;
  color: var(--text-secondary);
  font-weight: 700;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  text-align: center;
}
.check {
  position: absolute;
  top: 8px;
  right: 8px;
  width: 22px;
  height: 22px;
  border-radius: 50%;
  background: var(--accent);
  color: var(--accent-contrast);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-size: 12px;
  box-shadow: 0 2px 6px rgba(0, 0, 0, 0.25);
  z-index: 2;
}

/* 主题卡片上的编辑按钮（右上角，hover 显示） */
.card-actions-top {
  position: absolute;
  top: 6px;
  right: 6px;
  display: flex;
  gap: 4px;
  opacity: 0;
  transition: opacity 0.15s ease;
  z-index: 3;
}
.theme-card:hover .card-actions-top {
  opacity: 1;
}

/* 自定义主题卡片上的删除按钮（右下角，hover 显示） */
.card-actions {
  position: absolute;
  bottom: 6px;
  right: 6px;
  display: flex;
  gap: 4px;
  opacity: 0;
  transition: opacity 0.15s ease;
  z-index: 3;
}
.theme-card:hover .card-actions {
  opacity: 1;
}
.mini-btn {
  width: 24px;
  height: 24px;
  border-radius: 6px;
  border: 1px solid var(--border);
  background: var(--bg-card);
  color: var(--text-secondary);
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  transition: all 0.15s ease;
}
.mini-btn:hover {
  background: var(--accent);
  color: var(--accent-contrast);
  border-color: var(--accent);
}
.mini-btn.danger:hover {
  background: var(--danger);
  border-color: var(--danger);
  color: #fff;
}

/* ============ 数据源 / 关于 ============ */
.source-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.source-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 14px;
  background: var(--bg-secondary);
  border: 1px solid var(--border);
  border-radius: 8px;
  font-size: 13px;
}
.source-item .dot {
  width: 8px; height: 8px; border-radius: 50%;
}
.source-item .name { font-weight: 600; }
.source-item .muted {
  color: var(--text-muted);
  font-family: ui-monospace, Menlo, Monaco, Consolas, monospace;
  font-size: 11px;
  margin-left: auto;
}
.empty {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  padding: 32px;
  background: var(--bg-secondary);
  border: 1px dashed var(--border);
  border-radius: 10px;
  color: var(--text-muted);
  font-size: 13px;
}
.about-card {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 16px 18px;
  background: linear-gradient(135deg, var(--accent-alpha-10), transparent 75%);
  border: 1px solid var(--border);
  border-radius: 12px;
}
.about-icon {
  width: 52px; height: 52px;
  border-radius: 14px;
  background: var(--accent);
  color: var(--accent-contrast);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  box-shadow: 0 6px 18px var(--accent-alpha-35);
}
.about-card h3 { margin: 0 0 4px; font-size: 16px; font-weight: 700; }
.about-card p { margin: 0 0 4px; font-size: 13px; color: var(--text-secondary); }
.about-card small { color: var(--text-muted); font-size: 12px; }
.about-actions {
  margin-top: 16px;
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
}

/* ============ 声明卡片 ============ */
.disclaimer-card {
  display: flex;
  gap: 14px;
  padding: 16px 18px;
  background: linear-gradient(135deg, rgba(255, 193, 7, 0.08), transparent 75%);
  border: 1px solid var(--border);
  border-radius: 12px;
}
.disclaimer-icon {
  flex-shrink: 0;
  width: 44px;
  height: 44px;
  border-radius: 12px;
  background: var(--warning);
  color: #1a1a1a;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  box-shadow: 0 4px 12px var(--warning-alpha-10);
}
.disclaimer-content {
  flex: 1;
  min-width: 0;
}
.disclaimer-content p {
  margin: 0 0 6px;
  font-size: 12.5px;
  line-height: 1.55;
  color: var(--text-secondary);
}
.disclaimer-content p:last-child { margin-bottom: 0; }

/* ============ 模态框 ============ */
.modal-mask {
  position: fixed; inset: 0;
  background: rgba(0,0,0,0.55);
  display: flex; align-items: center; justify-content: center;
  z-index: 100;
  animation: fadeIn 0.15s ease;
}
@keyframes fadeIn { from { opacity: 0; } to { opacity: 1; } }
.modal {
  width: min(780px, 94vw);
  max-height: 90vh;
  background: var(--bg-card);
  color: var(--text-primary);
  border: 1px solid var(--border);
  border-radius: 14px;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  box-shadow: 0 24px 60px rgba(0,0,0,0.35);
}
.modal-head {
  display: flex; align-items: center; justify-content: space-between;
  padding: 14px 18px;
  border-bottom: 1px solid var(--border);
}
.modal-head h3 { margin: 0; font-size: 15px; font-weight: 700; }
.close-btn {
  background: transparent; border: none;
  color: var(--text-muted); cursor: pointer;
  padding: 4px; border-radius: 6px;
}
.close-btn:hover { background: var(--bg-hover); color: var(--text-primary); }

.modal-body {
  padding: 18px 20px;
  overflow-y: auto;
}
.edit-row {
  display: flex;
  align-items: flex-end;
  gap: 20px;
  padding-bottom: 14px;
  margin-bottom: 10px;
  border-bottom: 1px dashed var(--border);
  flex-wrap: wrap;
}
.field { display: flex; flex-direction: column; gap: 4px; flex: 1; min-width: 180px; }
.field label { font-size: 11px; color: var(--text-muted); font-weight: 600; }
.field input[type='text'] {
  padding: 8px 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--bg-secondary);
  color: var(--text-primary);
  font-size: 13px;
  outline: none;
}
.field input[type='text']:focus { border-color: var(--accent); }
.flags { display: flex; gap: 18px; align-items: center; flex-wrap: wrap; }
.derive-btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 8px 14px;
  background: var(--accent);
  color: var(--accent-contrast);
  border: none;
  border-radius: 8px;
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
  transition: all 0.15s ease;
}
.derive-btn:hover { background: var(--accent-dim); transform: translateY(-1px); }

.group-title {
  font-size: 11px;
  color: var(--text-muted);
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 1px;
  margin: 12px 0 8px;
}
.picker-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
  gap: 10px;
  margin-bottom: 6px;
}
.picker-grid.small {
  grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
}
.picker-item label {
  display: block;
  font-size: 11px;
  color: var(--text-muted);
  margin-bottom: 4px;
  font-weight: 600;
}
.picker-cell {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 10px;
  background: var(--bg-secondary);
  border: 1px solid var(--border);
  border-radius: 10px;
}
.picker-cell input[type='color'] {
  width: 32px; height: 28px;
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 0;
  background: transparent;
  cursor: pointer;
}
.picker-cell span {
  font-size: 11px;
  color: var(--text-secondary);
  font-family: ui-monospace, Menlo, Monaco, Consolas, monospace;
}
.picker-cell.range-cell {
  gap: 10px;
}
.picker-cell.range-cell input[type='range'] {
  flex: 1;
  height: 4px;
  -webkit-appearance: none;
  appearance: none;
  background: var(--border);
  border-radius: 2px;
  outline: none;
}
.picker-cell.range-cell input[type='range']::-webkit-slider-thumb {
  -webkit-appearance: none;
  width: 16px;
  height: 16px;
  border-radius: 50%;
  background: var(--accent);
  cursor: pointer;
  box-shadow: 0 2px 6px rgba(0, 0, 0, 0.2);
}
.bg-drop-zone {
  position: relative;
  margin-top: 14px;
  border: 2px dashed var(--border);
  border-radius: 10px;
  min-height: 180px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--bg-card);
  cursor: pointer;
  transition: all 0.15s ease;
  overflow: hidden;
}
.bg-drop-zone:hover { border-color: var(--accent); background: var(--bg-hover); }
.bg-drop-zone.has-image { border-style: solid; cursor: default; padding: 0; }
.bg-drop-zone.has-image:hover { background: var(--bg-card); }
.bg-drop-zone img {
  display: block;
  width: 100%;
  height: auto;
  max-height: 240px;
  object-fit: cover;
}
.bg-drop-hint {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
  color: var(--text-secondary);
}
.bg-drop-hint span { font-size: 13px; }
.bg-remove {
  position: absolute;
  top: 8px;
  right: 8px;
  width: 28px;
  height: 28px;
  border-radius: 50%;
  background: rgba(0,0,0,0.55);
  color: #fff;
  border: none;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 2;
  transition: background 0.15s ease;
}
.bg-remove:hover { background: var(--danger); }

.modal-foot {
  display: flex; align-items: center; gap: 10px;
  padding: 14px 20px;
  border-top: 1px solid var(--border);
  background: var(--bg-secondary);
}

/* 导入弹窗底部 - 与 modal-footer 与 modal-foot 统一 */
.modal-footer {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 10px;
  padding: 14px 20px;
  border-top: 1px solid var(--border);
  background: var(--bg-secondary);
  border-bottom-left-radius: 14px;
  border-bottom-right-radius: 14px;
}
.modal-footer .btn {
  padding: 9px 18px;
  font-size: 13px;
  border-radius: 9px;
  min-width: 80px;
  justify-content: center;
}
.btn {
  display: inline-flex; align-items: center; gap: 6px;
  padding: 8px 16px;
  border-radius: 8px;
  border: 1px solid var(--border);
  background: var(--bg-card);
  color: var(--text-primary);
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  transition: all 0.15s ease;
}
.btn:hover { background: var(--bg-hover); }
.btn.primary {
  background: var(--accent);
  color: var(--accent-contrast);
  border-color: var(--accent);
}
.btn.primary:hover { background: var(--accent-dim); }
.btn.danger {
  background: transparent;
  border-color: var(--danger);
  color: var(--danger);
}
.btn.danger:hover { background: var(--danger); color: #fff; border-color: var(--danger); }
.btn.btn-secondary {
  background: var(--bg-secondary);
  color: var(--text-primary);
}
.btn[disabled] {
  opacity: 0.5;
  cursor: not-allowed;
}
.setting-input {
  flex: 1;
  min-width: 200px;
  padding: 8px 12px;
  border-radius: 8px;
  border: 1px solid var(--border);
  background: var(--bg-card);
  color: var(--text-primary);
  font-size: 13px;
  font-family: inherit;
  outline: none;
  transition: border-color 0.15s ease;
}
.setting-input:focus {
  border-color: var(--accent);
}
.block .hint {
  margin-top: 8px;
  font-size: 12px;
  color: var(--text-muted);
  line-height: 1.5;
}

/* -------- 采集调度面板样式 -------- */
.desc {
  color: var(--text-muted);
  font-size: 13px;
  margin: 0 0 12px 0;
}
.schedule-card {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 12px;
  padding: 16px 18px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.schedule-card .row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  min-height: 36px;
}
.schedule-card .row.toggle { justify-content: flex-start; gap: 10px; }
.row-right { display: inline-flex; align-items: center; gap: 8px; }
.row-right.grow { flex: 1; min-width: 0; }
.row-right.grow .select-dropdown { width: 320px; max-width: 100%; }

/* 气泡编辑模式 */
.bubble-btn {
  background: var(--bg-tag);
  border: 1px solid var(--border);
  color: var(--accent);
  padding: 6px 14px;
  border-radius: 999px;
  font-size: 13px;
  font-weight: 500;
  font-variant-numeric: tabular-nums;
  cursor: pointer;
  transition: all 0.15s ease;
  min-width: 70px;
}
.bubble-btn:hover {
  background: var(--accent-alpha-15);
  border-color: var(--accent);
  color: var(--accent);
}
.bubble-input {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.bubble-input input {
  width: 80px;
  padding: 6px 10px;
  border-radius: 8px;
  border: 1px solid var(--accent);
  background: var(--bg-input);
  color: var(--text-primary);
  font-size: 13px;
  font-variant-numeric: tabular-nums;
  text-align: right;
  outline: none;
  box-shadow: 0 0 0 3px var(--accent-alpha-20);
}
.bubble-input .unit { color: var(--text-muted); font-size: 13px; }

.row-right .unit { color: var(--text-muted); font-size: 13px; }
.row-right .hint { font-size: 12px; margin-left: 4px; }
.row.actions {
  margin-top: 6px;
  padding-top: 12px;
  border-top: 1px solid var(--border);
  flex-wrap: wrap;
  justify-content: flex-start;
  gap: 10px;
}
.row.actions button {
  padding: 8px 18px;
  border-radius: 8px;
  border: 1px solid var(--border);
  font-size: 13px;
  cursor: pointer;
  transition: all 0.15s ease;
}
.row.actions .primary {
  background: var(--accent);
  color: var(--accent-contrast);
  border-color: var(--accent);
}
.row.actions .primary:hover:not(:disabled) { background: var(--accent-dim); }
.row.actions .primary:disabled { opacity: 0.6; cursor: not-allowed; }
.row.actions .secondary {
  background: var(--bg-card);
  color: var(--text-primary);
  border-color: var(--border);
}
.row.actions .secondary:hover { background: var(--bg-hover); }
.row.actions .danger {
  background: transparent;
  color: var(--danger);
  border-color: var(--danger);
}
.row.actions .danger:hover { background: var(--danger); color: #fff; }

/* 采集调度开关样式 */
.schedule-card .toggle-label {
  position: relative;
  display: inline-flex;
  align-items: center;
  gap: 10px;
  cursor: pointer;
  font-size: 14px;
  color: var(--text-primary);
  user-select: none;
}
.schedule-card .toggle-label input { display: none; }
.schedule-card .switch {
  position: relative;
  display: inline-block;
  width: 36px;
  height: 20px;
  background: var(--border);
  border-radius: 999px;
  transition: background 0.15s ease;
  flex-shrink: 0;
}
.schedule-card .switch::after {
  content: '';
  position: absolute;
  top: 2px;
  left: 2px;
  width: 16px;
  height: 16px;
  background: #fff;
  border-radius: 50%;
  box-shadow: 0 1px 3px rgba(0,0,0,0.2);
  transition: transform 0.15s ease;
}
.schedule-card .toggle-label input:checked ~ .switch { background: var(--accent); }
.schedule-card .toggle-label input:checked ~ .switch::after { transform: translateX(16px); }

.schedule-status {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 12px;
  padding: 14px 18px;
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px 16px;
  font-size: 13px;
  color: var(--text-primary);
}
.schedule-status .muted { color: var(--text-muted); margin-right: 6px; }
.schedule-status strong.running {
  color: var(--accent);
  position: relative;
  padding-left: 14px;
}
.schedule-status strong.running::before {
  content: '';
  position: absolute;
  left: 0;
  top: 50%;
  transform: translateY(-50%);
  width: 8px;
  height: 8px;
  background: var(--accent);
  border-radius: 50%;
  box-shadow: 0 0 0 3px var(--accent-alpha-20);
  animation: pulse 1.5s ease-in-out infinite;
}
@keyframes pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.4; }
}
@media (max-width: 640px) {
  .schedule-status { grid-template-columns: 1fr; }
}

.log-box {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 12px;
  padding: 12px 14px;
  font-family: 'SF Mono', Consolas, monospace;
  font-size: 12px;
  color: var(--text-secondary);
  max-height: 220px;
  overflow-y: auto;
}
.log-box .log-line { line-height: 1.6; }

/* ---------- 日志 viewer ---------- */
.log-toolbar {
  display: flex;
  gap: 10px;
  align-items: center;
  margin: 12px 0 8px 0;
  flex-wrap: wrap;
}
.log-toolbar select {
  background: var(--bg-secondary);
  color: var(--text-primary);
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 6px 10px;
  font-size: 12px;
  outline: none;
}
.log-toolbar .btn {
  background: var(--bg-secondary);
  color: var(--text-primary);
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 6px 12px;
  font-size: 12px;
  cursor: pointer;
  transition: background .15s, border-color .15s;
}
.log-toolbar .btn:hover {
  background: var(--bg-hover);
  border-color: var(--accent);
}
.log-toolbar .btn.danger { color: var(--danger); border-color: var(--danger); }
.log-toolbar .btn.danger:hover { background: var(--danger); color: #fff; }
.log-toolbar .btn:disabled { opacity: .4; cursor: not-allowed; }

.log-dir { margin: 6px 0 10px 0; font-size: 11px; color: var(--text-muted); }
.log-dir code { background: var(--bg-secondary); padding: 2px 6px; border-radius: 4px; }

.log-viewer {
  margin-top: 8px;
  background: var(--bg-secondary);
  border: 1px solid var(--border);
  border-radius: 10px;
  padding: 12px 14px;
  max-height: 420px;
  overflow-y: auto;
}
.log-viewer pre {
  margin: 0;
  font-size: 11px;
  line-height: 1.55;
  color: var(--text-secondary);
  white-space: pre-wrap;
  word-break: break-word;
}
.log-viewer pre + pre { margin-top: 8px; padding-top: 8px; border-top: 1px dashed var(--border); }
.log-session-entry { color: var(--text-primary) !important; }
.log-empty {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 30px 0;
  color: var(--text-muted);
  font-size: 13px;
  justify-content: center;
}

/* ============ 数据源管理面板新增样式 ============ */
.source-panel .source-list .source-item {
  cursor: pointer;
  align-items: center;
}
.source-panel .source-list .source-item .meta {
  display: flex;
  flex-direction: column;
  gap: 2px;
  flex: 1;
  min-width: 0;
}
.source-panel .source-list .source-item .name {
  font-size: 14px;
  font-weight: 600;
  color: var(--text-primary);
}
.source-panel .source-list .source-item.active {
  border-color: var(--accent);
  background: var(--accent-alpha-10);
}

.source-detail {
  margin-top: 10px;
  padding: 14px 16px 10px;
  background: var(--bg-secondary);
  border: 1px solid var(--border);
  border-radius: 10px;
}
.detail-head {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding-bottom: 12px;
  border-bottom: 1px dashed var(--border);
  margin-bottom: 12px;
}
.detail-head .d-name { font-size: 16px; font-weight: 700; color: var(--text-primary); }
.detail-head .d-key {
  font-size: 11px; font-family: ui-monospace, Menlo, Monaco, Consolas, monospace;
  color: var(--text-muted);
  margin-top: 2px;
}
.detail-head .d-url {
  font-size: 11px; color: var(--text-muted); word-break: break-all; margin-top: 2px;
}
.detail-head .d-actions {
  display: flex; flex-wrap: wrap; gap: 8px;
}

.source-toolbar {
  display: flex;
  align-items: center;
  gap: 14px;
  flex-wrap: wrap;
  margin: 10px 0 18px;
  padding: 14px 16px;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 12px;
}
.source-toolbar .btn {
  padding: 10px 18px;
  font-size: 13px;
  border-radius: 10px;
  box-shadow: 0 2px 8px var(--accent-alpha-20);
}
.source-toolbar .btn.primary {
  background: linear-gradient(135deg, var(--accent), var(--accent-dim));
  border: none;
}
.source-toolbar .btn.primary:hover {
  transform: translateY(-1px);
  box-shadow: 0 4px 12px var(--accent-alpha-35);
  background: linear-gradient(135deg, var(--accent), var(--accent-dim));
}
.source-toolbar .hint {
  font-size: 12px;
  color: var(--text-muted);
}

/* 源详情头部 */
.detail-head .d-actions .btn {
  padding: 8px 14px;
  font-size: 12px;
  border-radius: 8px;
}

.export-result {
  margin: 10px 0 16px;
  padding: 14px 16px;
  background: var(--bg-card);
  border: 1px dashed var(--accent);
  border-radius: 12px;
  font-size: 13px;
  color: var(--text-primary);
}
.export-result .export-path {
  font-family: ui-monospace, Menlo, Monaco, Consolas, monospace;
  font-size: 12px;
  word-break: break-all;
  margin-bottom: 10px;
  padding: 8px 10px;
  background: var(--bg-secondary);
  border-radius: 8px;
}
.export-result .export-actions {
  display: flex;
  gap: 8px;
  margin-bottom: 10px;
}
.export-result .btn {
  padding: 8px 14px;
  font-size: 12px;
  border-radius: 8px;
}
.export-result .hint {
  font-size: 12px;
  color: var(--text-muted);
}

.file-picker {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}
.file-picker .file-name {
  font-family: ui-monospace, Menlo, Monaco, Consolas, monospace;
  font-size: 12px;
  color: var(--text-secondary);
  word-break: break-all;
}

.import-result {
  margin-top: 14px;
  padding: 10px 12px;
  background: var(--bg-secondary);
  border: 1px solid var(--border);
  border-radius: 10px;
  font-size: 13px;
  color: var(--text-primary);
}

/* ========== 拖拽上传区域 ========== */
.drop-zone {
  margin: 16px 0;
  padding: 32px 16px;
  border: 2px dashed var(--border);
  border-radius: 14px;
  background: var(--bg-secondary);
  cursor: pointer;
  transition: all 0.2s ease;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
}

.drop-zone:hover {
  border-color: var(--accent);
  background: rgba(var(--accent-rgb), 0.05);
}

.drop-zone.dragging {
  border-color: var(--accent);
  border-style: solid;
  background: rgba(var(--accent-rgb), 0.1);
  transform: scale(1.02);
  box-shadow: 0 0 20px rgba(var(--accent-rgb), 0.2);
}

.drop-icon {
  width: 64px;
  height: 64px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 16px;
  background: var(--accent);
  color: var(--accent-contrast);
  transition: transform 0.2s ease;
}

.drop-zone:hover .drop-icon,
.drop-zone.dragging .drop-icon {
  transform: scale(1.1);
}

.drop-title {
  font-size: 16px;
  font-weight: 600;
  color: var(--text-primary);
  margin: 0;
}

.drop-subtitle {
  font-size: 13px;
  color: var(--text-muted);
  margin: 0;
}

.drop-format {
  font-size: 12px;
  color: var(--text-muted);
  margin: 0;
  padding: 4px 10px;
  background: var(--bg-card);
  border-radius: 20px;
}

/* 已选择的文件 */
.selected-file {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 12px;
  background: var(--bg-card);
  border: 1px solid var(--accent);
  border-radius: 10px;
  margin-top: -8px;
  font-size: 13px;
  color: var(--text-primary);
}

.selected-file .clear-btn {
  margin-left: auto;
  padding: 4px;
  background: transparent;
  border: none;
  border-radius: 6px;
  cursor: pointer;
  color: var(--text-muted);
  transition: all 0.15s ease;
}

.selected-file .clear-btn:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

.tables {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
  gap: 10px;
  margin-bottom: 16px;
}
.table-card {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 10px;
  padding: 10px 12px;
}
.t-head {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
  font-size: 12px;
}
.t-head .t-name {
  font-family: ui-monospace, Menlo, Monaco, Consolas, monospace;
  font-weight: 600;
  color: var(--text-primary);
}
.t-head .t-role {
  padding: 2px 8px;
  background: var(--accent-alpha-10);
  color: var(--accent);
  border-radius: 999px;
  font-size: 11px;
  font-weight: 600;
}
.t-head .t-count {
  margin-left: auto;
  color: var(--text-muted);
  font-size: 11px;
}

.col-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 12px;
  color: var(--text-secondary);
}
.col-table th, .col-table td {
  text-align: left;
  padding: 4px 6px;
  border-bottom: 1px solid var(--border);
}
.col-table th {
  font-weight: 600;
  color: var(--text-muted);
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}
.col-table tr:last-child td { border-bottom: none; }

.samples { margin-bottom: 16px; }
.samples h4 {
  font-size: 12px;
  margin: 6px 0 8px;
  color: var(--text-primary);
  font-weight: 700;
  letter-spacing: 0.3px;
}
.sample-list {
  display: flex; flex-direction: column; gap: 8px;
}
.sample-row {
  display: flex; align-items: center; gap: 10px;
  padding: 8px 10px;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 8px;
}
.sample-pic {
  width: 48px; height: 64px; object-fit: cover; border-radius: 4px;
  background: var(--bg-secondary);
}
.sample-meta { flex: 1; min-width: 0; }
.sample-meta .title {
  font-size: 13px; font-weight: 600; color: var(--text-primary);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.sample-meta .small { margin-top: 2px; }

.sample-card {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 8px;
  overflow: hidden;
}
.sample-head {
  display: flex; align-items: flex-start; gap: 10px;
  padding: 10px 12px;
  cursor: pointer;
  user-select: none;
  transition: background 0.15s;
}
.sample-head:hover { background: var(--bg-hover); }
.sample-head .chevron {
  font-size: 20px; color: var(--text-muted);
  transition: transform 0.2s;
  line-height: 1;
  padding-top: 2px;
  flex-shrink: 0;
  margin-left: auto;
}
.sample-head .chevron.open { transform: rotate(90deg); }
.sample-pic {
  width: 48px; height: 64px; object-fit: cover; border-radius: 4px;
  background: var(--bg-secondary);
  flex-shrink: 0;
}
.sample-meta {
  flex: 1;
  min-width: 0;
  display: flex; flex-direction: column; gap: 3px;
}
.sample-meta .title {
  font-size: 13px; font-weight: 600; color: var(--text-primary);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.sample-meta .small {
  font-size: 11px;
  line-height: 1.5;
}
.sample-eps {
  padding: 10px 12px 12px;
  border-top: 1px solid var(--border);
  background: var(--bg-secondary);
}
.sample-eps .inline-btn {
  margin-top: 10px;
  padding: 5px 14px;
  font-size: 12px;
  white-space: nowrap;
  min-width: 90px;
  border: none;
  background: var(--danger);
  color: #fff;
  border-radius: 6px;
  cursor: pointer;
  font-weight: 600;
}
.sample-eps .inline-btn:hover {
  opacity: 0.85;
}
.samples.compact .desc {
  margin: 0; line-height: 1.6;
}
.samples h4 .hint {
  font-size: 11px; font-weight: normal; color: var(--text-muted);
  margin-left: 8px;
}

.mono { font-family: ui-monospace, Menlo, Monaco, Consolas, monospace; }
.small { font-size: 11px; }
.break-all { word-break: break-all; }

/* ---------- 日志查看器新样式 ---------- */
.panel-header {
  margin-bottom: 4px;
}
.panel-header h3 {
  font-size: 15px;
  font-weight: 700;
  margin: 0 0 4px;
}
.panel-header .hint {
  font-size: 12px;
  color: var(--text-muted);
  margin: 0;
}

/* 日志过滤栏 */
.log-filter-bar {
  display: flex;
  gap: 10px;
  align-items: center;
  margin: 10px 0 8px 0;
  flex-wrap: wrap;
}
.log-search {
  flex: 1;
  min-width: 160px;
  padding: 7px 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--bg-secondary);
  color: var(--text-primary);
  font-size: 13px;
  outline: none;
  transition: border-color 0.15s ease;
}
.log-search:focus {
  border-color: var(--accent);
}
.log-level-filter {
  -webkit-appearance: none;
  appearance: none;
  padding: 7px 28px 7px 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--bg-secondary) url("data:image/svg+xml;charset=UTF-8,%3csvg xmlns='http://www.w3.org/2000/svg' width='12' height='12' viewBox='0 0 24 24' fill='none' stroke='%2364748b' stroke-width='2' stroke-linecap='round' stroke-linejoin='round'%3e%3cpolyline points='6 9 12 15 18 9'%3e%3c/polyline%3e%3c/svg%3e") no-repeat right 8px center;
  color: var(--text-primary);
  font-size: 12px;
  outline: none;
  cursor: pointer;
  font-family: inherit;
}
.log-level-filter:hover {
  border-color: var(--accent);
}
.log-level-filter:focus {
  border-color: var(--accent);
  box-shadow: 0 0 0 2px var(--accent-alpha-20);
}
.log-info {
  font-size: 12px;
  color: var(--text-muted);
  white-space: nowrap;
}

/* 日志查看器容器 */
.log-viewer {
  margin-top: 8px;
  background: var(--bg-secondary);
  border: 1px solid var(--border);
  border-radius: 10px;
  padding: 0;
  max-height: 480px;
  overflow-y: auto;
  font-family: 'SF Mono', 'Cascadia Code', 'Fira Code', 'JetBrains Mono', Consolas, 'Courier New', monospace;
  font-size: 13px;
  line-height: 1.7;
}
.log-lines {
  padding: 8px 0;
}
.log-line {
  padding: 2px 14px;
  color: var(--text-secondary);
  white-space: pre-wrap;
  word-break: break-word;
  transition: background 0.1s ease;
}
.log-line:hover {
  background: var(--bg-hover);
}

/* 日志级别着色 */
.log-line.level-error {
  background: rgba(220, 38, 38, 0.12);
  color: #dc2626;
  border-left: 3px solid #dc2626;
  font-weight: 600;
}
.log-line.level-error:hover {
  background: rgba(220, 38, 38, 0.18);
}
.log-line.level-warn {
  background: rgba(234, 179, 8, 0.10);
  color: #b45309;
  border-left: 3px solid #eab308;
}
.log-line.level-warn:hover {
  background: rgba(234, 179, 8, 0.16);
}
.log-line.level-debug {
  background: transparent;
  color: var(--text-muted);
  font-size: 12px;
  opacity: 0.7;
}
.log-line.level-info {
  background: transparent;
  color: var(--text-secondary);
}

/* 搜索高亮 */
.log-line mark {
  background: #fde047;
  color: #1e1b4b;
  border-radius: 2px;
  padding: 0 2px;
  font-weight: 700;
}

.log-file-select {
  min-width: 180px;
}

@media (max-width: 720px) {
  .tabs { flex-wrap: wrap; }
  .tab { padding: 6px 10px; font-size: 12px; }
}

/* ====== 开发者模式密码弹窗 ====== */
.dev-password-overlay {
  position: fixed;
  inset: 0;
  z-index: 10000;
  background: rgba(0, 0, 0, 0.6);
  display: flex;
  align-items: center;
  justify-content: center;
  backdrop-filter: blur(6px);
  -webkit-backdrop-filter: blur(6px);
}

.dev-password-modal {
  width: min(420px, 90vw);
  background: var(--bg-card);
  border: 1px solid var(--accent-alpha-20);
  border-radius: var(--radius-xl);
  padding: 32px 28px 24px;
  box-shadow: 0 20px 60px rgba(0, 0, 0, 0.4);
  text-align: center;
}

.dev-password-modal h2 {
  margin: 0 0 8px;
  font-size: 20px;
  color: var(--accent);
}

.dev-password-desc {
  margin: 0 0 20px;
  font-size: 13px;
  color: var(--text-muted);
}

.dev-password-input {
  width: 160px;
  padding: 10px 16px;
  font-size: 22px;
  letter-spacing: 8px;
  text-align: center;
  border: 2px solid var(--border-strong);
  border-radius: var(--radius);
  background: var(--bg-input);
  color: var(--text-primary);
  outline: none;
  font-family: monospace;
  transition: border-color 0.2s;
}

.dev-password-input:focus {
  border-color: var(--accent);
  box-shadow: 0 0 0 3px var(--accent-alpha-15);
}

.dev-password-error {
  margin: 10px 0 0;
  color: var(--danger);
  font-size: 13px;
  font-weight: 500;
}

.dev-password-actions {
  display: flex;
  gap: 12px;
  justify-content: center;
  margin-top: 20px;
}

.dev-password-btn {
  padding: 8px 28px;
  border: none;
  border-radius: var(--radius);
  cursor: pointer;
  font-size: 14px;
  font-weight: 600;
  transition: all 0.15s;
  font-family: inherit;
}

.dev-password-btn--cancel {
  background: var(--bg-hover);
  color: var(--text-secondary);
}

.dev-password-btn--cancel:hover {
  background: var(--border);
}

.dev-password-btn--confirm {
  background: var(--accent);
  color: var(--accent-contrast);
}

.dev-password-btn--confirm:hover {
  background: var(--accent-dim);
}

/* 关于应用中可点击的软件名称 */
.app-name-clickable {
  cursor: default;
  user-select: none;
}

/* 基本设置中开发者模式块 */
.block-dev .desc {
  display: block;
  margin-top: 6px;
  color: var(--text-muted);
  font-size: 12px;
  line-height: 1.5;
}
</style>
