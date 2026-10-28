<script setup lang="ts">
/** Application preferences live here; playback controls remain in the player. */
defineOptions({ name: 'SettingsBasicGroup' })
import { computed, onMounted, ref, onBeforeUnmount } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import {
  GetSetting, SetSetting, GetCloseBehavior, SetCloseBehavior,
  WindowSetResizable, WindowGetResizable, WindowSetSize, WindowGetSize,
  GetLogKeepDays, SetLogKeepDays,
  GetIgnoredVersion, IgnoreVersion,
} from '../../api/app'
import { useErrorStore } from '../../stores/error'
import { useLayoutStore, type LayoutDensity } from '../../stores/layout'
import { useMotionStore } from '../../stores/motion'
import NumberField from '../../components/ui/NumberField.vue'
import SettingsDataPreferences from './SettingsDataPreferences.vue'
import { Segment, Select as SelectDropdown } from '../../components/ui'
import Icon from '../../components/Icon.vue'
import ApplicationModeSwitch from '../../modes/ApplicationModeSwitch.vue'
import { setLocale as saveLocalePreference } from '../../locales'
import { isMobileRuntime } from '../../api/runtime'
import { FONT_SIZE_PRESETS, applyFontSize as applyAppFontSize, applyFontFamily as applyAppFontFamily } from '../../platform/typography'

const { t } = useI18n()
const mobile = isMobileRuntime()
const route = useRoute()
const router = useRouter()
const errorStore = useErrorStore()
const layoutStore = useLayoutStore()
const motion = useMotionStore()
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
const fontSizePresets = computed(() => FONT_SIZE_PRESETS.map(preset => ({
  ...preset, label:`${t(`settings.fontSizePresets.${preset.key}`)} · ${preset.px}px`,
})))
const fontSizeOptions = computed(() => fontSizePresets.value.map(p => ({ value: p.key, label: p.label })))
const fontSizeKey = ref('md')

function applyFontSize(key: string, persist = true): void {
  const preset = fontSizePresets.value.find(p => p.key === key)
  if (!preset) return
  fontSizeKey.value = key
  applyAppFontSize(key)
  if (persist) void save('font_size_key', key)
}

function applyFontFamily(value: string, persist = true): void {
  fontFamily.value = value
  applyAppFontFamily(value)
  if (persist) void save('font_family', value)
}

async function applyLanguage(value: string): Promise<void> {
  language.value = value
  saveLocalePreference(value)
  await save('language', value)
}

// ---------- 窗口 ----------
const MIN_WINDOW_WIDTH = 960
const MIN_WINDOW_HEIGHT = 600
const windowResizable = ref(true)
const windowWidth = ref(1280)
const windowHeight = ref(800)
let resizeRevision = 0
let resizeWrites = Promise.resolve()

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
  await applyWindowSize()
}

/** 宽高任一改动都成对下发：Go 侧按最小值夹住并落库，这里回读一次好让界面不说谎。 */
function applyWindowSize(): Promise<void> {
  const width = Math.max(MIN_WINDOW_WIDTH, Math.round(windowWidth.value) || MIN_WINDOW_WIDTH)
  const height = Math.max(MIN_WINDOW_HEIGHT, Math.round(windowHeight.value) || MIN_WINDOW_HEIGHT)
  windowWidth.value = width
  windowHeight.value = height
  const revision = ++resizeRevision
  const write = resizeWrites.then(async () => {
    if (disposed || revision !== resizeRevision) return
    try {
      await WindowSetSize(width, height)
      if (disposed || revision !== resizeRevision) return
      const size = await WindowGetSize()
      if (size && !disposed && revision === resizeRevision) {
        windowWidth.value = size.width
        windowHeight.value = size.height
      }
    } catch (error) {
      if (disposed || revision !== resizeRevision) return
      errorStore.fromError(t('settings.saveFailed'), error, 'Settings.windowSize')
      await loadWindowSize()
    }
  })
  resizeWrites = write.catch(() => {})
  return write
}

async function saveWindowResizable(): Promise<void> {
  try { await WindowSetResizable(windowResizable.value) } catch (error) {
    errorStore.fromError(t('settings.saveFailed'), error, 'Settings.windowResizable')
    await loadWindowResizable()
  }
}

// ---------- 首页 ----------
const closeToTray = ref(false)

async function saveCloseBehavior(): Promise<void> {
  try { await SetCloseBehavior(closeToTray.value) } catch (error) {
    errorStore.fromError(t('settings.saveFailed'), error, 'Settings.closeBehavior')
    closeToTray.value = await safeGetCloseBehavior()
  }
}

const carouselAutoPlay = computed({
  get: () => layoutStore.carouselAutoPlay,
  set: (v: boolean) => void layoutStore.setCarouselAutoPlay(v),
})
const carouselSeconds = computed({
  get: () => layoutStore.carouselSeconds,
  set: (v: number) => void layoutStore.setCarouselSeconds(v),
})

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
  try { await SetSetting(key, String(val)) } catch (error) { errorStore.fromError(t('settings.saveFailed'), error, 'Settings.save') }
}

// ---------- 启动：与拆分前设置页 onMounted 里这几行的顺序、次数一致 ----------
let disposed = false
onBeforeUnmount(() => { disposed = true })
onMounted(async () => {
  const [fsk, family, lang, close, retention, ignored] = await Promise.all([
    safeGet('font_size_key', 'md'), safeGet('font_family', ''), safeGet('language', 'zh-CN'),
    mobile ? Promise.resolve(false) : safeGetCloseBehavior(),
    GetLogKeepDays().catch(() => 30), GetIgnoredVersion().catch(() => ''),
    layoutStore.load(), ...(mobile ? [] : [loadWindowResizable(), loadWindowSize()]),
  ])
  if (disposed) return
  initializing.value = false
  if (fontSizePresets.value.find(p => p.key === fsk)) fontSizeKey.value = fsk
  applyFontSize(fontSizeKey.value, false)
  applyFontFamily(family, false)
  language.value = lang
  closeToTray.value = close
  logKeepDays.value = retention
  ignoredVersion.value = ignored

})

const sections = computed(() => [
  { id: 'appearance', label: t('settings.secAppearance'), icon: 'palette' },
  { id: 'window', label: t('settings.secWindow'), icon: 'monitor' },
  { id: 'home', label: t('settings.secHome'), icon: 'home' },
  { id: 'data', label: t('settings.secData'), icon: 'database' },
  { id: 'maintenance', label: t('settings.secMaintenance'), icon: 'settings' },
].filter(section => !mobile || section.id !== 'window'))
const initializing = ref(true)
const basicRoot = ref<HTMLElement | null>(null)
const mobileSection = computed(() => sections.value.some(s => s.id === route.query.section) ? String(route.query.section) : 'appearance')
function jumpTo(id: string): void {
  if (mobile) {
    void router.replace({ path: '/settings', query: { group: 'basic', section: id } })
    basicRoot.value?.closest('.main-content')?.scrollTo({ top: 0, behavior: 'auto' })
    return
  }
  const section = basicRoot.value?.querySelector<HTMLElement>(`#basic-${id}`)
  section?.scrollIntoView({ behavior: motion.enabled && !matchMedia('(prefers-reduced-motion: reduce)').matches ? 'smooth' : 'auto', block: 'start' })
  section?.focus({ preventScroll: true })
}

async function loadWindowResizable(): Promise<void> {
  try { windowResizable.value = await WindowGetResizable() } catch { /* 忽略 */ }
}

async function loadWindowSize(expectedRevision = resizeRevision): Promise<void> {
  try {
    const size = await WindowGetSize()
    if (!size || disposed || expectedRevision !== resizeRevision) return
    windowWidth.value = size.width
    windowHeight.value = size.height
  } catch { /* 忽略 */ }
}

async function safeGetCloseBehavior(): Promise<boolean> {
  try { return await GetCloseBehavior() } catch { return false }
}

const emit = defineEmits<{ (e: 'open-group', id: string): void }>()
</script>

<template>
  <div ref="basicRoot" class="basic-settings">
    <nav class="section-links" :aria-label="t('settings.quickJump')">
      <button v-for="section in sections" :key="section.id" :aria-current="mobile && mobileSection === section.id ? 'page' : undefined" @click="jumpTo(section.id)">
        <Icon :name="section.icon" :size="14" />{{ section.label }}
      </button>
    </nav>
    <fieldset class="basic-fields" :disabled="initializing">
    <div class="basic-grid">

    <!-- ==================== 外观 ==================== -->
    <section v-show="!mobile || mobileSection === 'appearance'" id="basic-appearance" class="block basic-section" tabindex="-1" style="--section-order: 0">
      <header class="section-heading">
        <span class="section-icon"><Icon name="palette" :size="18" /></span>
        <h3>{{ t('settings.secAppearance') }}</h3>
        <span class="section-number">01</span>
      </header>

      <ApplicationModeSwitch />
      <div class="item">
        <div class="item-text">
          <span class="item-name">{{ t('settings.language') }}</span>
        </div>
        <div class="item-control">
          <SelectDropdown :aria-label="t('settings.language')" :model-value="language" :options="languageOptions"
            @update:model-value="(v: any) => applyLanguage(String(v))" />
        </div>
      </div>

      <div class="item">
        <div class="item-text">
          <span class="item-name">{{ t('settings.fontSize') }}</span>
        </div>
        <div class="item-control">
          <SelectDropdown :aria-label="t('settings.fontSize')" :model-value="fontSizeKey" :options="fontSizeOptions"
            @update:model-value="(v: any) => applyFontSize(String(v))" />
        </div>
      </div>

      <div v-if="!mobile" class="item">
        <div class="item-text">
          <span class="item-name">{{ t('settings.fontFamily') }}</span>
        </div>
        <div class="item-control">
          <SelectDropdown :aria-label="t('settings.fontFamily')" :model-value="fontFamily" :options="fontFamilyOptions"
            @update:model-value="(v: any) => applyFontFamily(String(v))" />
        </div>
      </div>

      <!-- 动效由内置扩展包提供：包被停用或删掉时这个开关无处生效，整行就不出现。 -->
      <div v-if="motion.packReady" class="item">
        <div class="item-text">
          <span class="item-name">{{ t('settings.motionSwitch') }}</span>
          <span class="item-desc">{{ t(mobile ? 'mobile.settingsMotionHint' : 'settings.motionSwitchDesc') }}</span>
        </div>
        <div class="item-control">
          <label class="switch cczj-cursor-pointer">
            <input :aria-label="t('settings.motionSwitch')" type="checkbox" :checked="motion.enabled"
              @change="motion.setEnabled(($event.target as HTMLInputElement).checked)" />
            <span class="slider" />
          </label>
        </div>
      </div>
    </section>

    <!-- ==================== 窗口 ==================== -->
    <section v-if="!mobile" id="basic-window" class="block basic-section" tabindex="-1" style="--section-order: 1">
      <header class="section-heading">
        <span class="section-icon"><Icon name="monitor" :size="18" /></span>
        <h3>{{ t('settings.secWindow') }}</h3>
        <span class="section-number">02</span>
      </header>

      <div class="item">
        <div class="item-text">
          <span class="item-name">{{ t('settings.windowSize') }}</span>
          <span class="item-desc">{{ t('settings.windowSizeDesc') }}</span>
        </div>
        <div class="item-control">
          <SelectDropdown :aria-label="t('settings.windowSize')" :model-value="windowSizeKey" :options="windowSizeOptions"
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
          <NumberField v-model="windowWidth" :ariaLabel="t('settings.windowWidth')" :min="MIN_WINDOW_WIDTH" :step="10" @change="applyWindowSize" />
          <span class="field-label">{{ t('settings.windowHeight') }}</span>
          <NumberField v-model="windowHeight" :ariaLabel="t('settings.windowHeight')" :min="MIN_WINDOW_HEIGHT" :step="10" @change="applyWindowSize" />
          <span class="unit">px</span>
        </div>
      </div>

      <div class="item">
        <div class="item-text">
          <span class="item-name">{{ t('settings.allowResize') }}</span>
        </div>
        <div class="item-control">
          <label class="switch cczj-cursor-pointer">
            <input :aria-label="t('settings.allowResize')" type="checkbox" v-model="windowResizable" @change="saveWindowResizable" />
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
            <input :aria-label="t('settings.closeBehavior')" type="checkbox" v-model="closeToTray" @change="saveCloseBehavior" />
            <span class="slider" />
          </label>
        </div>
      </div>
    </section>

    <!-- ==================== 首页 ==================== -->
    <section v-show="!mobile || mobileSection === 'home'" id="basic-home" class="block basic-section" tabindex="-1" style="--section-order: 2">
      <header class="section-heading">
        <span class="section-icon"><Icon name="home" :size="18" /></span>
        <h3>{{ t('settings.secHome') }}</h3>
        <span class="section-number">03</span>
      </header>

      <div v-if="!mobile" class="item">
        <div class="item-text">
          <span class="item-name">{{ t('settings.gridColumns') }}</span>
        </div>
        <div class="item-control cczj-inline-flex cczj-items-center">
          <input :aria-label="t('settings.gridColumns')" type="range" :value="layoutStore.columns" min="2" max="10"
            @input="layoutStore.previewColumns(Number(($event.target as HTMLInputElement).value))"
            @change="layoutStore.setColumns(layoutStore.columns)" />
          <span class="value">{{ t('settings.columnCount', { n: layoutStore.columns }) }}</span>
        </div>
      </div>

      <div v-if="!mobile" class="item">
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
          <span class="item-desc">{{ t(mobile ? 'mobile.homeCarouselHint' : 'settings.showCarouselDesc') }}</span>
        </div>
        <div class="item-control">
          <label class="switch cczj-cursor-pointer">
            <input :aria-label="t('settings.showCarousel')" type="checkbox" :checked="layoutStore.showCarousel"
              @change="layoutStore.setShowCarousel(($event.target as HTMLInputElement).checked)" />
            <span class="slider" />
          </label>
        </div>
      </div>

      <div class="item" v-if="layoutStore.showCarousel">
        <div class="item-text">
          <span class="item-name">{{ t('settings.carouselAutoPlay') }}</span>
          <span class="item-desc">{{ t(mobile ? 'mobile.homeAutoplayHint' : 'settings.carouselAutoPlayDesc') }}</span>
        </div>
        <div class="item-control">
          <label class="switch cczj-cursor-pointer">
            <input :aria-label="t('settings.carouselAutoPlay')" type="checkbox" v-model="carouselAutoPlay" />
            <span class="slider" />
          </label>
        </div>
      </div>

      <div class="item" v-if="layoutStore.showCarousel && carouselAutoPlay">
        <div class="item-text">
          <span class="item-name">{{ t('settings.carouselInterval') }}</span>
        </div>
        <div class="item-control cczj-inline-flex cczj-items-center">
          <NumberField :ariaLabel="t('settings.carouselInterval')" v-model="carouselSeconds" :min="3" :max="60" />
          <span class="unit">{{ t('common.unitSecond') }}</span>
        </div>
      </div>

      <div class="item">
        <div class="item-text">
          <span class="item-name">{{ t('settings.showRecommend') }}</span>
          <span class="item-desc">{{ t(mobile ? 'mobile.homeRecommendHint' : 'settings.showRecommendDesc') }}</span>
        </div>
        <div class="item-control">
          <label class="switch cczj-cursor-pointer">
            <input :aria-label="t('settings.showRecommend')" type="checkbox" :checked="layoutStore.showRecommend"
              @change="layoutStore.setShowRecommend(($event.target as HTMLInputElement).checked)" />
            <span class="slider" />
          </label>
        </div>
      </div>
    </section>

    <!-- ==================== 数据与网络 ==================== -->
    <section v-show="!mobile || mobileSection === 'data'" id="basic-data" class="block basic-section" tabindex="-1" style="--section-order: 3">
      <header class="section-heading">
        <span class="section-icon"><Icon name="database" :size="18" /></span>
        <h3>{{ t('settings.secData') }}</h3>
        <span class="section-number">04</span>
      </header>

      <SettingsDataPreferences @open-group="emit('open-group', $event)" />
    </section>

    <!-- ==================== 维护 ==================== -->
    <section v-show="!mobile || mobileSection === 'maintenance'" id="basic-maintenance" class="block basic-section" tabindex="-1" style="--section-order: 4">
      <header class="section-heading">
        <span class="section-icon"><Icon name="settings" :size="18" /></span>
        <h3>{{ t('settings.secMaintenance') }}</h3>
        <span class="section-number">05</span>
      </header>

      <div class="item">
        <div class="item-text">
          <span class="item-name">{{ t('settings.logRetention') }}</span>
          <span class="item-desc">{{ t('settings.logRetentionDesc') }}</span>
        </div>
        <div class="item-control">
          <SelectDropdown :aria-label="t('settings.logRetention')" :model-value="String(logKeepDays)" :options="logKeepOptions"
            @update:model-value="(v: any) => saveLogKeepDays(v)" />
        </div>
      </div>

      <div class="item" data-update-preferences>
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
    </fieldset>
  </div>
</template>

<style scoped>
.basic-settings { @apply cczj-min-w-0; container-type: inline-size; }
.section-links { @apply cczj-flex cczj-flex-wrap; gap: 7px; margin-bottom: 18px; }
.section-links button {
  @apply cczj-inline-flex cczj-items-center; gap: 7px; padding: 8px 11px;
  @apply cczj-border; border-radius: 7px; background: var(--bg-card);
  @apply cczj-text-secondary; font: inherit; font-size: .83rem; @apply cczj-cursor-pointer;
  transition: color var(--cczj-motion-fast), background-color var(--cczj-motion-fast), transform var(--cczj-motion-fast);
}
.section-links button:hover { background: var(--accent-alpha-10); @apply cczj-text-accent; }
.section-links button:active { transform: translateY(1px); }
.section-links button:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
.basic-fields { border: 0; margin: 0; padding: 0; @apply cczj-min-w-0; }
.basic-grid { @apply cczj-grid; grid-template-columns: minmax(0, 1fr); gap: 18px; align-items: start; }
.basic-section {
  @apply cczj-border; background: var(--bg-card); border-radius: 14px;
  padding: 20px 22px; @apply cczj-min-w-0; container-type: inline-size; scroll-margin-top: 90px;
  animation: settings-reveal var(--cczj-motion-normal) var(--cczj-motion-ease-standard) backwards;
  animation-delay: calc(var(--section-order) * var(--cczj-motion-stagger, 35ms));
}
.basic-section:focus-visible { outline: 2px solid var(--accent); outline-offset: 3px; }
.section-heading { @apply cczj-flex cczj-items-center cczj-gap-5; padding-bottom: 15px; @apply cczj-mb-2 cczj-border-bottom; }
.section-heading h3 { margin: 0; font-size: 1rem; @apply cczj-font-bold; }
.section-icon { @apply cczj-grid; place-items: center; @apply cczj-w-32 cczj-h-32; border-radius: 9px; background: var(--accent-alpha-10); @apply cczj-text-accent; }
.section-number { @apply cczj-ml-auto cczj-text-muted; font-size: .75rem; font-variant-numeric: tabular-nums; letter-spacing: .1em; }
@media (prefers-reduced-motion: reduce) { .basic-section { animation: none; } }
html[data-cczj-motion='off'] .basic-section { animation: none; }
@keyframes settings-reveal { from { opacity: 0; transform: translateY(8px); } to { opacity: 1; transform: translateY(0); } }
@container (min-width: 980px) { .basic-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
@container (max-width: 570px) {
  .basic-section { @apply cczj-p-8; }
  .item { @apply cczj-flex-wrap; gap: 9px; }
  .item-text { flex: 1 1 180px; }
  .item-control { @apply cczj-max-w-full; }
  .item-control :deep(.select-dropdown) { width: 180px; }
}
/* Each section owns its layout; the settings shell no longer flattens these cards. */
.block > h3 {
  font-size: 0.97rem;
  @apply cczj-font-bold;
  margin: 0 0 6px;
  letter-spacing: 0.3px;
}
.block-note {
  margin: 0 0 10px;
  font-size: 0.86rem;
  @apply cczj-text-muted cczj-leading-normal;
}

/* 一行一项：左边名称+说明，右边控件。行内不画分隔线，块与块之间已有发丝线。 */
.item {
  @apply cczj-flex cczj-items-center cczj-justify-between cczj-gap-10;
  padding: 10px 0;
}
.item-text {
  @apply cczj-flex cczj-flex-col cczj-gap-1 cczj-min-w-0;
}
.item-name {
  font-size: 0.95rem;
  @apply cczj-text-primary;
}
.item-desc {
  font-size: 0.83rem;
  @apply cczj-text-muted cczj-leading-normal;
}
.item-control {
  @apply cczj-flex-wrap cczj-justify-end;
  max-width: 55%;
  @apply cczj-flex cczj-items-center cczj-gap-4 cczj-flex-shrink-0;
}
.item-control :deep(.select-dropdown) {
  width: 190px;
}

.field-label {
  font-size: 0.86rem;
  @apply cczj-text-muted;
}
.unit {
  @apply cczj-text-muted;
  font-size: 0.86rem;
}
.num {
  width: 78px;
  padding: 6px 8px;
  @apply cczj-border cczj-rounded-md;
  background: var(--bg-input);
  @apply cczj-text-primary;
  font-family: inherit;
  font-size: 0.9rem;
  font-variant-numeric: tabular-nums;
  @apply cczj-text-right cczj-outline-none;
}
.num:focus {
  @apply cczj-border-primary;
}
.value {
  min-width: 58px;
  @apply cczj-font-semibold cczj-text-secondary;
  font-variant-numeric: tabular-nums;
}

.mini-btn {
  padding: 7px 14px;
  @apply cczj-border cczj-rounded-md;
  background: var(--bg-card);
  @apply cczj-text-primary;
  font-size: 0.88rem;
  @apply cczj-font-semibold;
  transition: background-color 0.15s ease, border-color 0.15s ease;
}
.mini-btn:hover:not(:disabled) {
  background: var(--bg-hover);
  @apply cczj-border-primary;
}
.mini-btn:disabled {
  @apply cczj-opacity-60 cczj-cursor-not-allowed;
}

.item input[type='range'] {
  width: 180px;
  accent-color: var(--accent);
}

/* 开关：滑块式，与设置页其余分组一致。 */
.switch {
  @apply cczj-relative cczj-inline-block;
  width: 40px;
  height: 22px;
  @apply cczj-flex-shrink-0;
}
.switch input {
  @apply cczj-absolute cczj-opacity-0 cczj-w-full cczj-h-full;
  margin: 0;
  @apply cczj-cursor-pointer;
}
.switch .slider {
  @apply cczj-absolute;
  inset: 0;
  border-radius: 999px;
  background: var(--bg-secondary);
  @apply cczj-border-strong;
  transition: background-color 0.18s ease, border-color 0.18s ease;
  @apply cczj-pointer-events-none;
}
.switch .slider::before {
  content: '';
  @apply cczj-absolute;
  top: 2px;
  left: 2px;
  @apply cczj-w-16 cczj-h-16 cczj-rounded-50;
  background: var(--text-secondary);
  transition: transform 0.18s ease, background-color 0.18s ease;
}
.switch input:checked + .slider {
  background: var(--accent);
  @apply cczj-border-primary;
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
