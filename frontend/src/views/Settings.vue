<script setup lang="ts">
defineOptions({ name: 'Settings' })
import { ref, reactive, onMounted, onUnmounted, computed, watch } from 'vue'
import { useRoute } from 'vue-router'
import {
  GetSetting, SetSetting, GetCloseBehavior, SetCloseBehavior, RestartApp,
  WindowSetResizable, WindowGetResizable, WindowSetSize, WindowGetSize,
  CheckUpdate, DownloadUpdate, InstallUpdate, GetAppVersion, IgnoreVersion, GetIgnoredVersion,
  GetPendingUpdateInfo, ClearPendingUpdateInfo,
} from '../../bindings/cczjVideo/app'
import { useThemeStore, type CustomTheme, type ColorPalette } from '../stores/theme'
import { useErrorStore } from '../stores/error'
import { useConfirmStore } from '../stores/confirm'
import { useDownloadStore } from '../stores/download'
import { useDevMode } from '../stores/devMode'
import Icon from '../components/Icon.vue'
import { Button, Modal, Segment, Select as SelectDropdown } from '../components/ui'
import { useI18n } from 'vue-i18n'
import { setLocale as saveLocalePreference } from '../locales'
import { stats as tsStats, clear as tsClear, diskCacheInfo, TsCache } from '../utils/tsCache'

const { t } = useI18n()

// ---------- 分组配置（顶部 tab） ----------
interface GroupItem {
  id: string
  label: string
  icon: string
}
const GROUPS = computed<GroupItem[]>(() => [
  { id: 'basic',  label: t('settings.basic'), icon: 'sliders' },
  { id: 'theme',  label: t('settings.theme'), icon: 'palette' },
  { id: 'play',   label: t('settings.playback'), icon: 'play' },
  { id: 'cache',  label: t('settings.cacheManagement'), icon: 'database' },
  { id: 'about',  label: t('settings.about'), icon: 'info' },
])

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
const layoutDensity = ref<'comfortable' | 'compact' | 'spacious'>('comfortable')
const playbackAutoPlay = ref(true)
const playbackAutoNext = ref(true)
const playbackSpeed = ref<number>(1)


// 窗口设置
const windowResizable = ref(true)
const windowWidth = ref<number>(1280)
const windowHeight = ref<number>(800)

// 窗口尺寸预设：更小/小/中/大/更大/超大/巨大
const windowSizePresets = computed(() => [
  { key: 'xs',   label: t('settings.windowSizePresets.xs'), width: 1024, height: 640 },
  { key: 'sm',   label: t('settings.windowSizePresets.sm'), width: 1152, height: 720 },
  { key: 'md',   label: t('settings.windowSizePresets.md'), width: 1280, height: 800 },
  { key: 'lg',   label: t('settings.windowSizePresets.lg'), width: 1366, height: 854 },
  { key: 'xl',   label: t('settings.windowSizePresets.xl'), width: 1440, height: 900 },
  { key: 'xxl',  label: t('settings.windowSizePresets.xxl'), width: 1600, height: 1000 },
  { key: 'huge', label: t('settings.windowSizePresets.huge'), width: 1920, height: 1080 },
])
const windowSizeKey = ref('md')

// 字体大小预设：更小/小/标准/大/更大/非常大
const fontSizePresets = computed(() => [
  { key: 'xs',    label: t('settings.fontSizePresets.xs'), px: 12 },
  { key: 'sm',    label: t('settings.fontSizePresets.sm'), px: 13 },
  { key: 'md',    label: t('settings.fontSizePresets.md'), px: 14 },
  { key: 'lg',    label: t('settings.fontSizePresets.lg'), px: 16 },
  { key: 'xl',    label: t('settings.fontSizePresets.xl'), px: 18 },
  { key: 'xxl',   label: t('settings.fontSizePresets.xxl'), px: 20 },
])
const fontSizeKey = ref('md')

// 字体设置
const fontFamily = ref<string>('')
const fontFamilyOptions = computed(() => [
  { value: '',     label: t('settings.fontOptions.default') },
  { value: '"Microsoft YaHei", "微软雅黑", sans-serif', label: t('settings.fontOptions.yahei') },
  { value: '"Source Han Sans SC", "思源黑体", sans-serif', label: t('settings.fontOptions.sourceHan') },
  { value: '"SimSun", "宋体", serif', label: t('settings.fontOptions.simsun') },
  { value: '"KaiTi", "楷体", serif', label: t('settings.fontOptions.kaiti') },
])

// 语言
const language = ref<string>('zh-CN')
const languageOptions = computed(() => [
  { value: 'zh-CN', label: t('settings.languageOptions.zhCN') },
  { value: 'en',    label: t('settings.languageOptions.en') },
])

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
    // 匹配最近的预设
    const match = windowSizePresets.value.find(p => p.width === size.width && p.height === size.height)
    if (match) windowSizeKey.value = match.key
  } catch { /* 忽略 */ }
}

async function applyWindowPreset(key: string): Promise<void> {
  windowSizeKey.value = key
  const preset = windowSizePresets.value.find(p => p.key === key)
  if (!preset) return
  windowWidth.value = preset.width
  windowHeight.value = preset.height
  try { await WindowSetSize(preset.width, preset.height) } catch { /* 忽略 */ }
  await save('window_size_key', key)
}

function applyFontPreset(key: string): void {
  fontSizeKey.value = key
  const preset = fontSizePresets.value.find(p => p.key === key)
  if (!preset) return
  document.documentElement.style.fontSize = preset.px + 'px'
  document.documentElement.style.setProperty('--font-size-base', preset.px + 'px')
  save('font_size_key', key)
}

function applyFontFamily(): void {
  if (fontFamily.value) {
    document.documentElement.style.fontFamily = fontFamily.value
  } else {
    document.documentElement.style.removeProperty('font-family')
  }
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

// ---------- 版本更新 ----------
const appVersion = ref('1.1.0')
const updateChecking = ref(false)
const updateDownloading = ref(false)
const updateDownloaded = ref(false)
const updateFilePath = ref('')
const updateInfo = ref<{
  has_update: boolean
  current_version: string
  latest_version: string
  release_name: string
  release_notes: string
  download_url: string
  asset_name: string
  asset_size: number
  published_at: string
  history?: { version: string; desc: string }[]
} | null>(null)
const updateDownloadProgress = reactive({
  downloaded: 0,
  total: 0,
  speed_bps: 0,
  percent: 0,
})
const updateModalOpen = ref(false)
const updateModalDownloaded = ref(false)
const updateInstalling = ref(false)
const updateCheckFailed = ref(false) // 检查更新失败状态
const tryAutoUpdate = ref(true) // 发现新版本时是否自动下载（参考 lx-music-desktop）
const showChangeLog = ref(true) // 版本变化时是否展示更新日志（参考 lx-music-desktop）
const changelogModalOpen = ref(false) // 更新日志弹窗
const changelogLastVersion = ref('') // 上次启动的版本号

// 一周内不再提醒检查失败
function canShowUpdateFailTip(): boolean {
  const lastTip = localStorage.getItem('update__check_failed_tip')
  if (!lastTip) return true
  return Date.now() - parseInt(lastTip) > 7 * 86400000
}
function dismissUpdateFailTip(): void {
  localStorage.setItem('update__check_failed_tip', Date.now().toString())
  updateCheckFailed.value = false
  updateModalOpen.value = false
}

function fmtSize(bytes: number): string {
  if (bytes <= 0) return '未知'
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB'
  return (bytes / (1024 * 1024)).toFixed(1) + ' MB'
}

function fmtSpeed(bps: number): string {
  if (bps <= 0) return ''
  if (bps < 1024) return bps.toFixed(0) + ' B/s'
  if (bps < 1024 * 1024) return (bps / 1024).toFixed(1) + ' KB/s'
  return (bps / (1024 * 1024)).toFixed(1) + ' MB/s'
}

async function doCheckUpdate(): Promise<void> {
  updateChecking.value = true
  updateCheckFailed.value = false
  try {
    const info = await CheckUpdate(true) // reCheck: 强制刷新缓存
    if (!info) return
    updateInfo.value = info
    if (info.has_update) {
      updateModalOpen.value = true
      // tryAutoUpdate: 自动下载更新（参考 lx-music-desktop）
      if (tryAutoUpdate.value && info.download_url) {
        updateDownloading.value = true
        doDownloadUpdate().catch(() => {})
      }
    } else {
      errorStore.info('检查更新', `当前已是最新版本 ${info.current_version}`)
    }
  } catch (e: any) {
    // 检查更新失败：显示友好提示（参考 lx-music-desktop 的 isUnknown 状态）
    updateCheckFailed.value = true
    if (canShowUpdateFailTip()) {
      updateModalOpen.value = true
    } else {
      errorStore.fromError('检查更新失败', e, 'Settings.checkUpdate')
    }
  } finally {
    updateChecking.value = false
  }
}

async function doDownloadUpdate(): Promise<void> {
  if (!updateInfo.value?.download_url) return
  updateDownloading.value = true
  updateDownloadProgress.downloaded = 0
  updateDownloadProgress.total = 0
  updateDownloadProgress.percent = 0
  updateDownloadProgress.speed_bps = 0
  try {
    const path = await DownloadUpdate(updateInfo.value.download_url)
    updateFilePath.value = path
    updateDownloaded.value = true
    updateModalDownloaded.value = true
  } catch (e: any) {
    errorStore.fromError('下载更新失败', e, 'Settings.downloadUpdate')
    // 下载失败一次提示（参考 lx-music-desktop 的 update__download_failed_tip）
    if (!localStorage.getItem('update__download_failed_tip')) {
      setTimeout(() => {
        errorStore.info('下载提示', '下载更新失败，可能是网络问题。你可以稍后重试，或手动前往发布页下载更新。')
        localStorage.setItem('update__download_failed_tip', '1')
      }, 500)
    }
  } finally {
    updateDownloading.value = false
  }
}

async function doInstallUpdate(): Promise<void> {
  if (!updateFilePath.value) return
  updateInstalling.value = true
  try {
    await ClearPendingUpdateInfo()
    await InstallUpdate(updateFilePath.value)
  } catch (e: any) {
    errorStore.fromError('安装更新失败', e, 'Settings.installUpdate')
    updateInstalling.value = false
  }
}

async function doIgnoreVersion(): Promise<void> {
  if (!updateInfo.value?.latest_version) return
  try {
    await IgnoreVersion(updateInfo.value.latest_version)
    await ClearPendingUpdateInfo()
    updateModalOpen.value = false
    updateModalDownloaded.value = false
  } catch { /* ignore */ }
}

// 监听下载进度事件
function onUpdateDownloadProgress(data: any): void {
  if (data.total > 0) {
    updateDownloadProgress.total = data.total
    updateDownloadProgress.percent = Math.round((data.downloaded / data.total) * 100)
  }
  updateDownloadProgress.downloaded = data.downloaded
  updateDownloadProgress.speed_bps = data.speed_bps
}

// 监听自动检查更新事件（来自 App.vue 或 Go 后端启动检查）
function onUpdateAvailable(data: any): void {
  updateInfo.value = data
  updateModalOpen.value = true
}

// 监听版本变化事件（参考 lx-music-desktop 的 ChangeLogModal）
function onVersionChanged(data: any): void {
  if (!data || !data.old_version) return
  changelogLastVersion.value = data.old_version
  // 检查是否应该显示更新日志
  if (showChangeLog.value && data.new_version && data.old_version) {
    changelogModalOpen.value = true
  }
}

// 计算当前版本的更新日志（参考 lx-music-desktop 的 ChangeLogModal）
const changelogInfo = computed(() => {
  const currentVer = appVersion.value
  const lastVer = changelogLastVersion.value
  const info = {
    version: currentVer,
    desc: '',
    history: [] as Array<{ version: string; desc: string }>,
    isLatest: true,
  }

  if (!updateInfo.value) return info

  // 构建完整的版本历史列表（当前最新版本 + 历史版本）
  const allVersions = [
    {
      version: updateInfo.value.latest_version,
      desc: updateInfo.value.release_notes || '',
    },
    ...(updateInfo.value.history || []),
  ]

  // 检查当前版本是否是最新
  info.isLatest = currentVer >= updateInfo.value.latest_version

  if (lastVer) {
    // 有上次启动版本号：精确筛选从上次版本到当前版本之间的变更
    for (const ver of allVersions) {
      if (ver.version === currentVer) {
        info.version = ver.version
        info.desc = ver.desc
        // 找到当前版本后，把其余大于 lastVer 的版本作为历史
        for (const v of allVersions) {
          if (v.version > lastVer && v.version < currentVer) {
            info.history.push(v)
          }
        }
        break
      }
    }
  } else {
    // 首次启动：只显示当前版本信息
    const found = allVersions.find(v => v.version === currentVer)
    if (found) {
      info.version = found.version
      info.desc = found.desc
    } else {
      info.desc = '未找到当前版本的更新日志'
    }
  }

  // 设置历史版本
  if (info.history.length === 0 && updateInfo.value.history) {
    info.history = updateInfo.value.history
      .filter((v: any) => v.version < currentVer)
      .slice(0, 10)
  }

  return info
})

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

// ---------- 缓存管理 ----------
interface CacheInfo {
  localStorageBytes: number
  indexedDBBytes: number
  tsMemoryBytes: number
  tsMemoryEntries: number
  tsDiskBytes: number
  tsDiskEntries: number
}

const cacheInfo = ref<CacheInfo | null>(null)
const cacheLoading = ref(false)
const cacheClearing = ref('')

function fmtBytes(bytes: number): string {
  if (bytes <= 0) return '0 B'
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB'
  if (bytes < 1024 * 1024 * 1024) return (bytes / (1024 * 1024)).toFixed(1) + ' MB'
  return (bytes / (1024 * 1024 * 1024)).toFixed(2) + ' GB'
}

async function loadCacheInfo(): Promise<void> {
  cacheLoading.value = true
  try {
    // 1. localStorage
    let lsBytes = 0
    for (let i = 0; i < localStorage.length; i++) {
      const key = localStorage.key(i)
      if (key) {
        lsBytes += (key.length + (localStorage.getItem(key)?.length || 0)) * 2
      }
    }

    // 2. TsCache 内存
    const tsMem = tsStats()
    const tsMemoryBytes = tsMem.bytes || 0
    const tsMemoryEntries = tsMem.totalEntries || 0

    // 3. TsCache 磁盘 (IndexedDB)
    let tsDiskBytes = 0
    let tsDiskEntries = 0
    try {
      const disk = await diskCacheInfo()
      tsDiskBytes = disk.bytes || 0
      tsDiskEntries = disk.count || 0
    } catch { /* TsCache 未初始化 */ }

    // 4. IndexedDB 总量 (包含 TsCache 磁盘 + 其他)
    let idxDBBytes = tsDiskBytes
    try {
      const est = await (navigator as any).storage?.estimate()
      if (est && est.usage) {
        idxDBBytes = est.usage
      }
    } catch { /* ignore */ }

    cacheInfo.value = {
      localStorageBytes: lsBytes,
      indexedDBBytes: idxDBBytes,
      tsMemoryBytes,
      tsMemoryEntries,
      tsDiskBytes,
      tsDiskEntries,
    }
  } catch {
    cacheInfo.value = null
  } finally {
    cacheLoading.value = false
  }
}

async function doClearCache(type: string, label: string): Promise<void> {
  const yes = await confirmStore.confirm({
    title: '清除缓存',
    message: `确定要清除${label}吗？\n\n⚠ 注意：清除后相关数据将无法恢复。${type === 'ts_memory' ? '\n清除内存缓存不会影响已下载的磁盘缓存。' : ''}`,
    okText: '确认清除',
    level: 'warn',
  })
  if (!yes) return
  cacheClearing.value = type
  try {
    if (type === 'ts_memory') {
      tsClear()
    } else if (type === 'ts_disk') {
      await TsCache.diskClear()
    }
    await loadCacheInfo()
  } catch (e: any) {
    errorStore.fromError('清除失败', e, 'Settings.clearCache')
  } finally {
    cacheClearing.value = ''
  }
}

// ---------- 启动 ----------
onMounted(async () => {
  const hash = (route.hash || '').replace('#', '').trim()
  const validIds = GROUPS.value.map(g => g.id)
  if (hash && validIds.includes(hash)) {
    activeGroup.value = hash
  }

  try { if (!themeStore.loaded) await themeStore.load() } catch { /* 忽略 */ }
  try { await downloadStore.init() } catch { /* 忽略 */ }

  // 加载版本号
  try { const v = await GetAppVersion(); if (v) appVersion.value = v } catch { /* ignore */ }

  const col = await safeGet('grid_columns', '5')
  gridColumns.value = parseInt(col, 10) || 5
  const den = await safeGet('layout_density', 'comfortable')
  layoutDensity.value = (den === 'compact' || den === 'spacious') ? den as any : 'comfortable'
  playbackAutoPlay.value = (await safeGet('playback_auto_play', '1')) !== '0'
  playbackAutoNext.value = (await safeGet('playback_auto_next', '1')) !== '0'
  playbackSpeed.value = parseFloat(await safeGet('playback_speed', '1')) || 1

  // 加载窗口设置
  await loadWindowResizable()
  await loadWindowSize()
  const wsk = await safeGet('window_size_key', 'md')
  if (windowSizePresets.value.find(p => p.key === wsk)) windowSizeKey.value = wsk

  // 加载字体设置
  const fsk = await safeGet('font_size_key', 'md')
  if (fontSizePresets.value.find(p => p.key === fsk)) fontSizeKey.value = fsk
  applyFontPreset(fontSizeKey.value)
  const ff = await safeGet('font_family', '')
  fontFamily.value = ff
  applyFontFamily()
  const lang = await safeGet('language', 'zh-CN')
  language.value = lang || 'zh-CN'

  // 加载关闭行为设置
  await loadCloseBehavior()

  // 加载更新设置
  tryAutoUpdate.value = localStorage.getItem('update__try_auto_update') !== '0'
  showChangeLog.value = localStorage.getItem('update__show_change_log') !== '0'

  // 监听更新下载进度事件
  const { Events } = await import('@wailsio/runtime')
  Events.On('update:download:progress', onUpdateDownloadProgress)
  Events.On('update:available', onUpdateAvailable)
  Events.On('update:version:changed', onVersionChanged)

  // 检查是否有待处理的启动更新（从 App.vue 跳转过来时）
  if (activeGroup.value === 'about') {
    try {
      const pending = await GetPendingUpdateInfo()
      if (pending && pending.has_update) {
        updateInfo.value = pending
        updateModalOpen.value = true
      }
    } catch { /* ignore */ }
  }
})

onUnmounted(() => {
  import('@wailsio/runtime').then(({ Events }) => {
    Events.Off('update:download:progress')
    Events.Off('update:available')
    Events.Off('update:version:changed')
  }).catch(() => {})
})

// 持久化更新设置
watch(tryAutoUpdate, (v) => { localStorage.setItem('update__try_auto_update', v ? '1' : '0') })
watch(showChangeLog, (v) => { localStorage.setItem('update__show_change_log', v ? '1' : '0') })

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
    <div class="tabs cczj-flex">
      <button
        v-for="g in GROUPS"
        :key="g.id"
        class="tab cczj-inline-flex cczj-items-center cczj-gap-4 cczj-cursor-pointer"
        :class="{ active: activeGroup === g.id }"
        @click="activeGroup = g.id"
      >
        <Icon :name="g.icon" :size="14" />
        <span>{{ g.label }}</span>
      </button>
    </div>

    <div class="content">
      <!-- ========== 基本设置 ========== -->
      <div v-if="activeGroup === 'basic'" class="panel cczj-flex cczj-flex-col cczj-gap-2">

        <!-- 窗口尺寸（预设单选） -->
        <section class="block">
          <h3>{{ t('settings.windowSize') }}</h3>
          <div class="radio-group cczj-flex cczj-flex-wrap">
            <label v-for="p in windowSizePresets" :key="p.key" class="radio-item cczj-inline-flex cczj-items-center cczj-gap-3 cczj-cursor-pointer"
              :class="{ checked: windowSizeKey === p.key }"
              @click="applyWindowPreset(p.key)"
            >
              <span class="radio-box cczj-inline-flex cczj-items-center cczj-justify-center">
                <Icon v-if="windowSizeKey === p.key" name="check" :size="12" />
              </span>
              <span class="radio-label">{{ p.label }}</span>
            </label>
          </div>
          <div class="row cczj-flex cczj-items-center cczj-gap-7" style="margin-top: 10px;">
            <label class="toggle cczj-inline-flex cczj-items-center cczj-gap-4 cczj-cursor-pointer">
              <input type="checkbox" v-model="windowResizable" @change="saveWindowResizable" />
              <span>{{ t('settings.allowResize') }}</span>
            </label>
          </div>
        </section>

        <!-- 字体大小（预设单选） -->
        <section class="block">
          <h3>{{ t('settings.fontSize') }}</h3>
          <div class="radio-group cczj-flex cczj-flex-wrap">
            <label v-for="p in fontSizePresets" :key="p.key" class="radio-item cczj-inline-flex cczj-items-center cczj-gap-3 cczj-cursor-pointer"
              :class="{ checked: fontSizeKey === p.key }"
              @click="applyFontPreset(p.key)"
            >
              <span class="radio-box cczj-inline-flex cczj-items-center cczj-justify-center">
                <Icon v-if="fontSizeKey === p.key" name="check" :size="12" />
              </span>
              <span class="radio-label">{{ p.label }}</span>
            </label>
          </div>
        </section>

        <!-- 字体（下拉框） -->
        <section class="block">
          <h3>{{ t('settings.fontFamily') }}</h3>
          <div class="row cczj-flex cczj-items-center cczj-gap-7">
            <SelectDropdown
              :model-value="fontFamily"
              :options="fontFamilyOptions"
              @update:model-value="(v: any) => { fontFamily = v; applyFontFamily(); save('font_family', v) }"
            />
          </div>
        </section>

        <!-- 语言（单选） -->
        <section class="block">
          <h3>{{ t('settings.language') }}</h3>
          <div class="radio-group cczj-flex cczj-flex-wrap">
            <label v-for="opt in languageOptions" :key="opt.value" class="radio-item cczj-inline-flex cczj-items-center cczj-gap-3 cczj-cursor-pointer"
              :class="{ checked: language === opt.value }"
              @click="language = opt.value; saveLocalePreference(opt.value); save('language', opt.value)"
            >
              <span class="radio-box cczj-inline-flex cczj-items-center cczj-justify-center">
                <Icon v-if="language === opt.value" name="check" :size="12" />
              </span>
              <span class="radio-label">{{ opt.label }}</span>
            </label>
          </div>
        </section>

        <!-- 首页网格列数 -->
        <section class="block">
          <h3>{{ t('settings.gridColumns') }}</h3>
          <div class="row cczj-flex cczj-items-center cczj-gap-7">
            <input type="range" v-model.number="gridColumns" min="2" max="10" @change="save('grid_columns', gridColumns)" />
            <span class="value">{{ gridColumns }} 列</span>
          </div>
        </section>

        <!-- 卡片密度 -->
        <section class="block">
          <h3>{{ t('settings.cardDensity') }}</h3>
          <div class="row cczj-flex cczj-items-center cczj-gap-7">
            <Segment
              :model-value="layoutDensity"
              :options="[{ value: 'comfortable', label: t('settings.comfortable') }, { value: 'compact', label: t('settings.compact') }, { value: 'spacious', label: t('settings.spacious') }]"
              @update:model-value="(v: any) => { layoutDensity = v as 'comfortable'|'compact'|'spacious'; save('layout_density', String(v)) }"
            />
          </div>
        </section>

        <!-- 关闭行为 -->
        <section class="block">
          <h3>{{ t('settings.closeBehavior') }}</h3>
          <div class="row cczj-flex cczj-items-center cczj-gap-7">
            <label class="toggle cczj-inline-flex cczj-items-center cczj-gap-4 cczj-cursor-pointer">
              <input type="checkbox" v-model="closeToTray" @change="saveCloseBehavior" />
              <span>{{ t('settings.minimizeToTray') }}</span>
            </label>
          </div>
        </section>

        <!-- 开发者模式开关（密码解锁后可见） -->
        <section v-if="devMode.unlocked" class="block block-dev">
          <h3>开发者模式</h3>
          <div class="row cczj-flex cczj-items-center cczj-gap-7">
            <label class="toggle cczj-inline-flex cczj-items-center cczj-gap-4 cczj-cursor-pointer">
              <input type="checkbox" :checked="devMode.enabled" @change="(e: any) => devMode.setEnabled(e.target.checked)" />
              <span>打开开发者模式</span>
            </label>
          </div>
          <small class="desc">开启后在侧边栏显示"开发者模式"栏目，提供后台管理功能</small>
        </section>

      </div>

      <!-- ========== 主题外观 ========== -->
      <div v-else-if="activeGroup === 'theme'" class="panel cczj-flex cczj-flex-col cczj-gap-2">
        <section class="block">
          <h3>主题颜色</h3>

          <h4 class="sub-title">浅色主题</h4>
          <div class="theme-grid cczj-grid">
            <!-- 预设 -->
            <button
              v-for="t in lightPresets"
              :key="t.id"
              class="theme-card cczj-flex cczj-flex-col cczj-items-center cczj-gap-5 cczj-cursor-pointer"
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
              <span class="card-actions-top cczj-absolute cczj-flex cczj-gap-2" @click.stop>
                <button class="mini-btn cczj-inline-flex cczj-items-center cczj-justify-center" @click="openEditPreset(t)" title="编辑主题">
                  <Icon name="pencil" :size="12" />
                </button>
              </span>
              <span v-if="!resolvePreset(t).bg" class="swatch" :style="{ background: resolvePreset(t).data.primary }"></span>
              <span v-if="resolvePreset(t).bg" class="swatch small" :style="{ background: resolvePreset(t).data.primary }"></span>
              <span class="label cczj-truncate" :style="{ color: resolvePreset(t).textPrimary }">{{ resolvePreset(t).data.name }}</span>
              <span v-if="isActive(t.id)" class="check cczj-inline-flex cczj-items-center cczj-justify-center"><Icon name="check" :size="12" /></span>
            </button>

            <!-- 自定义（排除与预设同名的覆盖项，那些已通过上方预设卡显示） -->
            <button
              v-for="c in pureCustomThemes.filter((c) => !c.dark)"
              :key="c.id"
              class="theme-card custom cczj-flex cczj-flex-col cczj-items-center cczj-gap-5 cczj-cursor-pointer"
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
              <span class="card-actions-top cczj-absolute cczj-flex cczj-gap-2" @click.stop>
                <button class="mini-btn cczj-inline-flex cczj-items-center cczj-justify-center" @click="openEdit(c)" title="编辑主题">
                  <Icon name="pencil" :size="12" />
                </button>
              </span>
              <span v-if="!c.backgroundImage" class="swatch" :style="{ background: c.primary }"></span>
              <span v-if="c.backgroundImage" class="swatch small" :style="{ background: c.primary }"></span>
              <span class="label cczj-truncate" :style="{ color: c.text }">{{ c.name }}</span>
              <span class="card-actions cczj-absolute cczj-flex cczj-gap-2" @click.stop>
                <button class="mini-btn danger" @click="onDeleteTheme(c.id, c.name)" title="删除">
                  <Icon name="x" :size="12" />
                </button>
              </span>
            </button>

            <!-- 添加（浅色区） -->
            <button class="theme-card add cczj-flex cczj-flex-col cczj-items-center cczj-gap-5 cczj-cursor-pointer" @click="openCreate(); applyPreview()">
              <span class="swatch plus"><Icon name="plus" :size="22" /></span>
              <span class="label cczj-truncate">添加主题</span>
            </button>
          </div>

          <h4 class="sub-title">深色主题</h4>
          <div class="theme-grid cczj-grid">
            <button
              v-for="t in darkPresets"
              :key="t.id"
              class="theme-card cczj-flex cczj-flex-col cczj-items-center cczj-gap-5 cczj-cursor-pointer"
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
              <span class="card-actions-top cczj-absolute cczj-flex cczj-gap-2" @click.stop>
                <button class="mini-btn cczj-inline-flex cczj-items-center cczj-justify-center" @click="openEditPreset(t)" title="编辑主题">
                  <Icon name="pencil" :size="12" />
                </button>
              </span>
              <span v-if="!resolvePreset(t).bg" class="swatch" :style="{ background: resolvePreset(t).data.primary }"></span>
              <span v-if="resolvePreset(t).bg" class="swatch small" :style="{ background: resolvePreset(t).data.primary }"></span>
              <span class="label cczj-truncate" :style="{ color: resolvePreset(t).textPrimary }">{{ resolvePreset(t).data.name }}</span>
              <span v-if="isActive(t.id)" class="check cczj-inline-flex cczj-items-center cczj-justify-center"><Icon name="check" :size="12" /></span>
            </button>

            <button
              v-for="c in pureCustomThemes.filter((c) => c.dark)"
              :key="c.id"
              class="theme-card custom cczj-flex cczj-flex-col cczj-items-center cczj-gap-5 cczj-cursor-pointer"
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
              <span class="card-actions-top cczj-absolute cczj-flex cczj-gap-2" @click.stop>
                <button class="mini-btn cczj-inline-flex cczj-items-center cczj-justify-center" @click="openEdit(c)" title="编辑主题">
                  <Icon name="pencil" :size="12" />
                </button>
              </span>
              <span v-if="!c.backgroundImage" class="swatch" :style="{ background: c.primary }"></span>
              <span v-if="c.backgroundImage" class="swatch small" :style="{ background: c.primary }"></span>
              <span class="label cczj-truncate" :style="{ color: c.text }">{{ c.name }}</span>
              <span class="card-actions cczj-absolute cczj-flex cczj-gap-2" @click.stop>
                <button class="mini-btn danger" @click="onDeleteTheme(c.id, c.name)" title="删除">
                  <Icon name="x" :size="12" />
                </button>
              </span>
            </button>
          </div>
        </section>
      </div>

      <!-- ========== 播放设置 ========== -->
      <div v-else-if="activeGroup === 'play'" class="panel cczj-flex cczj-flex-col cczj-gap-2">
        <section class="block">
          <h3>播放行为</h3>
          <div class="row toggles cczj-flex cczj-items-center cczj-gap-7">
            <label class="toggle cczj-inline-flex cczj-items-center cczj-gap-4 cczj-cursor-pointer">
              <input type="checkbox" v-model="playbackAutoPlay" @change="save('playback_auto_play', playbackAutoPlay?'1':'0')" />
              <span>自动开始播放</span>
            </label>
            <label class="toggle cczj-inline-flex cczj-items-center cczj-gap-4 cczj-cursor-pointer">
              <input type="checkbox" v-model="playbackAutoNext" @change="save('playback_auto_next', playbackAutoNext?'1':'0')" />
              <span>播放完自动下一集</span>
            </label>
          </div>
        </section>

        <section class="block">
          <h3>默认播放速度</h3>
          <div class="row cczj-flex cczj-items-center cczj-gap-7">
            <Segment
              :model-value="playbackSpeed"
              :options="speedOptions"
              @update:model-value="(v: any) => { playbackSpeed = Number(v); save('playback_speed', String(v)) }"
            />
          </div>
        </section>
      </div>

      <!-- ========== 缓存管理 ========== -->
      <div v-else-if="activeGroup === 'cache'" class="panel cczj-flex cczj-flex-col cczj-gap-2">
        <section class="block">
          <div class="block-hd cczj-flex cczj-items-center cczj-justify-between">
            <h3>缓存占用</h3>
            <Button variant="secondary" size="sm" :loading="cacheLoading" @click="loadCacheInfo">
              <Icon name="refresh" :size="12" /> 刷新
            </Button>
          </div>

          <div v-if="cacheLoading" class="cache-loading cczj-flex cczj-items-center cczj-gap-4">
            <Icon name="spinner" :size="18" /> 正在统计...
          </div>
          <div v-else-if="cacheInfo" class="cache-grid cczj-flex cczj-flex-col cczj-gap-6">
            <div class="cache-item cczj-flex cczj-items-center cczj-gap-7">
              <div class="cache-item-icon cczj-flex-shrink-0"><Icon name="cpu" :size="18" /></div>
              <div class="cache-item-info cczj-flex-1 cczj-min-w-0">
                <div class="cache-item-label">TS 内存缓存</div>
                <div class="cache-item-size">{{ fmtBytes(cacheInfo.tsMemoryBytes) }}</div>
                <div class="cache-item-path">{{ cacheInfo.tsMemoryEntries }} 个片段 · 当前播放集</div>
              </div>
              <Button
                variant="danger"
                size="sm"
                :disabled="cacheInfo.tsMemoryBytes <= 0"
                :loading="cacheClearing === 'ts_memory'"
                @click="doClearCache('ts_memory', 'TS 内存缓存')"
              >
                清除
              </Button>
            </div>

            <div class="cache-item cczj-flex cczj-items-center cczj-gap-7">
              <div class="cache-item-icon cczj-flex-shrink-0"><Icon name="download" :size="18" /></div>
              <div class="cache-item-info cczj-flex-1 cczj-min-w-0">
                <div class="cache-item-label">TS 磁盘缓存 (IndexedDB)</div>
                <div class="cache-item-size">{{ fmtBytes(cacheInfo.tsDiskBytes) }}</div>
                <div class="cache-item-path">{{ cacheInfo.tsDiskEntries }} 个片段 · 跨会话持久化</div>
              </div>
              <Button
                variant="danger"
                size="sm"
                :disabled="cacheInfo.tsDiskBytes <= 0"
                :loading="cacheClearing === 'ts_disk'"
                @click="doClearCache('ts_disk', 'TS 磁盘缓存')"
              >
                清除
              </Button>
            </div>

            <div class="cache-item cczj-flex cczj-items-center cczj-gap-7">
              <div class="cache-item-icon cczj-flex-shrink-0"><Icon name="database" :size="18" /></div>
              <div class="cache-item-info cczj-flex-1 cczj-min-w-0">
                <div class="cache-item-label">IndexedDB 总计</div>
                <div class="cache-item-size">{{ fmtBytes(cacheInfo.indexedDBBytes) }}</div>
                <div class="cache-item-path">浏览器分配的 IndexedDB 总占用空间</div>
              </div>
              <div class="cache-item-note cczj-flex-shrink-0">由浏览器自动管理</div>
            </div>

            <div class="cache-item cczj-flex cczj-items-center cczj-gap-7">
              <div class="cache-item-icon cczj-flex-shrink-0"><Icon name="browser" :size="18" /></div>
              <div class="cache-item-info cczj-flex-1 cczj-min-w-0">
                <div class="cache-item-label">浏览器存储 (localStorage)</div>
                <div class="cache-item-size">{{ fmtBytes(cacheInfo.localStorageBytes) }}</div>
                <div class="cache-item-path">主题、偏好设置、收藏夹映射等</div>
              </div>
              <div class="cache-item-note cczj-flex-shrink-0">⚠ 仅建议开发者手动清理</div>
            </div>
          </div>
          <div v-else class="cache-loading cczj-flex cczj-items-center cczj-gap-4">
            <span>点击「刷新」按钮查看缓存占用</span>
          </div>
        </section>

        <section class="block">
          <h3>说明</h3>
          <div class="cache-desc">
            <p><strong>TS 内存缓存：</strong>视频播放时缓存在内存中的 TS 片段，切换视频或关闭页面后自动释放。</p>
            <p><strong>TS 磁盘缓存：</strong>存储在浏览器 IndexedDB 中的 TS 片段，用于跨会话加速播放。</p>
            <p><strong>数据库文件：</strong>由后端管理，包含视频数据、收藏、历史记录等，位于应用数据目录。</p>
          </div>
        </section>
      </div>

      <!-- ========== 关于 ========== -->
      <div v-else-if="activeGroup === 'about'" class="panel cczj-flex cczj-flex-col cczj-gap-2">
        <section class="block">
          <div class="about-card cczj-flex cczj-items-center cczj-gap-8">
            <div class="about-icon cczj-inline-flex cczj-items-center cczj-justify-center"><Icon name="film" :size="22" /></div>
            <div>
              <h3 class="app-name-clickable" @click="devMode.clickAppName" title="点击 3 次以激活开发者模式">CCZJ Video</h3>
              <p>版本 <strong>{{ appVersion }}</strong> · Wails + Vue 3</p>
              <small>当前生效主题：<em>{{ themeStore.current.name }}</em>（{{ themeStore.current.mode === 'dark' ? '深色' : '浅色' }}）</small>
            </div>
          </div>

          <div class="about-actions cczj-flex cczj-gap-5">
            <Button
              variant="primary"
              size="md"
              :loading="updateChecking"
              @click="doCheckUpdate"
            >
              <Icon name="refresh" :size="14" /> 检查更新
            </Button>
            <Button
              variant="secondary"
              size="md"
              :disabled="restarting"
              :loading="restarting"
              @click="restartApp"
            >
              <Icon name="refresh" :size="14" /> 重启应用
            </Button>
          </div>
          <div class="about-auto-update cczj-flex cczj-items-center cczj-gap-4">
            <label class="cczj-flex cczj-items-center cczj-gap-3" style="cursor: pointer">
              <input type="checkbox" v-model="tryAutoUpdate" />
              <span class="auto-update-label">发现新版本时自动下载</span>
            </label>
            <label class="cczj-flex cczj-items-center cczj-gap-3" style="cursor: pointer">
              <input type="checkbox" v-model="showChangeLog" />
              <span class="auto-update-label">版本变化时显示更新日志</span>
            </label>
          </div>
        </section>

        <section class="block">
          <h3>使用声明</h3>
          <div class="disclaimer-card cczj-flex cczj-gap-7">
            <div class="disclaimer-icon cczj-inline-flex cczj-items-center cczj-justify-center"><Icon name="shield" :size="20" /></div>
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
      <div v-if="devMode.showPasswordModal" class="dev-password-overlay cczj-fixed cczj-inset-0 cczj-flex cczj-items-center cczj-justify-center" @click.self="devMode.closePasswordModal">
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
          <div class="dev-password-actions cczj-flex cczj-justify-center cczj-gap-6">
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
        <div class="edit-row cczj-flex cczj-flex-wrap">
          <div class="field cczj-flex cczj-flex-col cczj-gap-2">
            <label>主题名称</label>
            <input type="text" v-model="editing.name" placeholder="我的主题" @input="applyPreview" />
          </div>
          <div class="flags cczj-flex cczj-items-center cczj-flex-wrap">
            <label class="toggle cczj-inline-flex cczj-items-center cczj-gap-4 cczj-cursor-pointer">
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
            <div class="picker-cell cczj-flex cczj-items-center cczj-gap-4"><input type="color" v-model="editing.primary" @input="applyPreview" /><span>{{ editing.primary }}</span></div>
          </div>
          <div class="picker-item">
            <label>字体颜色</label>
            <div class="picker-cell cczj-flex cczj-items-center cczj-gap-4"><input type="color" v-model="editing.text" @input="applyPreview" /><span>{{ editing.text }}</span></div>
          </div>
          <div class="picker-item">
            <label>应用背景</label>
            <div class="picker-cell cczj-flex cczj-items-center cczj-gap-4"><input type="color" v-model="editing.background" @input="applyPreview" /><span>{{ editing.background }}</span></div>
          </div>
          <div class="picker-item">
            <label>侧边栏背景</label>
            <div class="picker-cell cczj-flex cczj-items-center cczj-gap-4"><input type="color" v-model="editing.sidebar" @input="applyPreview" /><span>{{ editing.sidebar }}</span></div>
          </div>
          <div class="picker-item">
            <label>内容区域背景</label>
            <div class="picker-cell cczj-flex cczj-items-center cczj-gap-4"><input type="color" v-model="editing.content" @input="applyPreview" /><span>{{ editing.content }}</span></div>
          </div>
        </div>

        <h4 class="group-title">背景透明度</h4>
        <div class="picker-grid">
          <div class="picker-item">
            <label>侧边栏透明度</label>
            <div class="picker-cell range-cell cczj-flex cczj-items-center cczj-gap-4">
              <input type="range" v-model.number="editing.sidebarAlpha" min="0" max="1" step="0.05" @input="applyPreview" />
              <span>{{ Math.round((editing.sidebarAlpha ?? 0.65) * 100) }}%</span>
            </div>
          </div>
          <div class="picker-item">
            <label>卡片透明度</label>
            <div class="picker-cell range-cell cczj-flex cczj-items-center cczj-gap-4">
              <input type="range" v-model.number="editing.contentAlpha" min="0" max="1" step="0.05" @input="applyPreview" />
              <span>{{ Math.round((editing.contentAlpha ?? 0.88) * 100) }}%</span>
            </div>
          </div>
        </div>

        <h4 class="group-title">窗口控制按钮颜色</h4>
        <div class="picker-grid small">
          <div class="picker-item">
            <label>关闭按钮</label>
            <div class="picker-cell cczj-flex cczj-items-center cczj-gap-4"><input type="color" v-model="editing.btnClose" @input="applyPreview" /><span>{{ editing.btnClose }}</span></div>
          </div>
          <div class="picker-item">
            <label>最小化按钮</label>
            <div class="picker-cell cczj-flex cczj-items-center cczj-gap-4"><input type="color" v-model="editing.btnMin" @input="applyPreview" /><span>{{ editing.btnMin }}</span></div>
          </div>
          <div class="picker-item">
            <label>隐藏按钮</label>
            <div class="picker-cell cczj-flex cczj-items-center cczj-gap-4"><input type="color" v-model="editing.btnHide" @input="applyPreview" /><span>{{ editing.btnHide }}</span></div>
          </div>
        </div>

        <div
          class="bg-drop-zone cczj-relative cczj-flex cczj-items-center cczj-justify-center cczj-overflow-hidden cczj-cursor-pointer"
          :class="{ 'has-image': !!backgroundImageUrl }"
          @click="onDropZoneClick"
          @dragover.prevent="onDragOver"
          @dragleave="onDragLeave"
          @drop.prevent="onDrop"
        >
          <button
            v-if="backgroundImageUrl"
            class="bg-remove cczj-absolute cczj-flex cczj-items-center cczj-justify-center"
            title="移除图片"
            @click.stop="clearBackgroundImage"
          >
            <Icon name="x" :size="14" />
          </button>
          <img v-if="backgroundImageUrl" :src="backgroundImageUrl" alt="背景预览" />
          <div v-else class="bg-drop-hint cczj-flex cczj-flex-col cczj-items-center cczj-gap-5">
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

    <!-- ========== 版本更新弹窗 ========== -->
    <Modal
      :model-value="updateModalOpen"
      :title="updateCheckFailed ? '获取更新信息失败' : (updateModalDownloaded ? '下载完成' : '发现新版本')"
      width="min(560px, 94vw)"
      :show-footer="true"
      :closable="!updateDownloading"
      @update:model-value="(v: boolean) => { if (!v && !updateDownloading) { updateModalOpen = false; updateModalDownloaded = false; updateCheckFailed = false; ClearPendingUpdateInfo() } }"
    >
      <div class="modal-body">
        <!-- 检查更新失败界面（参考 lx-music-desktop 的 isUnknown 状态） -->
        <template v-if="updateCheckFailed">
          <div class="update-done-card cczj-flex cczj-flex-col cczj-items-center cczj-gap-8">
            <div class="update-fail-icon cczj-inline-flex cczj-items-center cczj-justify-center">
              <Icon name="alert-circle" :size="28" />
            </div>
            <div class="update-done-text">
              <p><strong>获取最新版本信息失败</strong></p>
              <p>可能是无法访问 GitHub 导致的，请尝试手动检查更新。</p>
            </div>
            <div class="update-fail-hint">
              <p>检查方法：打开 <a href="https://github.com/ws-cczj/CCZJ-Video/releases" target="_blank" class="update-link">软件发布页</a>，查看「Latest」发布的版本号与当前版本 (<strong>{{ appVersion }}</strong>) 对比是否一致。</p>
              <p>若一致则不必理会，直接关闭即可；否则请手动下载新版本更新。</p>
            </div>
          </div>
        </template>

        <!-- 下载完成界面 -->
        <template v-else-if="updateModalDownloaded">
          <div class="update-done-card cczj-flex cczj-flex-col cczj-items-center cczj-gap-8">
            <div class="update-done-icon cczj-inline-flex cczj-items-center cczj-justify-center">
              <Icon name="check" :size="28" />
            </div>
            <div class="update-done-text">
              <p><strong>更新包已下载完成</strong></p>
              <p class="update-done-path">{{ updateFilePath }}</p>
            </div>
            <div class="update-done-hint">
              <p>点击「立即更新」将关闭当前程序并启动新版本安装程序。</p>
              <p>也可以点击「下次启动」，下次启动应用时再安装更新。</p>
            </div>
          </div>
        </template>

        <!-- 更新信息界面 -->
        <template v-else-if="updateInfo">
          <div class="update-header cczj-flex cczj-items-center cczj-gap-8">
            <div class="update-icon cczj-inline-flex cczj-items-center cczj-justify-center">
              <Icon name="download" :size="22" />
            </div>
            <div>
              <h3 class="update-title">{{ updateInfo.release_name || `v${updateInfo.latest_version}` }}</h3>
              <p class="update-versions">
                <span class="update-current">{{ updateInfo.current_version }}</span>
                <span class="update-arrow">&rarr;</span>
                <span class="update-latest">{{ updateInfo.latest_version }}</span>
              </p>
            </div>
          </div>

          <!-- 更新内容 -->
          <div v-if="updateInfo.release_notes" class="update-notes">
            <h4>更新内容</h4>
            <div class="update-notes-body">{{ updateInfo.release_notes }}</div>
          </div>

          <!-- 历史版本（参考 lx-music-desktop 的 history 展示） -->
          <div v-if="updateInfo.history && updateInfo.history.length > 0" class="update-history">
            <h4>历史版本</h4>
            <div v-for="(ver, index) in updateInfo.history" :key="index" class="update-history-item">
              <h5>v{{ ver.version }}</h5>
              <pre>{{ ver.desc }}</pre>
            </div>
          </div>

          <!-- 下载进度 -->
          <div v-if="updateDownloading" class="update-progress">
            <div class="progress-track">
              <div class="progress-fill" :style="{ width: updateDownloadProgress.percent + '%' }"></div>
            </div>
            <div class="progress-meta cczj-flex cczj-justify-between">
              <span>{{ fmtSize(updateDownloadProgress.downloaded) }} / {{ fmtSize(updateDownloadProgress.total) }}</span>
              <span>{{ fmtSpeed(updateDownloadProgress.speed_bps) }}</span>
            </div>
          </div>

          <!-- 文件信息 -->
          <div v-if="updateInfo.asset_name" class="update-file-info">
            <span class="update-file-name">{{ updateInfo.asset_name }}</span>
            <span class="update-file-size">{{ fmtSize(updateInfo.asset_size) }}</span>
          </div>
        </template>
      </div>

      <template #footer>
        <!-- 检查失败界面按钮 -->
        <template v-if="updateCheckFailed">
          <Button variant="secondary" size="md" :disabled="!canShowUpdateFailTip()" @click="dismissUpdateFailTip">
            一个星期内不再提醒
          </Button>
          <span style="flex: 1"></span>
          <Button variant="primary" size="md" :loading="updateChecking" @click="doCheckUpdate">
            <Icon name="refresh" :size="14" /> 重新检查更新
          </Button>
        </template>

        <!-- 下载完成界面按钮 -->
        <template v-else-if="updateModalDownloaded">
          <Button variant="secondary" size="md" @click="doIgnoreVersion">
            下次启动
          </Button>
          <span style="flex: 1"></span>
          <Button variant="primary" size="md" :loading="updateInstalling" @click="doInstallUpdate">
            <Icon name="zap" :size="14" /> 立即更新
          </Button>
        </template>

        <!-- 更新信息界面按钮 -->
        <template v-else>
          <Button variant="secondary" size="md" @click="doIgnoreVersion">
            忽略此版本
          </Button>
          <span style="flex: 1"></span>
          <Button variant="primary" size="md" :disabled="updateDownloading" :loading="updateDownloading" @click="doDownloadUpdate">
            <Icon name="download" :size="14" /> {{ updateDownloading ? '下载中...' : '下载更新' }}
          </Button>
        </template>
      </template>
    </Modal>

    <!-- ========== 更新日志弹窗（参考 lx-music-desktop 的 ChangeLogModal） ========== -->
    <Modal
      :model-value="changelogModalOpen"
      title="当前版本更新日志"
      width="min(560px, 94vw)"
      :show-footer="true"
      @update:model-value="(v: boolean) => { if (!v) changelogModalOpen = false }"
    >
      <div class="modal-body">
        <div class="changelog-content">
          <div class="changelog-current">
            <h3>当前版本：{{ changelogInfo.version }}</h3>
            <template v-if="changelogInfo.desc">
              <h3>版本变化：</h3>
              <pre class="changelog-desc">{{ changelogInfo.desc }}</pre>
            </template>
          </div>
          <div v-if="changelogInfo.history.length > 0" class="changelog-history">
            <h3>历史版本：</h3>
            <div v-for="(ver, index) in changelogInfo.history" :key="index" class="changelog-history-item">
              <h4>v{{ ver.version }}</h4>
              <pre>{{ ver.desc }}</pre>
            </div>
          </div>
        </div>
        <div class="changelog-footer-note">
          <p>为了减少疑问，强烈建议阅读版本更新日志来了解当前所用版本的变化！</p>
          <p v-if="!changelogInfo.isLatest">发现新版本 (v{{ updateInfo?.latest_version }})！建议去「软件更新」更新新版本。</p>
        </div>
      </div>
      <template #footer>
        <span style="flex: 1"></span>
        <Button variant="primary" size="md" @click="changelogModalOpen = false">
          我知道了
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
  font-size: 1.43rem;
  font-weight: 700;
  margin: 0 0 4px;
}
.page-desc {
  font-size: 0.86rem;
  color: var(--text-muted);
  margin: 0;
}

/* ============ 顶部 tab ============ */
.tabs {
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
  gap: 8px;
  padding: 8px 14px;
  border: 1px solid transparent;
  background: transparent;
  color: var(--text-secondary);
  border-radius: 8px;
  font-size: 0.93rem;
  font-weight: 500;
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
  font-size: 1rem;
  font-weight: 700;
  margin: 0 0 14px;
  letter-spacing: 0.3px;
}

/* 单选按钮组 */
.radio-group {
  gap: 6px 10px;
}
.radio-item {
  gap: 6px;
  padding: 5px 12px;
  border-radius: 6px;
  font-size: 0.93rem;
  color: var(--text-secondary);
  transition: all 0.15s ease;
  user-select: none;
}
.radio-item:hover {
  background: var(--bg-secondary);
}
.radio-item.checked {
  color: var(--accent);
  font-weight: 600;
}
.radio-box {
  width: 16px;
  height: 16px;
  border: 2px solid var(--border);
  border-radius: 3px;
  transition: all 0.15s ease;
  flex-shrink: 0;
}
.radio-item.checked .radio-box {
  background: var(--accent);
  border-color: var(--accent);
  color: #fff;
}
.radio-label {
  white-space: nowrap;
}
.sub-title {
  font-size: 0.93rem;
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
  gap: 8px;
  font-size: 0.93rem;
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
  font-size: 0.93rem;
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
  grid-template-columns: repeat(auto-fill, minmax(128px, 1fr));
  gap: 14px;
  margin-bottom: 18px;
}
.theme-card {
  position: relative;
  gap: 10px;
  padding: 14px 10px 14px;
  min-height: 160px;
  background: var(--bg-secondary);
  border: 2px solid transparent;
  border-radius: 12px;
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
  font-size: 0.86rem;
  color: var(--text-secondary);
  font-weight: 700;
  max-width: 100%;
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
  font-size: 0.86rem;
  box-shadow: 0 2px 6px rgba(0, 0, 0, 0.25);
  z-index: 2;
}

/* 主题卡片上的编辑按钮（右上角，hover 显示） */
.card-actions-top {
  top: 6px;
  right: 6px;
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
  bottom: 6px;
  right: 6px;
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
  gap: 6px;
}
.source-item {
  gap: 10px;
  padding: 10px 14px;
  background: var(--bg-secondary);
  border: 1px solid var(--border);
  border-radius: 8px;
  font-size: 0.93rem;
}
.source-item .dot {
  width: 8px; height: 8px; border-radius: 50%;
}
.source-item .name { font-weight: 600; }
.source-item .muted {
  color: var(--text-muted);
  font-family: ui-monospace, Menlo, Monaco, Consolas, monospace;
  font-size: 0.79rem;
  margin-left: auto;
}
.empty {
  gap: 10px;
  padding: 32px;
  background: var(--bg-secondary);
  border: 1px dashed var(--border);
  border-radius: 10px;
  color: var(--text-muted);
  font-size: 0.93rem;
}
.about-card {
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
  box-shadow: 0 6px 18px var(--accent-alpha-35);
}
.about-card h3 { margin: 0 0 4px; font-size: 1.14rem; font-weight: 700; }
.about-card p { margin: 0 0 4px; font-size: 0.93rem; color: var(--text-secondary); }
.about-card small { color: var(--text-muted); font-size: 0.86rem; }
.about-actions {
  margin-top: 16px;
  gap: 10px;
  flex-wrap: wrap;
}

/* ============ 声明卡片 ============ */
.disclaimer-card {
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
  box-shadow: 0 4px 12px var(--warning-alpha-10);
}
.disclaimer-content {
  flex: 1;
  min-width: 0;
}
.disclaimer-content p {
  margin: 0 0 6px;
  font-size: 0.89rem;
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
.modal-head h3 { margin: 0; font-size: 1.07rem; font-weight: 700; }
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
  align-items: flex-end;
  gap: 20px;
  padding-bottom: 14px;
  margin-bottom: 10px;
  border-bottom: 1px dashed var(--border);
}
.field { gap: 4px; flex: 1; min-width: 180px; }
.field label { font-size: 0.79rem; color: var(--text-muted); font-weight: 600; }
.field input[type='text'] {
  padding: 8px 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--bg-secondary);
  color: var(--text-primary);
  font-size: 0.93rem;
  outline: none;
}
.field input[type='text']:focus { border-color: var(--accent); }
.flags { gap: 18px; }
.derive-btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 8px 14px;
  background: var(--accent);
  color: var(--accent-contrast);
  border: none;
  border-radius: 8px;
  font-size: 0.86rem;
  font-weight: 600;
  cursor: pointer;
  transition: all 0.15s ease;
}
.derive-btn:hover { background: var(--accent-dim); transform: translateY(-1px); }

.group-title {
  font-size: 0.79rem;
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
  font-size: 0.79rem;
  color: var(--text-muted);
  margin-bottom: 4px;
  font-weight: 600;
}
.picker-cell {
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
  font-size: 0.79rem;
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
  margin-top: 14px;
  border: 2px dashed var(--border);
  border-radius: 10px;
  min-height: 180px;
  background: var(--bg-card);
  transition: all 0.15s ease;
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
  gap: 10px;
  color: var(--text-secondary);
}
.bg-drop-hint span { font-size: 0.93rem; }
.bg-remove {
  top: 8px;
  right: 8px;
  width: 28px;
  height: 28px;
  border-radius: 50%;
  background: rgba(0,0,0,0.55);
  color: #fff;
  border: none;
  cursor: pointer;
  z-index: 2;
  transition: background 0.15s ease;
}
.bg-remove:hover { background: var(--danger); }

.modal-foot {
  gap: 10px;
  padding: 14px 20px;
  border-top: 1px solid var(--border);
  background: var(--bg-secondary);
}

/* 导入弹窗底部 - 与 modal-footer 与 modal-foot 统一 */
.modal-footer {
  gap: 10px;
  padding: 14px 20px;
  border-top: 1px solid var(--border);
  background: var(--bg-secondary);
  border-bottom-left-radius: 14px;
  border-bottom-right-radius: 14px;
}
.modal-footer .btn {
  padding: 9px 18px;
  font-size: 0.93rem;
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
  font-size: 0.93rem;
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
  font-size: 0.93rem;
  font-family: inherit;
  outline: none;
  transition: border-color 0.15s ease;
}
.setting-input:focus {
  border-color: var(--accent);
}
.block .hint {
  margin-top: 8px;
  font-size: 0.86rem;
  color: var(--text-muted);
  line-height: 1.5;
}

/* -------- 采集调度面板样式 -------- */
.desc {
  color: var(--text-muted);
  font-size: 0.93rem;
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
  font-size: 0.93rem;
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
  font-size: 0.93rem;
  font-variant-numeric: tabular-nums;
  text-align: right;
  outline: none;
  box-shadow: 0 0 0 3px var(--accent-alpha-20);
}
.bubble-input .unit { color: var(--text-muted); font-size: 0.93rem; }

.row-right .unit { color: var(--text-muted); font-size: 0.93rem; }
.row-right .hint { font-size: 0.86rem; margin-left: 4px; }
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
  font-size: 0.93rem;
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
  font-size: 1rem;
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
  font-size: 0.93rem;
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
  font-size: 0.86rem;
  color: var(--text-secondary);
  max-height: 220px;
  overflow-y: auto;
}
.log-box .log-line { line-height: 1.6; }

/* ---------- 日志 viewer ---------- */
.log-toolbar {
  gap: 10px;
  margin: 12px 0 8px 0;
}
.log-toolbar select {
  background: var(--bg-secondary);
  color: var(--text-primary);
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 6px 10px;
  font-size: 0.86rem;
  outline: none;
}
.log-toolbar .btn {
  background: var(--bg-secondary);
  color: var(--text-primary);
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 6px 12px;
  font-size: 0.86rem;
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

.log-dir { margin: 6px 0 10px 0; font-size: 0.79rem; color: var(--text-muted); }
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
  font-size: 0.79rem;
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
  font-size: 0.93rem;
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
  font-size: 1rem;
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
.detail-head .d-name { font-size: 1.14rem; font-weight: 700; color: var(--text-primary); }
.detail-head .d-key {
  font-size: 0.79rem; font-family: ui-monospace, Menlo, Monaco, Consolas, monospace;
  color: var(--text-muted);
  margin-top: 2px;
}
.detail-head .d-url {
  font-size: 0.79rem; color: var(--text-muted); word-break: break-all; margin-top: 2px;
}
.detail-head .d-actions {
  gap: 8px;
}

.source-toolbar {
  gap: 14px;
  margin: 10px 0 18px;
  padding: 14px 16px;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 12px;
}
.source-toolbar .btn {
  padding: 10px 18px;
  font-size: 0.93rem;
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
  font-size: 0.86rem;
  color: var(--text-muted);
}

/* 源详情头部 */
.detail-head .d-actions .btn {
  padding: 8px 14px;
  font-size: 0.86rem;
  border-radius: 8px;
}

.export-result {
  margin: 10px 0 16px;
  padding: 14px 16px;
  background: var(--bg-card);
  border: 1px dashed var(--accent);
  border-radius: 12px;
  font-size: 0.93rem;
  color: var(--text-primary);
}
.export-result .export-path {
  font-family: ui-monospace, Menlo, Monaco, Consolas, monospace;
  font-size: 0.86rem;
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
  font-size: 0.86rem;
  border-radius: 8px;
}
.export-result .hint {
  font-size: 0.86rem;
  color: var(--text-muted);
}

.file-picker {
  gap: 10px;
}
.file-picker .file-name {
  font-family: ui-monospace, Menlo, Monaco, Consolas, monospace;
  font-size: 0.86rem;
  color: var(--text-secondary);
  word-break: break-all;
}

.import-result {
  margin-top: 14px;
  padding: 10px 12px;
  background: var(--bg-secondary);
  border: 1px solid var(--border);
  border-radius: 10px;
  font-size: 0.93rem;
  color: var(--text-primary);
}

/* ========== 拖拽上传区域 ========== */
.drop-zone {
  margin: 16px 0;
  padding: 32px 16px;
  border: 2px dashed var(--border);
  border-radius: 14px;
  background: var(--bg-secondary);
  transition: all 0.2s ease;
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
  font-size: 1.14rem;
  font-weight: 600;
  color: var(--text-primary);
  margin: 0;
}

.drop-subtitle {
  font-size: 0.93rem;
  color: var(--text-muted);
  margin: 0;
}

.drop-format {
  font-size: 0.86rem;
  color: var(--text-muted);
  margin: 0;
  padding: 4px 10px;
  background: var(--bg-card);
  border-radius: 20px;
}

/* 已选择的文件 */
.selected-file {
  gap: 8px;
  padding: 10px 12px;
  background: var(--bg-card);
  border: 1px solid var(--accent);
  border-radius: 10px;
  margin-top: -8px;
  font-size: 0.93rem;
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
  font-size: 0.86rem;
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
  font-size: 0.79rem;
  font-weight: 600;
}
.t-head .t-count {
  margin-left: auto;
  color: var(--text-muted);
  font-size: 0.79rem;
}

.col-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 0.86rem;
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
  font-size: 0.79rem;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}
.col-table tr:last-child td { border-bottom: none; }

.samples { margin-bottom: 16px; }
.samples h4 {
  font-size: 0.86rem;
  margin: 6px 0 8px;
  color: var(--text-primary);
  font-weight: 700;
  letter-spacing: 0.3px;
}
.sample-list {
  display: flex; flex-direction: column; gap: 8px;
}
.sample-row {
  gap: 10px;
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
  font-size: 0.93rem; font-weight: 600; color: var(--text-primary);
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
  align-items: flex-start; gap: 10px;
  padding: 10px 12px;
  user-select: none;
  transition: background 0.15s;
}
.sample-head:hover { background: var(--bg-hover); }
.sample-head .chevron {
  font-size: 1.43rem; color: var(--text-muted);
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
  font-size: 0.93rem; font-weight: 600; color: var(--text-primary);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.sample-meta .small {
  font-size: 0.79rem;
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
  font-size: 0.86rem;
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
  font-size: 0.79rem; font-weight: normal; color: var(--text-muted);
  margin-left: 8px;
}

.mono { font-family: ui-monospace, Menlo, Monaco, Consolas, monospace; }
.small { font-size: 0.79rem; }
.break-all { word-break: break-all; }

/* ---------- 日志查看器新样式 ---------- */
.panel-header {
  margin-bottom: 4px;
}
.panel-header h3 {
  font-size: 1.07rem;
  font-weight: 700;
  margin: 0 0 4px;
}
.panel-header .hint {
  font-size: 0.86rem;
  color: var(--text-muted);
  margin: 0;
}

/* 日志过滤栏 */
.log-filter-bar {
  gap: 10px;
  margin: 10px 0 8px 0;
}
.log-search {
  flex: 1;
  min-width: 160px;
  padding: 7px 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--bg-secondary);
  color: var(--text-primary);
  font-size: 0.93rem;
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
  font-size: 0.86rem;
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
  font-size: 0.86rem;
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
  font-size: 0.93rem;
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
  font-size: 0.86rem;
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
  .tab { padding: 6px 10px; font-size: 0.86rem; }
}

/* ====== 开发者模式密码弹窗 ====== */
.dev-password-overlay {
  z-index: 10000;
  background: rgba(0, 0, 0, 0.6);
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
  font-size: 1.43rem;
  color: var(--accent);
}

.dev-password-desc {
  margin: 0 0 20px;
  font-size: 0.93rem;
  color: var(--text-muted);
}

.dev-password-input {
  width: 160px;
  padding: 10px 16px;
  font-size: 1.57rem;
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
  font-size: 0.93rem;
  font-weight: 500;
}

.dev-password-actions {
  gap: 12px;
  margin-top: 20px;
}

.dev-password-btn {
  padding: 8px 28px;
  border: none;
  border-radius: var(--radius);
  cursor: pointer;
  font-size: 1rem;
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
  font-size: 0.86rem;
  line-height: 1.5;
}

/* ============ 缓存管理 ============ */
.block-hd {
  margin-bottom: 16px;
}
.block-hd h3 {
  margin: 0;
  font-size: 15px;
  font-weight: 600;
}
.cache-loading {
  gap: 8px;
  padding: 20px 0;
  color: var(--text-muted);
  font-size: 13px;
}
.cache-grid {
  gap: 12px;
}
.cache-item {
  gap: 14px;
  padding: 14px 16px;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 10px;
}
.cache-item-icon {
  color: var(--accent);
}
.cache-item-info {
}
.cache-item-label {
  font-size: 13px;
  font-weight: 500;
  color: var(--text-primary);
}
.cache-item-size {
  font-size: 18px;
  font-weight: 700;
  color: var(--text-primary);
  margin: 2px 0;
}
.cache-item-path {
  font-size: 11px;
  color: var(--text-muted);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 320px;
}
.cache-item-note {
  font-size: 11px;
  color: var(--text-muted);
  white-space: nowrap;
}
.cache-desc {
  font-size: 13px;
  color: var(--text-muted);
  margin: 0 0 12px;
  line-height: 1.5;
}

/* ============ 版本更新弹窗 ============ */
.update-header {
  gap: 16px;
  padding: 8px 0 16px;
  border-bottom: 1px dashed var(--border);
  margin-bottom: 16px;
}
.update-icon {
  width: 52px;
  height: 52px;
  border-radius: 14px;
  background: var(--accent);
  color: var(--accent-contrast);
  box-shadow: 0 6px 18px var(--accent-alpha-35);
  flex-shrink: 0;
}
.update-title {
  font-size: 1.14rem;
  font-weight: 700;
  margin: 0 0 4px;
  color: var(--text-primary);
}
.update-versions {
  margin: 0;
  font-size: 0.93rem;
  display: flex;
  align-items: center;
  gap: 10px;
}
.update-current {
  color: var(--text-muted);
  font-family: ui-monospace, Menlo, Monaco, Consolas, monospace;
}
.update-arrow {
  color: var(--text-muted);
  font-size: 1.14rem;
}
.update-latest {
  color: var(--accent);
  font-weight: 700;
  font-family: ui-monospace, Menlo, Monaco, Consolas, monospace;
}

.update-notes {
  margin-bottom: 16px;
}
.update-notes h4 {
  font-size: 0.86rem;
  font-weight: 600;
  color: var(--text-secondary);
  margin: 0 0 8px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}
.update-notes-body {
  background: var(--bg-secondary);
  border: 1px solid var(--border);
  border-radius: 10px;
  padding: 12px 14px;
  font-size: 0.93rem;
  line-height: 1.6;
  color: var(--text-secondary);
  max-height: 200px;
  overflow-y: auto;
  white-space: pre-wrap;
  word-break: break-word;
}

.update-progress {
  margin-bottom: 16px;
}
.update-progress .progress-track {
  height: 6px;
  background: var(--bg-secondary);
  border: 1px solid var(--border);
  border-radius: 3px;
  overflow: hidden;
  margin-bottom: 8px;
}
.update-progress .progress-fill {
  height: 100%;
  background: var(--accent);
  border-radius: 3px;
  transition: width 0.3s ease;
}
.update-progress .progress-meta {
  font-size: 0.86rem;
  color: var(--text-muted);
}

.update-file-info {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 10px 14px;
  background: var(--bg-secondary);
  border: 1px solid var(--border);
  border-radius: 8px;
  font-size: 0.86rem;
}
.update-file-name {
  color: var(--text-primary);
  font-family: ui-monospace, Menlo, Monaco, Consolas, monospace;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  flex: 1;
  min-width: 0;
  margin-right: 12px;
}
.update-file-size {
  color: var(--text-muted);
  flex-shrink: 0;
}

/* 下载完成界面 */
.update-done-card {
  gap: 16px;
  padding: 16px 0;
  text-align: center;
}
.update-done-icon {
  width: 64px;
  height: 64px;
  border-radius: 50%;
  background: var(--success);
  color: #fff;
  box-shadow: 0 6px 18px var(--success-alpha-10);
}
.update-done-text p {
  margin: 0 0 4px;
  font-size: 1.07rem;
  color: var(--text-primary);
}
.update-done-path {
  font-size: 0.79rem !important;
  color: var(--text-muted) !important;
  font-family: ui-monospace, Menlo, Monaco, Consolas, monospace;
  word-break: break-all;
  margin-top: 6px !important;
}
.update-done-hint {
  font-size: 0.86rem;
  color: var(--text-muted);
  line-height: 1.6;
}
.update-done-hint p {
  margin: 0 0 4px;
}

/* 检查更新失败界面 */
.update-fail-icon {
  width: 64px;
  height: 64px;
  border-radius: 50%;
  background: var(--warning, #f59e0b);
  color: #fff;
  box-shadow: 0 6px 18px rgba(245, 158, 11, 0.15);
}
.update-fail-hint {
  font-size: 0.86rem;
  color: var(--text-muted);
  line-height: 1.6;
  text-align: left;
  padding: 0 8px;
}
.update-fail-hint p {
  margin: 0 0 4px;
}
.update-link {
  color: var(--accent);
  text-decoration: underline;
  cursor: pointer;
}
.update-link:hover {
  opacity: 0.8;
}

/* 历史版本 */
.update-history {
  margin-top: 16px;
  padding-top: 12px;
  border-top: 1px solid var(--border);
}
.update-history h4 {
  font-size: 0.91rem;
  font-weight: 600;
  color: var(--text-primary);
  margin: 0 0 10px;
}
.update-history-item {
  margin-bottom: 12px;
  padding: 10px 12px;
  background: var(--bg-secondary);
  border-radius: 8px;
}
.update-history-item h5 {
  font-size: 0.89rem;
  font-weight: 600;
  color: var(--text-primary);
  margin: 0 0 6px;
}
.update-history-item pre {
  font-size: 0.82rem;
  color: var(--text-secondary);
  white-space: pre-wrap;
  margin: 0;
  line-height: 1.5;
  font-family: inherit;
}

/* 自动更新开关 */
.about-auto-update {
  margin-top: 12px;
  padding: 8px 0;
}
.auto-update-label {
  font-size: 0.88rem;
  color: var(--text-secondary);
  user-select: none;
}

/* 更新日志弹窗 */
.changelog-content {
  max-height: 50vh;
  overflow-y: auto;
  font-size: 0.88rem;
  line-height: 1.6;
}
.changelog-content h3 {
  font-size: 0.93rem;
  font-weight: 600;
  padding: 8px 0 4px;
}
.changelog-content h4 {
  font-size: 0.88rem;
  font-weight: 600;
  padding: 4px 0;
}
.changelog-desc {
  white-space: pre-wrap;
  text-align: justify;
  margin-top: 8px;
  font-size: 0.85rem;
  color: var(--text-secondary);
  font-family: inherit;
  line-height: 1.5;
}
.changelog-history {
  margin-top: 16px;
  padding-top: 12px;
  border-top: 1px solid var(--border-color);
}
.changelog-history-item {
  padding: 8px 12px;
  margin-bottom: 8px;
}
.changelog-history-item pre {
  white-space: pre-wrap;
  font-size: 0.82rem;
  color: var(--text-secondary);
  margin: 0;
  line-height: 1.5;
  font-family: inherit;
}
.changelog-footer-note {
  margin-top: 16px;
  padding-top: 12px;
  border-top: 1px solid var(--border-color);
  font-size: 0.82rem;
  color: var(--text-muted);
  line-height: 1.5;
}
.changelog-footer-note p {
  margin: 4px 0;
}
</style>
