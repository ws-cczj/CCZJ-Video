<script setup lang="ts">
defineOptions({ name: 'Settings' })
import { ref, onMounted, computed, watch } from 'vue'
import { useRoute } from 'vue-router'
import { GetAppVersion } from '../api/app'
import { useThemeStore } from '../stores/theme'
import { useDownloadStore } from '../stores/download'
import { useLayoutStore } from '../stores/layout'
import Icon from '../components/Icon.vue'
import ExtensionsPanel from '../components/ExtensionsPanel.vue'
import { pluginSettingsTabs } from '../plugins/registry'
import { MotionList } from '../components/ui'
import { useI18n } from 'vue-i18n'
import SettingsBasicGroup from './settings/SettingsBasicGroup.vue'
import SettingsThemeGroup from './settings/SettingsThemeGroup.vue'
import SettingsAdvancedGroup from './settings/SettingsAdvancedGroup.vue'
import SettingsAboutGroup from './settings/SettingsAboutGroup.vue'

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

const activeGroup = ref<string>('basic')

// 分组挂载过一次之后就留着，切走只是 v-show。拆分前所有状态都挂在页面级，切 tab 什么都
// 不会丢；拆成子组件后若继续用 v-if，「主题编辑器改了一半切去看日志再切回来」会连草稿
// 和打开着的弹窗一起消失，各分组 onMounted 里的取数也会从「每次进页面一遍」变成
// 「每次切 tab 一遍」。
const mountedGroups = ref<Record<string, boolean>>({})
watch(activeGroup, (id) => {
  if (id) mountedGroups.value[id] = true
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
  <div class="settings-page">
    <!-- 顶部分组切换 tab。扩展包的分组会随启停增删，所以这一条用 list 过渡：
         新分组淡入落位，右边的分组平滑让位，而不是整条忽然横移一格。 -->
    <MotionList preset="list" tag="div" class="tabs cczj-flex">
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
    </MotionList>

    <div class="content">
      <!-- ========== 基本设置 ========== -->
      <SettingsBasicGroup v-if="mountedGroups.basic" v-show="activeGroup === 'basic'" />

      <!-- ========== 主题外观 ========== -->
      <SettingsThemeGroup v-if="mountedGroups.theme" v-show="activeGroup === 'theme'" />

      <!-- ========== 扩展包 ========== -->
      <div v-if="mountedGroups.extensions" v-show="activeGroup === 'extensions'" class="panel cczj-flex cczj-flex-col cczj-gap-2">
        <ExtensionsPanel />
      </div>

      <!-- ========== 高级 ========== -->
      <SettingsAdvancedGroup v-if="mountedGroups.advanced" v-show="activeGroup === 'advanced'" />

      <!-- ========== 关于 ========== -->
      <SettingsAboutGroup v-if="mountedGroups.about" v-show="activeGroup === 'about'" :app-version="appVersion" />

      <!-- ========== 扩展包加进来的分组（内置的日志、诊断走的就是这条路） ==========
           逐个分组各自挂一份，而不是只渲染「当前那个」：component :is 会跟着
           activeGroup 换，留下的实例就会显示成别的分组的内容。 -->
      <template v-for="tab in pluginSettingsTabs" :key="tab.id">
        <div v-if="mountedGroups[tab.id]" v-show="activeGroup === tab.id" class="panel cczj-flex cczj-flex-col cczj-gap-2">
          <component :is="tab.component" />
        </div>
      </template>
    </div>
  </div>
</template>

<style scoped src="../styles/views/settings.css"></style>
