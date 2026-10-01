/**
 * 交给注入型扩展包的那一份 `cczj`。
 *
 * 设计约束只有一条：包代码是 blob 里的 ES 模块，浏览器不给它解析任何 import
 * （相对路径、裸模块名都不行），所以它需要的一切——Vue、路由、store、绑定、
 * localStorage——都必须由这里递到手上当作参数。宁可多给几个入口，也不要让
 * 作者为了拿一个 `ref` 就得等应用发版。
 *
 * 反过来说，这也意味着包能碰到的只有「应用已经装在运行时里的东西」：
 * 没有 Vue 模板编译器（组件只能写 h() 渲染函数），没有 Node，没有文件系统。
 */
import {
  computed,
  defineComponent,
  h,
  markRaw,
  nextTick,
  onBeforeUnmount,
  onMounted,
  reactive,
  ref,
  shallowRef,
  toRaw,
  watch,
  watchEffect,
  type Component,
} from 'vue'
import type { Router } from 'vue-router'
import * as bindings from '../api/app'
import { WriteLog } from '../api/app'
import { onBackendEvent } from '../api/events'
import i18n, { type Locale } from '../locales'
import DiagnosticsPanel from '../components/DiagnosticsPanel.vue'
import LogPanel from '../components/LogPanel.vue'
import MotionPanel from '../components/MotionPanel.vue'
import router from '../router'
import { readStorage, removeStorage, writeStorage } from '../platform/storage'
import { guardBindings, registerPack } from './permissions'
import { useCollectStore } from '../stores/collect'
import { useConfirmStore } from '../stores/confirm'
import { useDownloadStore } from '../stores/download'
import { useErrorStore } from '../stores/error'
import { useLogsStore } from '../stores/logs'
import { usePluginStore, type PluginInfo } from '../stores/plugins'
import { useSourceStore } from '../stores/source'
import { useThemeStore } from '../stores/theme'
import { useVideoStore } from '../stores/video'
import {
  addPluginNav,
  addPluginRoute,
  addPluginSettingsTab,
  dropPackNav,
  dropPackRoutes,
  dropPackSettingsTabs,
  removePluginNav,
  removePluginRoute,
  removePluginSettingsTab,
  RESERVED_SETTINGS_IDS,
  routeNameFor,
  type PluginSettingsTab,
} from './registry'

/** 包作者拿到的接口；runtime.ts 在 import 完模块后把它作为唯一参数传进 setup。 */
export interface CczjApi {
  /** 这个包自己的 manifest 摘要，只读。 */
  readonly pack: {
    id: string
    kind: string
    version: string
    name: Record<string, string>
    dir: string
    /** manifest.icon 校验通过后的包内相对路径，空串表示没给。 */
    icon: string
  }

  /**
   * 全部 Wails 绑定（应用后端的能力）加上 normalizeApiError。
   *
   * 这不是原样递过去的命名空间：读数据的那批（Get/List/Search…）直通，其余算「写」，
   * 每次调用先过授权闸门（plugins/permissions.ts）——包没在 manifest 里声明 `write`
   * 就当场抛错，声明了则第一次用的时候弹窗问用户一次。返回值仍是 Promise，所以
   * 包的写法一个字都不用改。
   */
  readonly bindings: typeof bindings
  /** 路由实例：可以 push/replace，也可以读 currentRoute。 */
  readonly router: Router
  /** Vue 的运行时零件。包代码没有 import 能力，组件就靠这些写。 */
  readonly vue: {
    h: typeof h
    ref: typeof ref
    shallowRef: typeof shallowRef
    reactive: typeof reactive
    computed: typeof computed
    watch: typeof watch
    watchEffect: typeof watchEffect
    nextTick: typeof nextTick
    markRaw: typeof markRaw
    toRaw: typeof toRaw
    defineComponent: typeof defineComponent
    onMounted: typeof onMounted
    onBeforeUnmount: typeof onBeforeUnmount
  }
  /** 应用的 pinia store。访问时才实例化，且拿到的是同一个单例。 */
  readonly stores: {
    readonly video: ReturnType<typeof useVideoStore>
    readonly source: ReturnType<typeof useSourceStore>
    readonly theme: ReturnType<typeof useThemeStore>
    readonly plugins: ReturnType<typeof usePluginStore>
    readonly logs: ReturnType<typeof useLogsStore>
    readonly download: ReturnType<typeof useDownloadStore>
    readonly collect: ReturnType<typeof useCollectStore>
    readonly confirm: ReturnType<typeof useConfirmStore>
    readonly error: ReturnType<typeof useErrorStore>
  }
  readonly i18n: {
    t(key: string, named?: Record<string, unknown>): string
    /** 往应用的语言包里并进的键；建议一律挂在 `pluginPacks.<packId>.*` 下，别撞内置键。 */
    merge(messages: Partial<Record<Locale, Record<string, unknown>>>): void
    /** 当前语言。读它时会建立响应式依赖，语言切换后包里的 computed 会重算。 */
    readonly locale: Locale
  }
  readonly events: {
    /** 订阅后端事件（'collect:done'、'app:log'…）；包卸载时自动退订。 */
    on<T = unknown>(name: string, handler: (payload: T) => void): () => void
  }
  /** 按包的命名空间存东西，落在 localStorage，卸载包时不清（用户的偏好不该跟着包一起删）。 */
  readonly storage: {
    get<T>(key: string, fallback: T): T
    set<T>(key: string, value: T): void
    remove(key: string): void
  }
  /** 写进应用的日志时间线（设置页 → 日志），来源标成包 id。 */
  readonly log: {
    info(message: string, detail?: string): void
    warn(message: string, detail?: string): void
    error(message: string, detail?: string): void
  }

  /** 在侧边栏「管理」区加一项；label 给函数才能跟随语言切换。返回撤销函数。 */
  nav(item: { path: string; label: string | (() => string); icon?: string }): () => void
  /**
   * 往设置页顶部的分组条加一项：component 可以是自己用 `cczj.vue.h` 写的渲染函数，
   * 也可以直接复用 `cczj.components` 里现成的面板。
   *
   * id 不能撞内置分组（basic/theme/extensions/advanced/about），那是「动内核」；
   * order 决定它插在哪个位置，默认 50 即「扩展包之后、高级之前」。返回撤销函数。
   */
  settingsTab(item: {
    id: string
    label: string | (() => string)
    component: Component
    icon?: string
    order?: number
  }): () => void
  /**
   * 应用自己的面板组件。内置的「日志」「诊断」「动画」三个分组就是靠它把自己变成扩展包的：
   * 界面在应用里，开关在包目录里——删掉那个文件夹，设置页就真的少了那一项。
   */
  readonly components: typeof packComponents
  /** 追加一条路由。组件必须是渲染函数写法（应用里没有模板编译器）。 */
  route(path: string, component: Component): () => void
  /** 注入一段全局样式；返回撤销函数。样式是全局的，请自己加包名作用域。 */
  css(text: string): () => void
  /**
   * 包住一个已有对象上的方法（store 的 action、普通对象上的函数…）。
   * wrapper 拿到 `next` 调用原实现，也可以完全不调它——这就是改行为的入口。
   * 注意 `cczj.bindings` 是 ES module 命名空间对象，只读，包住它既改不动也不影响
   * 应用自己的调用点：要改后端能力的行为，包 store 的 action 或直接自己调绑定。
   * 返回撤销函数，包卸载时自动还原。
   */
  intercept(target: object, method: string, wrapper: Interceptor): () => void
  /** 注册一段卸载时要跑的东西（定时器、外部连接、自己加的 DOM 节点）。 */
  onDispose(cleanup: () => void): void
}

export type Interceptor = (next: (...args: unknown[]) => unknown, ...args: unknown[]) => unknown

export interface PackRuntime {
  cczj: CczjApi
  /** 收回这个包做过的每一件事：样式、路由、导航、补丁、订阅。 */
  dispose(): void
}

const vueRuntime = {
  h,
  ref,
  shallowRef,
  reactive,
  computed,
  watch,
  watchEffect,
  nextTick,
  markRaw,
  toRaw,
  defineComponent,
  onMounted,
  onBeforeUnmount,
}

/**
 * 交给扩展包的现成面板。markRaw 的理由和 route() 里那句一样：登记簿是个 ref，
 * 深层响应化会把组件定义包成 proxy，Vue 挂载时就会报「received a Component
 * that was made a reactive object」。
 */
const packComponents = {
  LogPanel: markRaw(LogPanel),
  DiagnosticsPanel: markRaw(DiagnosticsPanel),
  MotionPanel: markRaw(MotionPanel),
}

/** pinia store 用 getter 现取：包多半只用其中一两个，没必要为它把九个 store 都实例化。 */
function createStores() {
  return {
    get video() { return useVideoStore() },
    get source() { return useSourceStore() },
    get theme() { return useThemeStore() },
    get plugins() { return usePluginStore() },
    get logs() { return useLogsStore() },
    get download() { return useDownloadStore() },
    get collect() { return useCollectStore() },
    get confirm() { return useConfirmStore() },
    get error() { return useErrorStore() },
  }
}

export function createPackRuntime(pack: PluginInfo): PackRuntime {
  const packId = pack.id
  /** 授权弹窗要报名字，而这一刻还没有 store 可用，所以按当前语言从 manifest 里挑一条。 */
  const locale = i18n.global.locale.value as Locale
  const displayName = pack.name?.[locale] || pack.name?.['zh-CN'] || pack.name?.en || packId
  registerPack(packId, displayName, pack.permissions ?? [])
  /** 后登记的先撤销：同一个方法上叠了两层补丁时，这样还原才不会留下半截包装。 */
  const disposers: Array<() => void> = []
  const register = (cleanup: () => void) => disposers.push(cleanup)

  function writeLine(level: 'INFO' | 'WARN' | 'ERROR', message: string, detail?: string): void {
    // 生产构建把 console.log 整条 drop 掉了，日志面板是唯一看得见的那扇门。
    WriteLog({ level, message, source: packId, detail: detail ?? '' }).catch(() => { /* 日志失败不该再抛 */ })
  }

  function assertNewPath(path: string): void {
    if (!path.startsWith('/') || path.includes('..')) {
      throw new Error(`路径 ${path} 不合法：要以 / 开头，且不能含 ..`)
    }
    const name = routeNameFor(packId, path)
    if (router.hasRoute(name)) throw new Error(`路由 ${path} 已经由本包登记过`)
    // 覆盖内置页面等于把应用自己的功能换掉，这是「动内核」，不该由一次拼错路径触发。
    const clash = router.getRoutes().find(r => r.path === path && r.name !== name)
    if (clash) throw new Error(`路由 ${path} 与应用内置页面重名，请换一个路径`)
  }

  const cczj: CczjApi = {
    pack: {
      id: packId,
      kind: pack.kind,
      version: pack.version,
      name: pack.name ?? {},
      dir: pack.dir ?? '',
      icon: pack.icon ?? '',
    },
    bindings: guardBindings(bindings, packId),
    router,
    vue: vueRuntime,
    stores: createStores(),
    i18n: {
      t: (key: string, named?: Record<string, unknown>) => i18n.global.t(key, named ?? {}),
      merge(messages) {
        for (const [locale, content] of Object.entries(messages)) {
          i18n.global.mergeLocaleMessage(locale as Locale, content)
        }
      },
      get locale() { return i18n.global.locale.value as Locale },
    },
    events: {
      on<T>(name: string, handler: (payload: T) => void) {
        const off = onBackendEvent<T>(name, handler)
        register(off)
        return off
      },
    },
    storage: {
      get<T>(key: string, fallback: T) {
        return readStorage<T>(`plugin:${packId}:${key}`, fallback)
      },
      set<T>(key: string, value: T) {
        writeStorage(`plugin:${packId}:${key}`, value)
      },
      remove(key: string) {
        removeStorage(`plugin:${packId}:${key}`)
      },
    },
    log: {
      info: (message, detail) => writeLine('INFO', message, detail),
      warn: (message, detail) => writeLine('WARN', message, detail),
      error: (message, detail) => writeLine('ERROR', message, detail),
    },
    nav(item) {
      addPluginNav({ packId, path: item.path, label: item.label, icon: item.icon || 'code' })
      const undo = () => removePluginNav(packId, item.path)
      register(undo)
      return undo
    },
    components: packComponents,
    settingsTab(item) {
      if (RESERVED_SETTINGS_IDS.has(item.id)) {
        throw new Error(`分组 id ${item.id} 是应用内置的，扩展包不能占用`)
      }
      const tab: PluginSettingsTab = {
        packId,
        id: item.id,
        label: item.label,
        icon: item.icon || 'sliders',
        // markRaw 的理由和 route() 里那句一样：登记簿是 ref，不能被响应式包装。
        component: markRaw(item.component),
        order: item.order ?? 50,
      }
      addPluginSettingsTab(tab)
      const undo = () => removePluginSettingsTab(packId, item.id)
      register(undo)
      return undo
    },
    route(path, component) {
      assertNewPath(path)
      const name = routeNameFor(packId, path)
      // markRaw 是必需的而不是优化：登记簿是个 ref，深层响应化会把组件定义对象包成 proxy，
      // Vue 挂载时就在这里那句「received a Component that was made a reactive object」告警。
      const raw = markRaw(component)
      router.addRoute({ name, path, component: raw })
      addPluginRoute({ packId, name, path, component: raw })
      const undo = () => {
        if (router.hasRoute(name)) router.removeRoute(name)
        removePluginRoute(name)
      }
      register(undo)
      return undo
    },
    css(text) {
      const el = document.createElement('style')
      el.dataset.cczjPack = packId
      el.textContent = text
      document.head.appendChild(el)
      const undo = () => el.remove()
      register(undo)
      return undo
    },
    intercept(target, method, wrapper) {
      const holder = target as Record<string, unknown>
      const original = holder[method]
      if (typeof original !== 'function') {
        throw new Error(`${method} 不是一个方法，没法包住它`)
      }
      const patched = function (this: unknown, ...args: unknown[]) {
        return wrapper(original.bind(this) as (...a: unknown[]) => unknown, ...args)
      }
      holder[method] = patched
      const undo = () => {
        if (holder[method] === patched) holder[method] = original
      }
      register(undo)
      return undo
    },
    onDispose(cleanup) {
      register(cleanup)
    },
  }

  function dispose(): void {
    for (let i = disposers.length - 1; i >= 0; i--) {
      try {
        disposers[i]()
      } catch (e) {
        console.error('[plugin] 撤销失败', packId, e)
      }
    }
    disposers.length = 0
    // 包自己没登记的（比如它只调了 nav 又没保存撤销函数）也要收回，否则重扫之后侧边栏会攒出幽灵条目。
    dropPackNav(packId)
    dropPackRoutes(packId)
    dropPackSettingsTabs(packId)
  }

  return { cczj, dispose }
}
