<script setup lang="ts">
/**
 * 「基本」分组：外观、窗口、首页、播放、数据与网络、维护六节，一行一项。
 *
 * 控件类型按桌面软件的惯例给：布尔用开关、两到六个枚举用下拉、数值用带单位的数字框，
 * 不再用一排单选格子铺满一行。每一项都要求后端真有对应行为——写的都是既有设置键或既有
 * 绑定方法，没有接线的选项一律不出现在这一页；能力不存在的（硬件解码、采集并发之类）
 * 干脆不渲染，而不是摆一个点了没反应的开关。
 *
 * 生效时机逐项写清：列数与首页显隐走 layout store（首页挂在 KeepAlive 下，只有跟着同一
 * 份状态才改完就看得见），播放器偏好写 vp_settings / cczj_auto_next（下次打开播放器时读），
 * 其余走各自的即时接口。
 */
defineOptions({ name: 'SettingsBasicGroup' })
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  GetSetting, SetSetting, GetCloseBehavior, SetCloseBehavior,
  WindowSetResizable, WindowGetResizable, WindowSetSize, WindowGetSize,
  GetDoubanIntervalMinutes, SetDoubanIntervalMinutes,
  GetLogKeepDays, SetLogKeepDays,
  GetIgnoredVersion, IgnoreVersion,
} from '../../api/app'
import { useErrorStore } from '../../stores/error'
import { useLayoutStore, type LayoutDensity } from '../../stores/layout'
import { useMotionStore } from '../../stores/motion'
import { createPlayerSettings } from '../../player/settings'
import { readStorageBoolean, writeStorage } from '../../platform/storage'
import { Segment, Select as SelectDropdown } from '../../components/ui'
import { setLocale as saveLocalePreference } from '../../locales'

const { t } = useI18n()
const errorStore = useErrorStore()
const layoutStore = useLayoutStore()
const motion = useMotionStore()
// 播放器偏好存在 localStorage 的 vp_settings 容器里：与播放器共用同一份读写口径，
// 这里改的值就是下次打开播放器时读到的值。
const playerSettings = createPlayerSettings()

// ---------- 外观 ----------
const language = ref('zh-CN')
const languageOptions = computed(() => [
  { value: 'zh-CN', label: t('settings.languageOptions.zhCN') },
  { value: 'en', label: t('settings.languageOptions.en') },
])

const fontFamily = ref('')
const fontFamilyOptions = computed(() => [
  { value: '', label: t('settings.fontOptions.default') },
  { value: '"Microsoft YaHei", "微软雅黑", sans-serif', label: t('settings.fontOptions.yahei') },
  { value: '"Source Han Sans SC", "思源黑体", sans-serif', label: t('settings.fontOptions.sourceHan') },
  { value: '"SimSun", "宋体", serif', label: t('settings.fontOptions.simsun') },
  { value: '"KaiTi", "楷体", serif', label: t('settings.fontOptions.kaiti') },
])

// 字号档位把像素写在标签上：只写「大」的话要点上去才知道到底多大。
const fontSizePresets = computed(() => [
  { key: 'xs', label: `${t('settings.fontSizePresets.xs')} · 12px`, px: 12 },
  { key: 'sm', label: `${t('settings.fontSizePresets.sm')} · 13px`, px: 13 },
  { key: 'md', label: `${t('settings.fontSizePresets.md')} · 14px`, px: 14 },
  { key: 'lg', label: `${t('settings.fontSizePresets.lg')} · 16px`, px: 16 },
  { key: 'xl', label: `${t('settings.fontSizePresets.xl')} · 18px`, px: 18 },
  { key: 'xxl', label: `${t('settings.fontSizePresets.xxl')} · 20px`, px: 20 },
])
const fontSizeOptions = computed(() => fontSizePresets.value.map(p => ({ value: p.key, label: p.label })))
const fontSizeKey = ref('md')

function applyFontSize(key: string): void {
  const preset = fontSizePresets.value.find(p => p.key === key)
  if (!preset) return
  fontSizeKey.value = key
  document.documentElement.style.fontSize = preset.px + 'px'
  document.documentElement.style.setProperty('--font-size-base', preset.px + 'px')
  save('font_size_key', key)
}

function applyFontFamily(value: string): void {
  fontFamily.value = value
  if (value) document.documentElement.style.fontFamily = value
  else document.documentElement.style.removeProperty('font-family')
  save('font_family', value)
}

async function applyLanguage(value: string): Promise<void> {
  language.value = value
  saveLocalePreference(value)
  await save('language', value)
}

// ---------- 窗口 ----------
const MIN_WINDOW_WIDTH = 800
const MIN_WINDOW_HEIGHT = 500
const windowResizable = ref(true)
const windowWidth = ref(1280)
const windowHeight = ref(800)

const windowSizePresets = computed(() => [
  { key: 'xs', label: `${t('settings.windowSizePresets.xs')} · 1024×640`, width: 1024, height: 640 },
  { key: 'sm', label: `${t('settings.windowSizePresets.sm')} · 1152×720`, width: 1152, height: 720 },
  { key: 'md', label: `${t('settings.windowSizePresets.md')} · 1280×800`, width: 1280, height: 800 },
  { key: 'lg', label: `${t('settings.windowSizePresets.lg')} · 1366×854`, width: 1366, height: 854 },
  { key: 'xl', label: `${t('settings.windowSizePresets.xl')} · 1440×900`, width: 1440, height: 900 },
  { key: 'xxl', label: `${t('settings.windowSizePresets.xxl')} · 1600×1000`, width: 1600, height: 1000 },
  { key: 'huge', label: `${t('settings.windowSizePresets.huge')} · 1920×1080`, width: 1920, height: 1080 },
])
// '自定义' 用一个空键的选项占位：手工填过宽高之后下拉不该指着某个预设。
const windowSizeOptions = computed(() => [
  ...windowSizePresets.value.map(p => ({ value: p.key, label: p.label })),
  { value: '', label: t('settings.windowSizeCustomOption') },
])
const windowSizeKey = computed(() =>
  windowSizePresets.value.find(p => p.width === windowWidth.value && p.height === windowHeight.value)?.key ?? ''
)

async function applyWindowPreset(key: string): Promise<void> {
  const preset = windowSizePresets.value.find(p => p.key === key)
  if (!preset) return
  windowWidth.value = preset.width
  windowHeight.value = preset.height
  // 不再另存一把 window_size_key：Go 侧 SetSize 会把夹过的宽高直接落库，下次启动照原样开窗。
  try { await WindowSetSize(preset.width, preset.height) } catch { /* 忽略 */ }
}

/** 宽高任一改动都成对下发：Go 侧按最小值夹住并落库，这里回读一次好让界面不说谎。 */
async function applyWindowSize(): Promise<void> {
  const width = Math.max(MIN_WINDOW_WIDTH, Math.round(windowWidth.value) || MIN_WINDOW_WIDTH)
  const height = Math.max(MIN_WINDOW_HEIGHT, Math.round(windowHeight.value) || MIN_WINDOW_HEIGHT)
  windowWidth.value = width
  windowHeight.value = height
  try {
    await WindowSetSize(width, height)
    const size = await WindowGetSize()
    if (size) {
      windowWidth.value = size.width
      windowHeight.value = size.height
    }
  } catch { /* 忽略 */ }
}

async function saveWindowResizable(): Promise<void> {
  try { await WindowSetResizable(windowResizable.value) } catch { /* 忽略 */ }
}

// ---------- 首页 ----------
const closeToTray = ref(false)

async function saveCloseBehavior(): Promise<void> {
  try { await SetCloseBehavior(closeToTray.value) } catch { /* 忽略 */ }
}

const carouselAutoPlay = computed({
  get: () => layoutStore.carouselAutoPlay,
  set: (v: boolean) => void layoutStore.setCarouselAutoPlay(v),
})
const carouselSeconds = computed({
  get: () => layoutStore.carouselSeconds,
  set: (v: number) => void layoutStore.setCarouselSeconds(v),
})

// ---------- 播放 ----------
const autoNext = ref(true)
const resumeJump = ref(false)
const playbackSpeed = ref('1')
const qualityMode = ref('original')
const anime4kTier = ref('M')

const speedValues = ['0.5', '0.75', '1', '1.25', '1.5', '2']
const speedOptions = computed(() => {
  const values = [...speedValues]
  // 播放中用倍速菜单存过非标准值（1.35× 之类）时把它保留成一项，别让下拉显示成没选。
  if (!values.includes(playbackSpeed.value)) values.unshift(playbackSpeed.value)
  return values.map(v => ({ value: v, label: `${v}×` }))
})

const qualityOptions = computed(() => {
  const base = [
    { value: 'original', label: t('player.originalQuality') },
    { value: 'ai_anime', label: t('player.animeEnhance') },
    { value: 'ai_film', label: t('player.filmEnhance') },
  ]
  // 扩展包着色器有几十种具体档位，这一页只负责在用户没换回内置档时保住现状。
  if (qualityMode.value === 'ai_custom') base.push({ value: 'ai_custom', label: t('settings.qualityPackShader') })
  return base
})

const anime4kTierOptions = [
  { value: 'S', label: 'S' },
  { value: 'M', label: 'M' },
  { value: 'L', label: 'L' },
]

function savePlayerPref(key: string, value: string): void {
  playerSettings.write(key, value)
}

function setAutoNext(next: boolean): void {
  autoNext.value = next
  writeStorage('cczj_auto_next', next)
}

// ---------- 数据与网络 ----------
const doubanIntervalMinutes = ref(30)
// 豆瓣补全的随机抖动在 Go 侧闸门里，这里只排每轮的起点，所以最小只给到 10 分钟：
// 比这更短就是把「调快一点」变成「更容易被封」，不提供那种档位。
const doubanIntervalOptions = computed(() =>
  [10, 15, 20, 30, 45, 60, 90, 120].map(m => ({ value: String(m), label: t('settings.doubanEvery', { n: m }) }))
)

async function saveDoubanInterval(value: string | number): Promise<void> {
  try {
    doubanIntervalMinutes.value = await SetDoubanIntervalMinutes(Number(value) || 30)
    errorStore.info(t('common.saved'), t('settings.doubanIntervalSaved', { n: doubanIntervalMinutes.value }), '', 'Settings.saveDoubanInterval')
  } catch (e: any) {
    errorStore.fromError(t('settings.doubanIntervalSaveFailed'), e, 'Settings.saveDoubanInterval')
  }
}

const freshness = ref<'fast' | 'standard' | 'saver'>('standard')
// 用 computed 而不是模块级常量：后者会把 t() 在初始化时求值一次，切语言后标签不再变。
const freshnessOptions = computed(() => [
  { value: 'fast', label: t('settings.freshnessFast') },
  { value: 'standard', label: t('settings.freshnessStandard') },
  { value: 'saver', label: t('settings.freshnessSaver') },
])

// 写失败必须报出来：静默吞掉的话，界面上的档位会停在一个没生效的选择上。
async function saveFreshness(value: string): Promise<void> {
  const next: 'fast' | 'standard' | 'saver' = value === 'fast' || value === 'saver' ? value : 'standard'
  freshness.value = next
  try {
    await SetSetting('detail_cache_freshness', next)
    errorStore.info(t('common.saved'), t('settings.freshnessSaved'), '', 'Settings.saveFreshness')
  } catch (e: any) {
    errorStore.fromError(t('settings.freshnessSaveFailed'), e, 'Settings.saveFreshness')
  }
}

// ---------- 维护 ----------
const logKeepDays = ref(30)
const logKeepOptions = computed(() =>
  [7, 14, 30, 60, 90].map(d => ({ value: String(d), label: t('settings.logKeepDays', { n: d }) }))
)

async function saveLogKeepDays(value: string | number): Promise<void> {
  try {
    logKeepDays.value = await SetLogKeepDays(Number(value) || 30)
    errorStore.info(t('common.saved'), t('settings.logRetentionSaved', { n: logKeepDays.value }), '', 'Settings.saveLogKeepDays')
  } catch (e: any) {
    errorStore.fromError(t('settings.logRetentionSaveFailed'), e, 'Settings.saveLogKeepDays')
  }
}

const ignoredVersion = ref('')

async function restoreUpdateChecks(): Promise<void> {
  try {
    await IgnoreVersion('')
    ignoredVersion.value = ''
    errorStore.info(t('common.saved'), t('settings.ignoredVersionRestored'), '', 'Settings.restoreUpdateChecks')
  } catch (e: any) {
    errorStore.fromError(t('settings.ignoredVersionRestoreFailed'), e, 'Settings.restoreUpdateChecks')
  }
}

// ---------- 通用读写 ----------
async function safeGet(key: string, fallback: string): Promise<string> {
  try { const v = await GetSetting(key); return v || fallback } catch { return fallback }
}
async function save(key: string, val: string | number | boolean): Promise<void> {
  try { await SetSetting(key, String(val)) } catch { /* 忽略 */ }
}

// ---------- 启动：与拆分前设置页 onMounted 里这几行的顺序、次数一致 ----------
onMounted(async () => {
  // 子组件先于父级 onMounted 挂载，而首页那几项的真值只在 layout store 里；
  // 不先 load 就会把「缺省值」显示成「当前设置」，且滑块一碰就把缺省写回库。
  await layoutStore.load()
  await loadWindowResizable()
  await loadWindowSize()

  const fsk = await safeGet('font_size_key', 'md')
  if (fontSizePresets.value.find(p => p.key === fsk)) fontSizeKey.value = fsk
  applyFontSize(fontSizeKey.value)
  fontFamily.value = await safeGet('font_family', '')
  applyFontFamily(fontFamily.value)
  const lang = await safeGet('language', 'zh-CN')
  language.value = lang || 'zh-CN'

  closeToTray.value = await safeGetCloseBehavior()

  autoNext.value = readStorageBoolean('cczj_auto_next', true)
  resumeJump.value = playerSettings.read('auto_resume_jump', '0') === '1'
  playbackSpeed.value = playerSettings.read('speed', '1') || '1'
  qualityMode.value = normalizeQuality(playerSettings.read('quality_mode', 'original'))
  anime4kTier.value = playerSettings.read('anime4k_tier', 'M') || 'M'

  try { doubanIntervalMinutes.value = await GetDoubanIntervalMinutes() } catch { /* 忽略 */ }
  try { logKeepDays.value = await GetLogKeepDays() } catch { /* 忽略 */ }
  try { ignoredVersion.value = await GetIgnoredVersion() } catch { /* 忽略 */ }
  freshness.value = await loadFreshness()
})

async function loadWindowResizable(): Promise<void> {
  try { windowResizable.value = await WindowGetResizable() } catch { /* 忽略 */ }
}

async function loadWindowSize(): Promise<void> {
  try {
    const size = await WindowGetSize()
    if (!size) return
    windowWidth.value = size.width
    windowHeight.value = size.height
  } catch { /* 忽略 */ }
}

async function loadFreshness(): Promise<'fast' | 'standard' | 'saver'> {
  const raw = await safeGet('detail_cache_freshness', 'standard')
  return raw === 'fast' || raw === 'saver' ? raw : 'standard'
}

async function safeGetCloseBehavior(): Promise<boolean> {
  try { return await GetCloseBehavior() } catch { return false }
}

/** 与播放器 useVideoQuality 的归一保持一致：旧键值与扩展包档位在进下拉之前先认出来。 */
function normalizeQuality(value: string): string {
  if (value === 'ai_frame_interp' || value === 'ai_enhance') return 'ai_anime'
  if (value === 'ai_anime' || value === 'ai_film' || value === 'ai_custom') return value
  return 'original'
}
</script>

<template>
  <div class="panel group-card cczj-flex cczj-flex-col cczj-gap-2">

    <!-- ==================== 外观 ==================== -->
    <section class="block">
      <h3>{{ t('settings.secAppearance') }}</h3>

      <div class="item">
        <div class="item-text">
          <span class="item-name">{{ t('settings.language') }}</span>
        </div>
        <div class="item-control">
          <SelectDropdown :model-value="language" :options="languageOptions"
            @update:model-value="(v: any) => applyLanguage(String(v))" />
        </div>
      </div>

      <div class="item">
        <div class="item-text">
          <span class="item-name">{{ t('settings.fontSize') }}</span>
        </div>
        <div class="item-control">
          <SelectDropdown :model-value="fontSizeKey" :options="fontSizeOptions"
            @update:model-value="(v: any) => applyFontSize(String(v))" />
        </div>
      </div>

      <div class="item">
        <div class="item-text">
          <span class="item-name">{{ t('settings.fontFamily') }}</span>
        </div>
        <div class="item-control">
          <SelectDropdown :model-value="fontFamily" :options="fontFamilyOptions"
            @update:model-value="(v: any) => applyFontFamily(String(v))" />
        </div>
      </div>

      <!-- 动效由内置扩展包提供：包被停用或删掉时这个开关无处生效，整行就不出现。 -->
      <div v-if="motion.packReady" class="item">
        <div class="item-text">
          <span class="item-name">{{ t('settings.motionSwitch') }}</span>
          <span class="item-desc">{{ t('settings.motionSwitchDesc') }}</span>
        </div>
        <div class="item-control">
          <label class="switch cczj-cursor-pointer">
            <input type="checkbox" :checked="motion.enabled"
              @change="motion.setEnabled(($event.target as HTMLInputElement).checked)" />
            <span class="slider" />
          </label>
        </div>
      </div>
    </section>

    <!-- ==================== 窗口 ==================== -->
    <section class="block">
      <h3>{{ t('settings.secWindow') }}</h3>

      <div class="item">
        <div class="item-text">
          <span class="item-name">{{ t('settings.windowSize') }}</span>
          <span class="item-desc">{{ t('settings.windowSizeDesc') }}</span>
        </div>
        <div class="item-control">
          <SelectDropdown :model-value="windowSizeKey" :options="windowSizeOptions"
            @update:model-value="(v: any) => v && applyWindowPreset(String(v))" />
        </div>
      </div>

      <div class="item">
        <div class="item-text">
          <span class="item-name">{{ t('settings.windowCustomSize') }}</span>
          <span class="item-desc">{{ t('settings.windowSizeHint', { w: MIN_WINDOW_WIDTH, h: MIN_WINDOW_HEIGHT }) }}</span>
        </div>
        <div class="item-control cczj-inline-flex cczj-items-center">
          <span class="field-label">{{ t('settings.windowWidth') }}</span>
          <input class="num" type="number" step="10" :min="MIN_WINDOW_WIDTH" v-model.number="windowWidth"
            @change="applyWindowSize" />
          <span class="field-label">{{ t('settings.windowHeight') }}</span>
          <input class="num" type="number" step="10" :min="MIN_WINDOW_HEIGHT" v-model.number="windowHeight"
            @change="applyWindowSize" />
          <span class="unit">px</span>
        </div>
      </div>

      <div class="item">
        <div class="item-text">
          <span class="item-name">{{ t('settings.allowResize') }}</span>
        </div>
        <div class="item-control">
          <label class="switch cczj-cursor-pointer">
            <input type="checkbox" v-model="windowResizable" @change="saveWindowResizable" />
            <span class="slider" />
          </label>
        </div>
      </div>

      <div class="item">
        <div class="item-text">
          <span class="item-name">{{ t('settings.closeBehavior') }}</span>
          <span class="item-desc">{{ t('settings.minimizeToTray') }}</span>
        </div>
        <div class="item-control">
          <label class="switch cczj-cursor-pointer">
            <input type="checkbox" v-model="closeToTray" @change="saveCloseBehavior" />
            <span class="slider" />
          </label>
        </div>
      </div>
    </section>

    <!-- ==================== 首页 ==================== -->
    <section class="block">
      <h3>{{ t('settings.secHome') }}</h3>

      <div class="item">
        <div class="item-text">
          <span class="item-name">{{ t('settings.gridColumns') }}</span>
        </div>
        <div class="item-control cczj-inline-flex cczj-items-center">
          <input type="range" :value="layoutStore.columns" min="2" max="10"
            @input="layoutStore.previewColumns(Number(($event.target as HTMLInputElement).value))"
            @change="layoutStore.setColumns(layoutStore.columns)" />
          <span class="value">{{ t('settings.columnCount', { n: layoutStore.columns }) }}</span>
        </div>
      </div>

      <div class="item">
        <div class="item-text">
          <span class="item-name">{{ t('settings.cardDensity') }}</span>
        </div>
        <div class="item-control">
          <Segment :model-value="layoutStore.density"
            :options="[{ value: 'comfortable', label: t('settings.comfortable') }, { value: 'compact', label: t('settings.compact') }, { value: 'spacious', label: t('settings.spacious') }]"
            @update:model-value="(v: any) => layoutStore.setDensity(v as LayoutDensity)" />
        </div>
      </div>

      <div class="item">
        <div class="item-text">
          <span class="item-name">{{ t('settings.showCarousel') }}</span>
          <span class="item-desc">{{ t('settings.showCarouselDesc') }}</span>
        </div>
        <div class="item-control">
          <label class="switch cczj-cursor-pointer">
            <input type="checkbox" :checked="layoutStore.showCarousel"
              @change="layoutStore.setShowCarousel(($event.target as HTMLInputElement).checked)" />
            <span class="slider" />
          </label>
        </div>
      </div>

      <div class="item" v-if="layoutStore.showCarousel">
        <div class="item-text">
          <span class="item-name">{{ t('settings.carouselAutoPlay') }}</span>
          <span class="item-desc">{{ t('settings.carouselAutoPlayDesc') }}</span>
        </div>
        <div class="item-control">
          <label class="switch cczj-cursor-pointer">
            <input type="checkbox" v-model="carouselAutoPlay" />
            <span class="slider" />
          </label>
        </div>
      </div>

      <div class="item" v-if="layoutStore.showCarousel && carouselAutoPlay">
        <div class="item-text">
          <span class="item-name">{{ t('settings.carouselInterval') }}</span>
        </div>
        <div class="item-control cczj-inline-flex cczj-items-center">
          <input class="num" type="number" min="3" max="60" step="1" v-model.number="carouselSeconds" />
          <span class="unit">{{ t('common.unitSecond') }}</span>
        </div>
      </div>

      <div class="item">
        <div class="item-text">
          <span class="item-name">{{ t('settings.showRecommend') }}</span>
          <span class="item-desc">{{ t('settings.showRecommendDesc') }}</span>
        </div>
        <div class="item-control">
          <label class="switch cczj-cursor-pointer">
            <input type="checkbox" :checked="layoutStore.showRecommend"
              @change="layoutStore.setShowRecommend(($event.target as HTMLInputElement).checked)" />
            <span class="slider" />
          </label>
        </div>
      </div>
    </section>

    <!-- ==================== 播放 ==================== -->
    <section class="block">
      <h3>{{ t('settings.secPlayer') }}</h3>
      <p class="block-note">{{ t('settings.playerPrefsHint') }}</p>

      <div class="item">
        <div class="item-text">
          <span class="item-name">{{ t('player.autoNext') }}</span>
          <span class="item-desc">{{ t('settings.autoNextDesc') }}</span>
        </div>
        <div class="item-control">
          <label class="switch cczj-cursor-pointer">
            <input type="checkbox" :checked="autoNext" @change="setAutoNext(($event.target as HTMLInputElement).checked)" />
            <span class="slider" />
          </label>
        </div>
      </div>

      <div class="item">
        <div class="item-text">
          <span class="item-name">{{ t('settings.resumeJump') }}</span>
          <span class="item-desc">{{ t('settings.resumeJumpDesc') }}</span>
        </div>
        <div class="item-control">
          <label class="switch cczj-cursor-pointer">
            <input type="checkbox" v-model="resumeJump" @change="savePlayerPref('auto_resume_jump', resumeJump ? '1' : '0')" />
            <span class="slider" />
          </label>
        </div>
      </div>

      <div class="item">
        <div class="item-text">
          <span class="item-name">{{ t('player.playbackSpeed') }}</span>
          <span class="item-desc">{{ t('settings.playbackSpeedDesc') }}</span>
        </div>
        <div class="item-control">
          <SelectDropdown :model-value="playbackSpeed" :options="speedOptions"
            @update:model-value="(v: any) => { playbackSpeed = String(v); savePlayerPref('speed', String(v)) }" />
        </div>
      </div>

      <div class="item">
        <div class="item-text">
          <span class="item-name">{{ t('settings.qualityPreset') }}</span>
          <span class="item-desc">{{ t('settings.qualityPresetDesc') }}</span>
        </div>
        <div class="item-control">
          <SelectDropdown :model-value="qualityMode" :options="qualityOptions"
            @update:model-value="(v: any) => { qualityMode = String(v); savePlayerPref('quality_mode', String(v)) }" />
        </div>
      </div>

      <div class="item" v-if="qualityMode === 'ai_anime'">
        <div class="item-text">
          <span class="item-name">{{ t('settings.anime4kTier') }}</span>
          <span class="item-desc">{{ t('settings.anime4kTierDesc') }}</span>
        </div>
        <div class="item-control">
          <Segment :model-value="anime4kTier" :options="anime4kTierOptions"
            @update:model-value="(v: any) => { anime4kTier = String(v); savePlayerPref('anime4k_tier', String(v)) }" />
        </div>
      </div>
    </section>

    <!-- ==================== 数据与网络 ==================== -->
    <section class="block">
      <h3>{{ t('settings.secData') }}</h3>

      <div class="item">
        <div class="item-text">
          <span class="item-name">{{ t('settings.freshness') }}</span>
          <span class="item-desc">{{ t('settings.freshnessDesc') }}</span>
        </div>
        <div class="item-control">
          <Segment :model-value="freshness" :options="freshnessOptions"
            @update:model-value="(v: any) => saveFreshness(String(v))" />
        </div>
      </div>

      <div class="item">
        <div class="item-text">
          <span class="item-name">{{ t('settings.doubanPolling') }}</span>
          <span class="item-desc">{{ t('settings.doubanPollingDesc') }}</span>
        </div>
        <div class="item-control">
          <SelectDropdown :model-value="String(doubanIntervalMinutes)" :options="doubanIntervalOptions"
            @update:model-value="(v: any) => saveDoubanInterval(v)" />
        </div>
      </div>
    </section>

    <!-- ==================== 维护 ==================== -->
    <section class="block">
      <h3>{{ t('settings.secMaintenance') }}</h3>

      <div class="item">
        <div class="item-text">
          <span class="item-name">{{ t('settings.logRetention') }}</span>
          <span class="item-desc">{{ t('settings.logRetentionDesc') }}</span>
        </div>
        <div class="item-control">
          <SelectDropdown :model-value="String(logKeepDays)" :options="logKeepOptions"
            @update:model-value="(v: any) => saveLogKeepDays(v)" />
        </div>
      </div>

      <div class="item">
        <div class="item-text">
          <span class="item-name">{{ t('settings.ignoredVersion') }}</span>
          <span class="item-desc">{{ ignoredVersion ? ignoredVersion : t('settings.ignoredVersionNone') }}</span>
        </div>
        <div class="item-control" v-if="ignoredVersion">
          <button class="mini-btn cczj-cursor-pointer" @click="restoreUpdateChecks">
            {{ t('settings.restoreUpdates') }}
          </button>
        </div>
      </div>
    </section>

  </div>
</template>

<style scoped>
/* h3 这类内部元素拿不到父级（Settings.vue）的 scope id，所以标题排版留在用到它的那一组里
   （同 DataBackupPanel）；卡片外框与 .block 的压平仍由 settings.css 的 .panel.group-card 负责。 */
.block h3 {
  font-size: 0.97rem;
  font-weight: 700;
  margin: 0 0 6px;
  letter-spacing: 0.3px;
}
.block-note {
  margin: 0 0 10px;
  font-size: 0.86rem;
  color: var(--text-muted);
  line-height: 1.5;
}

/* 一行一项：左边名称+说明，右边控件。行内不画分隔线，块与块之间已有发丝线。 */
.item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 24px;
  padding: 10px 0;
}
.item-text {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}
.item-name {
  font-size: 0.95rem;
  color: var(--text-primary);
}
.item-desc {
  font-size: 0.83rem;
  color: var(--text-muted);
  line-height: 1.5;
}
.item-control {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}
.item-control :deep(.select-dropdown) {
  width: 190px;
}

.field-label {
  font-size: 0.86rem;
  color: var(--text-muted);
}
.unit {
  color: var(--text-muted);
  font-size: 0.86rem;
}
.num {
  width: 78px;
  padding: 6px 8px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--bg-input);
  color: var(--text-primary);
  font-family: inherit;
  font-size: 0.9rem;
  font-variant-numeric: tabular-nums;
  text-align: right;
  outline: none;
}
.num:focus {
  border-color: var(--accent);
}
.value {
  min-width: 58px;
  font-weight: 600;
  color: var(--text-secondary);
  font-variant-numeric: tabular-nums;
}

.mini-btn {
  padding: 7px 14px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--bg-card);
  color: var(--text-primary);
  font-size: 0.88rem;
  font-weight: 600;
  transition: background-color 0.15s ease, border-color 0.15s ease;
}
.mini-btn:hover:not(:disabled) {
  background: var(--bg-hover);
  border-color: var(--accent);
}
.mini-btn:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}

.item input[type='range'] {
  width: 180px;
  accent-color: var(--accent);
}

/* 开关：滑块式，与设置页其余分组一致。 */
.switch {
  position: relative;
  display: inline-block;
  width: 40px;
  height: 22px;
  flex-shrink: 0;
}
.switch input {
  position: absolute;
  opacity: 0;
  width: 100%;
  height: 100%;
  margin: 0;
  cursor: pointer;
}
.switch .slider {
  position: absolute;
  inset: 0;
  border-radius: 999px;
  background: var(--bg-secondary);
  border: 1px solid var(--border-strong);
  transition: background-color 0.18s ease, border-color 0.18s ease;
  pointer-events: none;
}
.switch .slider::before {
  content: '';
  position: absolute;
  top: 2px;
  left: 2px;
  width: 16px;
  height: 16px;
  border-radius: 50%;
  background: var(--text-secondary);
  transition: transform 0.18s ease, background-color 0.18s ease;
}
.switch input:checked + .slider {
  background: var(--accent);
  border-color: var(--accent);
}
.switch input:checked + .slider::before {
  transform: translateX(18px);
  background: var(--accent-contrast);
}
.switch input:focus-visible + .slider {
  outline: 2px solid var(--accent);
  outline-offset: 2px;
}
</style>
