/**
 * 注入型扩展包（script 段）在运行时留下的痕迹：侧边栏条目、追加的路由、注入失败的摘要。
 *
 * 这里只有「展示用的登记簿」，没有加载逻辑——加载在 runtime.ts，撤销动作由每个包自己的
 * dispose 闭包负责（api.ts 里登记），登记簿只保证界面上看到的东西和实际生效的东西一致。
 */
import { ref, type Component } from 'vue'

export interface PluginNavItem {
  /** 归属包 id：重扫或禁用时要能只收回这一个包加的那一项。 */
  packId: string
  path: string
  /**
   * 文字。给函数是为了跟随语言切换：侧边栏在 computed 里读它，包可以在函数里调
   * `cczj.i18n.t`，于是切换语言时这一行会跟着重算，而不是冻在注册那一刻的中文上。
   */
  label: string | (() => string)
  icon: string
}

export interface PluginRoute {
  packId: string
  /** 稳定的名字，router.removeRoute 只认名字。 */
  name: string
  path: string
  component: Component
}

/** 扩展包往设置页追加的那个分组（日志、诊断这类「不是人人要看」的面板走这里）。 */
export interface PluginSettingsTab {
  packId: string
  /** 同时是 `#hash` 深链用的分组 id，所以必须是小写稳定串。 */
  id: string
  label: string | (() => string)
  icon: string
  component: Component
  /**
   * 排在设置页分组条里的哪个位置。内置分组占 10/20/30…60/70（extensions 之后、
   * advanced 之前的 40、50 正好是日志和诊断原来的位置），所以不写这一项的包
   * 落在 0，会排在最前面——想插队就自己给一个。
   */
  order: number
}

/** 侧边栏读取的导航；包没注入进来时它就是空数组，界面不需要特判。 */
export const pluginNavItems = ref<PluginNavItem[]>([])

/** 本轮注入成功、已挂到 router 上的页面。 */
export const pluginRoutes = ref<PluginRoute[]>([])

export function routeNameFor(packId: string, path: string): string {
  return `plugin:${packId}:${path}`
}

export function addPluginNav(item: PluginNavItem): void {
  pluginNavItems.value = [...pluginNavItems.value.filter(n => n.path !== item.path), item]
}

export function removePluginNav(packId: string, path: string): void {
  pluginNavItems.value = pluginNavItems.value.filter(n => !(n.packId === packId && n.path === path))
}

export function addPluginRoute(route: PluginRoute): void {
  pluginRoutes.value = [...pluginRoutes.value, route]
}

export function removePluginRoute(name: string): void {
  pluginRoutes.value = pluginRoutes.value.filter(r => r.name !== name)
}

export function dropPackRoutes(packId: string): void {
  pluginRoutes.value = pluginRoutes.value.filter(r => r.packId !== packId)
}

export function dropPackNav(packId: string): void {
  pluginNavItems.value = pluginNavItems.value.filter(n => n.packId !== packId)
}

/** 设置页读取的扩展包分组，排在内置分组后面。 */
export const pluginSettingsTabs = ref<PluginSettingsTab[]>([])

/**
 * 内置分组占用的 id，扩展包不能拿（和 assertNewPath 不许覆盖内置页面同一个道理：
 * 一个拼错的 id 就把应用自己的设置面板换掉了）。Settings.vue 的 GROUPS 必须与这份
 * 清单保持一致，改那边记得改这里。
 */
export const RESERVED_SETTINGS_IDS = new Set(['basic', 'theme', 'extensions', 'advanced', 'about'])

export function addPluginSettingsTab(tab: PluginSettingsTab): void {
  // 按 id 全局去重：两个包都登记 logs 时应当是后者顶掉前者，而不是 v-for 撞出两个同名 tab。
  const rest = pluginSettingsTabs.value.filter(t => t.id !== tab.id)
  // 分组条按 order 排：内置分组占了 10/20/30 与 60/70，所以内置的日志（40）、
  // 诊断（50）落回原来的位置，新加的包不写 order 就排在中间偏后。
  pluginSettingsTabs.value = [...rest, tab].sort((a, b) => a.order - b.order)
}

export function removePluginSettingsTab(packId: string, id: string): void {
  pluginSettingsTabs.value = pluginSettingsTabs.value.filter(t => !(t.packId === packId && t.id === id))
}

export function dropPackSettingsTabs(packId: string): void {
  pluginSettingsTabs.value = pluginSettingsTabs.value.filter(t => t.packId !== packId)
}
