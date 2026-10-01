<script setup lang="ts">
/**
 * 「基本」分组：窗口、字体、语言、首页网格、关闭行为、豆瓣补全、数据新鲜度、日志保留。
 *
 * 分组本身还是 Settings.vue 的顶部 tab 之一，这里只接管那一组的内容与它自己的读写：
 * 列数与密度仍然直接绑 layout store（不在父级与子级之间传值），保持拖动即时预览的语义；
 * window_size_key / font_size_key / font_family / language 落库是即时的，其余控件走各自的
 * 即时写接口，和拆分前一模一样。
 */
defineOptions({ name: 'SettingsBasicGroup' })
import { ref, computed, onMounted } from 'vue'
import {
  GetSetting, SetSetting, GetCloseBehavior, SetCloseBehavior,
  WindowSetResizable, WindowGetResizable, WindowSetSize, WindowGetSize,
  GetDoubanIntervalMinutes, SetDoubanIntervalMinutes,
  GetLogKeepDays, SetLogKeepDays,
} from '../../api/app'
import { useErrorStore } from '../../stores/error'
import { useLayoutStore, type LayoutDensity } from '../../stores/layout'
import Icon from '../../components/Icon.vue'
import { Segment, Select as SelectDropdown } from '../../components/ui'
import { useI18n } from 'vue-i18n'
import { setLocale as saveLocalePreference } from '../../locales'

const { t } = useI18n()
const errorStore = useErrorStore()

// 通用设置：列数与密度存在 layout store 里，首页/搜索/收藏跟着同一份状态走，
// 改完立刻生效，不用等它们重挂。
const layoutStore = useLayoutStore()


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

// 关闭行为（「重启」那枚按钮在「关于」分组里）
const closeToTray = ref(false)

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

// ---------- 豆瓣补全轮询 ----------
const doubanIntervalMinutes = ref(30)
const doubanIntervalOptions = [10, 15, 20, 30, 45, 60, 90, 120].map(m => ({
  value: m,
  label: t('settings.doubanEvery', { n: m }),
}))

async function loadDoubanInterval(): Promise<void> {
  try { doubanIntervalMinutes.value = await GetDoubanIntervalMinutes() } catch { /* 忽略 */ }
}

async function saveDoubanInterval(minutes: number): Promise<void> {
  try {
    doubanIntervalMinutes.value = await SetDoubanIntervalMinutes(Number(minutes) || 30)
    errorStore.info(t('common.saved'), t('settings.doubanIntervalSaved', { n: doubanIntervalMinutes.value }), '', 'Settings.saveDoubanInterval')
  } catch (e: any) {
    errorStore.fromError(t('settings.doubanIntervalSaveFailed'), e, 'Settings.saveDoubanInterval')
  }
}

// ---------- 数据新鲜度 ----------
// 只决定「多旧的详情还值得直接用、多久在后台补一次新的」，不决定界面等不等：
// 过期条目一律先回旧数据。所以调快不等于更卡，调慢也不等于省流量。
const freshness = ref<'fast' | 'standard' | 'saver'>('standard')
// 用 computed 而不是模块级常量：后者会把 t() 在初始化时求值一次，切语言后标签不再变。
const freshnessOptions = computed(() => [
  { value: 'fast', label: t('settings.freshnessFast') },
  { value: 'standard', label: t('settings.freshnessStandard') },
  { value: 'saver', label: t('settings.freshnessSaver') },
])

async function loadFreshness(): Promise<void> {
  const raw = await safeGet('detail_cache_freshness', 'standard')
  freshness.value = raw === 'fast' || raw === 'saver' ? raw : 'standard'
}

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

const logKeepDays = ref(30)
const logKeepOptions = [7, 14, 30, 60, 90].map(d => ({ value: d, label: t('settings.logKeepDays', { n: d }) }))

async function loadLogKeepDays(): Promise<void> {
  try { logKeepDays.value = await GetLogKeepDays() } catch { /* 忽略 */ }
}

async function saveLogKeepDays(days: number): Promise<void> {
  try {
    logKeepDays.value = await SetLogKeepDays(Number(days) || 30)
    errorStore.info(t('common.saved'), t('settings.logRetentionSaved', { n: logKeepDays.value }), '', 'Settings.saveLogKeepDays')
  } catch (e: any) {
    errorStore.fromError(t('settings.logRetentionSaveFailed'), e, 'Settings.saveLogKeepDays')
  }
}

// ---------- 启动：和拆分前设置页 onMounted 里这几行的顺序、次数一致 ----------
onMounted(async () => {
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

  // 加载保留策略（豆瓣补全 / 日志保留）
  await loadDoubanInterval()
  await loadLogKeepDays()
  await loadFreshness()
})

async function safeGet(key: string, fallback: string): Promise<string> {
  try { const v = await GetSetting(key); return v || fallback } catch { return fallback }
}
async function save(key: string, val: string | number | boolean): Promise<void> {
  try { await SetSetting(key, String(val)) } catch { /* 忽略 */ }
}
</script>

<template>
  <div class="panel group-card cczj-flex cczj-flex-col cczj-gap-2">

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
        <input
          type="range"
          :value="layoutStore.columns"
          min="2"
          max="10"
          @input="layoutStore.previewColumns(Number(($event.target as HTMLInputElement).value))"
          @change="layoutStore.setColumns(layoutStore.columns)"
        />
        <span class="value">{{ t('settings.columnCount', { n: layoutStore.columns }) }}</span>
      </div>
    </section>

    <!-- 卡片密度 -->
    <section class="block">
      <h3>{{ t('settings.cardDensity') }}</h3>
      <div class="row cczj-flex cczj-items-center cczj-gap-7">
        <Segment
          :model-value="layoutStore.density"
          :options="[{ value: 'comfortable', label: t('settings.comfortable') }, { value: 'compact', label: t('settings.compact') }, { value: 'spacious', label: t('settings.spacious') }]"
          @update:model-value="(v: any) => layoutStore.setDensity(v as LayoutDensity)"
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

    <!-- 豆瓣补全轮询 -->
    <section class="block">
      <h3>{{ t('settings.doubanPolling') }}</h3>
      <p class="desc">{{ t('settings.doubanPollingDesc') }}</p>
      <div class="row cczj-flex cczj-items-center cczj-gap-7 cczj-flex-wrap">
        <Segment
          :model-value="doubanIntervalMinutes"
          :options="doubanIntervalOptions"
          @update:model-value="(v: any) => saveDoubanInterval(Number(v))"
        />
      </div>
    </section>

    <!-- 数据新鲜度 -->
    <section class="block">
      <h3>{{ t('settings.freshness') }}</h3>
      <p class="desc">{{ t('settings.freshnessDesc') }}</p>
      <div class="row cczj-flex cczj-items-center cczj-gap-7 cczj-flex-wrap">
        <Segment
          :model-value="freshness"
          :options="freshnessOptions"
          @update:model-value="(v: any) => saveFreshness(String(v))"
        />
      </div>
    </section>

    <!-- 日志保留 -->
    <section class="block">
      <h3>{{ t('settings.logRetention') }}</h3>
      <p class="desc">{{ t('settings.logRetentionDesc') }}</p>
      <div class="row cczj-flex cczj-items-center cczj-gap-7 cczj-flex-wrap">
        <Segment
          :model-value="logKeepDays"
          :options="logKeepOptions"
          @update:model-value="(v: any) => saveLogKeepDays(Number(v))"
        />
      </div>
    </section>

  </div>
</template>

<style scoped>
/* 以下三组是分组共用的控件样式：卡片外框与 .block 的压平仍由 Settings.vue 的
   .panel.group-card 负责（:deep 能落到本组件根节点以内的元素），但 h3 这类内部
   元素拿不到父级 scope id，所以标题排版留在用到它的那一组里（同 DataBackupPanel）。 */
.block h3 {
  font-size: 0.97rem;
  font-weight: 700;
  margin: 0 0 12px;
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

.row {
  gap: 14px;
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

.desc {
  color: var(--text-muted);
  font-size: 0.93rem;
  margin: 0 0 12px 0;
}
</style>
