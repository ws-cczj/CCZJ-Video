<script setup lang="ts">
defineOptions({ name: 'Settings' })
import { ref, onMounted, computed, watch, nextTick } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { isMobileRuntime } from '../api/runtime'
import { GetAppVersion } from '../api/app'
import { useThemeStore } from '../stores/theme'
import { useDownloadStore } from '../stores/download'
import { useMotionStore } from '../stores/motion'
import { useLayoutStore } from '../stores/layout'
import Icon from '../components/Icon.vue'
import CczjMobileTools from '../mobile/CczjMobileTools.vue'
import ExtensionsPanel from '../components/ExtensionsPanel.vue'
import { pluginSettingsTabs } from '../plugins/registry'
import { MotionList } from '../components/ui'
import { useI18n } from 'vue-i18n'
import SettingsBasicGroup from './settings/SettingsBasicGroup.vue'
import SettingsThemeGroup from './settings/SettingsThemeGroup.vue'
import SettingsAdvancedGroup from './settings/SettingsAdvancedGroup.vue'
import SettingsAboutGroup from './settings/SettingsAboutGroup.vue'
import { useHorizontalDrag } from '../composables/useHorizontalDrag'
const groupDrag = useHorizontalDrag()

const { t } = useI18n()

// ---------- 分组配置（顶部 tab） ----------
interface GroupItem {
  id: string
  label: string
  icon: string
  /** 与 registry.ts 的 PluginSettingsTab.order 同一套刻度，扩展包据此插在内置分组之间。 */
  order: number
}
/**
 * 内置分组。`logs` 与 `diagnostics` 不在这里 —— 它们由内置扩展包 logs-panel /
 * diagnostics-panel 通过 `cczj.settingsTab` 登记（order 40、50 正好回到原来的位置），
 * 所以删掉那两个文件夹，设置页就真的少了那两项。
 * RESERVED_SETTINGS_IDS 列的就是下面这些 id，两处要一起改。
 * 各组的内容本体在 views/settings/ 下按分组名一个文件，这里只留分组条、面板外壳与卡片样式。
 */
const GROUPS = computed<GroupItem[]>(() => [
  { id: 'basic',  label: t('settings.basic'), icon: 'sliders', order: 10 },
  { id: 'theme',  label: t('settings.theme'), icon: 'palette', order: 20 },
  { id: 'extensions', label: t('settings.extensions'), icon: 'layers', order: 30 },
  { id: 'advanced', label: t('advanced.title'), icon: 'shield', order: 60 },
  { id: 'about',  label: t('settings.about'), icon: 'info', order: 70 },
  ...pluginSettingsTabs.value.map(tab => ({
    id: tab.id,
    // label 是函数时才跟随语言切换：这里在 computed 里调用，切换语言会重算这一行。
    label: typeof tab.label === 'function' ? tab.label() : tab.label,
    icon: tab.icon,
    order: tab.order,
  })).sort((a, b) => a.order - b.order),
].sort((a, b) => a.order - b.order))

const themeStore = useThemeStore()
const downloadStore = useDownloadStore()
const route = useRoute()
const router = useRouter()
const mobile = isMobileRuntime()
const mobileGroup = computed(() => mobile && typeof route.query.group === 'string' && GROUPS.value.some(g => g.id === route.query.group))

const mobileCategories = computed(() => [
  { title: t('mobile.settingsPersonal'), ids: ['basic', 'theme', 'motion'] },
  { title: t('mobile.settingsTools'), ids: ['extensions', 'advanced', 'logs', 'diagnostics'] },
  { title: t('mobile.settingsApplication'), ids: ['about'] },
].map(category => ({ title: category.title, groups: GROUPS.value.filter(g => category.ids.includes(g.id) || (category.ids.includes('extensions') && !['basic','theme','motion','extensions','advanced','logs','diagnostics','about'].includes(g.id))) })))
function groupDescription(id: string): string {
  const keys: Record<string,string> = { basic: 'mobile.settingsBasicHint', theme: 'mobile.settingsThemeHint', motion: 'mobile.settingsMotionHint', extensions: 'mobile.settingsExtensionsHint', advanced: 'mobile.settingsAdvancedHint', logs: 'mobile.settingsLogsHint', diagnostics: 'mobile.settingsDiagnosticsHint', about: 'mobile.settingsAboutHint' }
  return keys[id] ? t(keys[id]) : ''
}

const activeGroup = ref<string>('basic')
const settingsRoot = ref<HTMLElement | null>(null)
const motion = useMotionStore()
function openGroup(id: string): void {
  if (!GROUPS.value.some(group => group.id === id)) return
  if (mobile) { void router.push({ path: '/settings', query: { group: id } });return }
  activeGroup.value = id
}
watch(() => route.query.group, id => {
  if (mobile && typeof id === 'string' && GROUPS.value.some(g => g.id === id)) activeGroup.value = id
}, { immediate: true })
function scrollGroups(direction: number): void {
  const nav = settingsRoot.value?.querySelector<HTMLElement>('.tabs')
  if (!nav) return
  nav.scrollBy({ left: direction * nav.clientWidth * .75,
    behavior: motion.enabled && !matchMedia('(prefers-reduced-motion: reduce)').matches ? 'smooth' : 'auto' })
}

// Keyboard navigation follows the same live list as plugin registration.
async function navigateGroup(event: KeyboardEvent): Promise<void> {
  const keys = ['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown', 'Home', 'End']
  if (!keys.includes(event.key)) return
  event.preventDefault()
  const groups = GROUPS.value
  const current = groups.findIndex(g => g.id === activeGroup.value)
  const next = event.key === 'Home' ? 0 : event.key === 'End' ? groups.length - 1
    : (current + (['ArrowLeft', 'ArrowUp'].includes(event.key) ? -1 : 1) + groups.length) % groups.length
  activeGroup.value = groups[next].id
  const nav = (event.currentTarget as HTMLElement).parentElement
  await nextTick()
  const button = Array.from(nav?.querySelectorAll<HTMLButtonElement>('[data-group]') || []).find(el => el.dataset.group === groups[next].id)
  button?.focus({ preventScroll: true })
  button?.scrollIntoView({ block: 'nearest', inline: 'nearest' })
}

// 分组挂载过一次之后就留着，切走只是 v-show。拆分前所有状态都挂在页面级，切 tab 什么都
// 不会丢；拆成子组件后若继续用 v-if，「主题编辑器改了一半切去看日志再切回来」会连草稿
// 和打开着的弹窗一起消失，各分组 onMounted 里的取数也会从「每次进页面一遍」变成
// 「每次切 tab 一遍」。
const mountedGroups = ref<Record<string, boolean>>({})
watch([activeGroup, mobileGroup], ([id, visible], [previous]) => {
  if (id && (!mobile || visible)) mountedGroups.value[id] = true
  if (previous) settingsRoot.value?.closest('.main-content')?.scrollTo({ top: 0, behavior: 'auto' })
}, { immediate: true })

// 停在扩展包分组上时把它删掉并重扫，分组条会少一项、面板却还指着那个 id——于是整块内容空白。
watch(GROUPS, (groups) => {
  if (!groups.some((g) => g.id === activeGroup.value)) activeGroup.value = 'basic'
})

// 通用设置：列数与密度存在 layout store 里，首页/搜索/收藏跟着同一份状态走，
// 改完立刻生效，不用等它们重挂。滑块与密度段在「基本」分组里直接读这份 store，
// 这里只负责进页面先把它装载好。
const layoutStore = useLayoutStore()

// ---------- 版本信息 ----------
// 版本号是页面级元信息，不属于某一组的选项：进页面取一次，再交给「关于」分组显示。
const appVersion = ref('1.1.0')

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

  await layoutStore.load()
})
</script>

<template>
  <div ref="settingsRoot" class="settings-page" :class="{ 'is-diagnostics': activeGroup === 'diagnostics', 'cczj-mobile-settings': mobile }">
    <header v-if="!mobile" class="page-head">
      <div class="page-kicker">CCZJ VIDEO</div>
      <h1>{{ t('sidebar.settings') }}</h1>
      <p class="page-desc">{{ t('settings.pageDescription') }}</p>
    </header>
    <!-- 顶部分组切换 tab。扩展包的分组会随启停增删，所以这一条用 list 过渡：
         新分组淡入落位，右边的分组平滑让位，而不是整条忽然横移一格。 -->
    <div v-if="mobile && !mobileGroup" class="cczj-mobile-settings-index">
      <p class="cczj-mobile-intro">{{ t('mobile.settingsDescription') }}</p>
      <template v-for="(category,index) in mobileCategories" :key="category.title"><section class="cczj-mobile-settings-category">
        <h2>{{ category.title }}</h2>
        <nav class="cczj-mobile-menu" :aria-label="category.title">
          <button v-for="g in category.groups" :key="g.id" :data-group="g.id" class="cczj-mobile-menu-row" @click="openGroup(g.id)">
            <Icon :name="g.icon" :size="20" />
            <span class="cczj-mobile-settings-copy"><strong>{{ g.label }}</strong><small>{{ groupDescription(g.id) }}</small></span>
            <Icon name="chevron-right" :size="17" />
          </button>
        </nav>
      </section><CczjMobileTools v-if="index===0" /></template>
    </div>
    <div v-if="!mobile" class="settings-navigation">
      <button class="nav-scroll" :aria-label="t('settings.previousGroups')" @click="scrollGroups(-1)"><Icon name="chevron-left" :size="16" /></button>
    <MotionList preset="list" tag="nav" class="tabs" :aria-label="t('sidebar.settings')" role="tablist" @pointerdown="groupDrag.down" @pointermove="groupDrag.move" @pointerup="groupDrag.end" @pointercancel="groupDrag.end" @click.capture="groupDrag.click">
      <button
        v-for="g in GROUPS"
        :key="g.id"
        class="tab cczj-inline-flex cczj-items-center cczj-gap-4 cczj-cursor-pointer"
        :class="{ active: activeGroup === g.id }"
        :title="g.label" :data-group="g.id" :id="`settings-tab-${g.id}`" role="tab"
        :aria-selected="activeGroup === g.id" aria-controls="settings-content"
        :tabindex="activeGroup === g.id ? 0 : -1"
        @keydown="navigateGroup"
        @click="activeGroup = g.id"
      >
        <Icon :name="g.icon" :size="14" />
        <span>{{ g.label }}</span>
      </button>
    </MotionList>
      <button class="nav-scroll" :aria-label="t('settings.nextGroups')" @click="scrollGroups(1)"><Icon name="chevron-right" :size="16" /></button>
    </div>

    <div id="settings-content" v-show="!mobile || mobileGroup" class="content" role="tabpanel" :aria-labelledby="mobile ? undefined : `settings-tab-${activeGroup}`" :aria-label="mobile ? GROUPS.find(g => g.id === activeGroup)?.label : undefined">
      <!-- ========== 基本设置 ========== -->
      <div v-if="mountedGroups.basic" v-show="activeGroup === 'basic'" :class="{ 'settings-group-active': activeGroup === 'basic' }" data-settings-group="basic"><SettingsBasicGroup @open-group="openGroup" /></div>

      <!-- ========== 主题外观 ========== -->
      <div v-if="mountedGroups.theme" v-show="activeGroup === 'theme'" :class="{ 'settings-group-active': activeGroup === 'theme' }" data-settings-group="theme"><SettingsThemeGroup /></div>

      <!-- ========== 扩展包 ========== -->
      <div v-if="mountedGroups.extensions" v-show="activeGroup === 'extensions'" class="panel cczj-flex cczj-flex-col cczj-gap-2" :class="{ 'settings-group-active': activeGroup === 'extensions' }">
        <ExtensionsPanel />
      </div>

      <!-- ========== 高级 ========== -->
      <div v-if="mountedGroups.advanced" v-show="activeGroup === 'advanced'" :class="{ 'settings-group-active': activeGroup === 'advanced' }" data-settings-group="advanced"><SettingsAdvancedGroup /></div>

      <!-- ========== 关于 ========== -->
      <div v-if="mountedGroups.about" v-show="activeGroup === 'about'" :class="{ 'settings-group-active': activeGroup === 'about' }" data-settings-group="about"><SettingsAboutGroup :app-version="appVersion" /></div>

      <!-- ========== 扩展包加进来的分组（内置的日志、诊断走的就是这条路） ==========
           逐个分组各自挂一份，而不是只渲染「当前那个」：component :is 会跟着
           activeGroup 换，留下的实例就会显示成别的分组的内容。 -->
      <template v-for="tab in pluginSettingsTabs" :key="tab.id">
        <div v-if="mountedGroups[tab.id]" v-show="activeGroup === tab.id" class="panel cczj-flex cczj-flex-col cczj-gap-2" :class="{ 'settings-group-active': activeGroup === tab.id }">
          <component :is="tab.component" :active="activeGroup === tab.id && (!mobile || mobileGroup)" />
        </div>
      </template>
    </div>
  </div>
</template>

<style scoped src="../styles/views/settings.css"></style>
