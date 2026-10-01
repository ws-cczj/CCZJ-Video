/**
 * 扩展包注册表的前端状态层。
 *
 * Go 侧 `app/plugin` 是唯一校验权威：它扫盘、校验 manifest、记住启停，并把每个包的
 * 状态回报成 ready/disabled/invalid。这里只做展示用的整理、按语言的名称回落，
 * 把包内文件读成引擎能用的文本/图片，以及记住哪些注入型包在本机跑挂过（`scriptError`）。
 */
import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import {
  GetPluginDirectory,
  ListPlugins,
  ReadPluginAsset,
  ReadPluginFile,
  RescanPlugins,
  SetPluginEnabled,
  UninstallPlugin,
  normalizeApiError,
} from '../api/app'
import i18n, { tr, type Locale } from '../locales'
import { readStorage, writeStorage } from '../platform/storage'

export type PluginKind = 'source' | 'shader' | 'theme' | 'script'
export type PluginStatus = 'ready' | 'disabled' | 'invalid'

/** 本地化名称：包作者给什么语言就用什么，缺当前语言时回落到另一种。 */
export type LocalizedText = Record<string, string>

export interface PluginAdapter {
  id: string
  name: LocalizedText
  /**
   * Go 侧是 json.RawMessage，到前端已经是一个策略文档对象（形状由 source-strategy-v2 校验过）。
   * 写回 sources.strategy_config 时要序列化，见 strategyText()。
   */
  strategy: unknown
}

export interface PluginShaderDecl {
  id: string
  name: LocalizedText
  /** 包内相对路径，指向 .glsl */
  entry: string
  scale: number
  category?: string
}

export interface PluginThemeDecl {
  id: string
  name: LocalizedText
  primary: string
  mode: string
  tint?: Record<string, string>
  bg_image?: string
}

/** script 段：注入 webview 的入口脚本与样式表，路径都是包内相对路径。 */
export interface PluginScriptDecl {
  entry: string
  styles?: string[]
}

export interface PluginInfo {
  id: string
  kind: string
  version: string
  name: LocalizedText
  description?: LocalizedText
  author?: string
  dir: string
  status: string
  reason_code?: string
  reason?: string
  source?: { adapters?: PluginAdapter[] }
  shader?: { shaders?: PluginShaderDecl[] }
  theme?: { presets?: PluginThemeDecl[] }
  script?: PluginScriptDecl
  /** manifest.icon 校验通过后的包内相对路径；空串表示这个包没给图标。 */
  icon?: string
  /** manifest.permissions 声明的能力（write|network）。只是声明，授权状态记在闸门那一本账里。 */
  permissions?: string[]
  /** 下面三项是 Go 扫描时从包目录实际数出来的，不来自 manifest。 */
  files?: number
  bytes?: number
  /** Go 侧格式化好的本地时间（2006-01-02 15:04），前端不再解析。 */
  updated?: string
  /** 应用自带的扩展包（logs-panel / diagnostics-panel 那批）：界面不给卸载按钮。 */
  builtin?: boolean
}

/** 一个可运行的着色器档位，带所属包的信息。 */
export interface ShaderOption {
  /** `${packId}/${shaderId}`，也是本地隔离记录的键 */
  key: string
  packId: string
  shader: PluginShaderDecl
}

/** 一个待注入的脚本包：script 是横切段，所以这里按「有没有这一段」挑，不看 kind。 */
export interface ScriptPack {
  packId: string
  pack: PluginInfo
  script: PluginScriptDecl
}

const BROKEN_KEY = 'plugin_shader_broken'
/** 本机跑挂过的脚本包：packId -> 错误摘要。持久化，否则每次启动都要重跑一遍坏代码。 */
const BROKEN_SCRIPT_KEY = 'plugin_script_broken'

function brokenMap(key: string): Record<string, string> {
  return readStorage<Record<string, string>>(key, {})
}

export const usePluginStore = defineStore('plugins', () => {
  const packs = ref<PluginInfo[]>([])
  const loaded = ref(false)
  const refreshing = ref(false)
  /** 扩展包目录的绝对路径，设置页用它指路（扫不到也不影响，留空即可）。 */
  const directory = ref('')
  /** 整个注册表读取失败（磁盘、数据库或绑定异常）时的人话说明。 */
  const registryError = ref('')
  /** 本机编译/链接失败的着色器：key -> GL 错误摘要。持久化，避免每次换集重复试探。 */
  const brokenShaders = ref<Record<string, string>>(brokenMap(BROKEN_KEY))
  /** 本机跑挂过的脚本包：packId -> 错误摘要。注入失败只隔离那一个包，不牵连别人。 */
  const brokenScripts = ref<Record<string, string>>(brokenMap(BROKEN_SCRIPT_KEY))

  const textCache = new Map<string, string>()
  const assetCache = new Map<string, string>()

  function localized(map?: LocalizedText): string {
    if (!map) return ''
    const lang = i18n.global.locale.value as Locale
    return map[lang] || map['zh-CN'] || map['en'] || Object.values(map)[0] || ''
  }

  const readyPacks = computed(() => packs.value.filter(p => p.status === 'ready'))
  const disabledPacks = computed(() => packs.value.filter(p => p.status === 'disabled'))
  const invalidPacks = computed(() => packs.value.filter(p => p.status === 'invalid'))

  const allShaderOptions = computed<ShaderOption[]>(() => {
    const out: ShaderOption[] = []
    for (const pack of readyPacks.value) {
      if (pack.kind !== 'shader') continue
      for (const shader of pack.shader?.shaders ?? []) {
        out.push({ key: `${pack.id}/${shader.id}`, packId: pack.id, shader })
      }
    }
    return out
  })

  const shaderOptions = computed(() =>
    allShaderOptions.value.filter(o => !brokenShaders.value[o.key])
  )

  const themePresets = computed(() => {
    const out: Array<{ packId: string; preset: PluginThemeDecl }> = []
    for (const pack of readyPacks.value) {
      if (pack.kind !== 'theme') continue
      for (const preset of pack.theme?.presets ?? []) {
        out.push({ packId: pack.id, preset })
      }
    }
    return out
  })

  const sourceAdapters = computed(() => {
    const out: Array<{ packId: string; packName: string; adapter: PluginAdapter }> = []
    for (const pack of readyPacks.value) {
      if (pack.kind !== 'source') continue
      const packName = localized(pack.name)
      for (const adapter of pack.source?.adapters ?? []) {
        out.push({ packId: pack.id, packName, adapter })
      }
    }
    return out
  })

  /** 待注入的脚本包：script 是横切段，按「有没有这一段」挑，不限 kind。 */
  const scriptPacks = computed<ScriptPack[]>(() => {
    const out: ScriptPack[] = []
    for (const pack of readyPacks.value) {
      if (!pack.script?.entry) continue
      out.push({ packId: pack.id, pack, script: pack.script })
    }
    return out
  })

  function shaderLabel(o: ShaderOption): string {
    const pack = packs.value.find(p => p.id === o.packId)
    const name = localized(o.shader.name) || localized(pack?.name)
    return name || o.shader.id
  }

  /** 采集适配策略写进 sources.strategy_config 时的 JSON 文本。 */
  function strategyText(a: PluginAdapter): string {
    return typeof a.strategy === 'string' ? a.strategy : JSON.stringify(a.strategy ?? {})
  }

  async function refresh(rescan = false): Promise<void> {
    refreshing.value = true
    if (!directory.value) {
      // 只是给界面多一行指路文字，读不到不算注册表失败。
      try { directory.value = await GetPluginDirectory() } catch { /* ignore */ }
    }
    try {
      const list = rescan ? await RescanPlugins() : await ListPlugins()
      packs.value = (list ?? []) as PluginInfo[]
      registryError.value = ''
    } catch (e) {
      registryError.value = normalizeApiError(e).message
    } finally {
      loaded.value = true
      refreshing.value = false
      // 包目录随时可能被人改，重扫之后所有已读过的文件都作废。
      textCache.clear()
      // 图标只在重扫时作废：开关一个包走的是 Go 缓存里的注册表，目录一个字节都没动，
      // 每次都重读一遍全部图片会白添几十个来回，正好落在用户能看见的那几百毫秒里。
      if (rescan) assetCache.clear()
    }
  }

  /**
   * 首次加载那次调用的 promise。ensureLoaded 的并发调用者必须等它，而不是看见
   * refreshing=true 就当注册表已经就绪——主题 store 在启动时就 void 调过一次
   * ensureLoaded，脚本包若不等这一次，拿到的永远是空表（真机上验证过：包 ready=1
   * 却没被注入，界面上没有任何一条错误可看）。
   */
  let loadingOnce: Promise<void> | null = null

  async function ensureLoaded(): Promise<void> {
    if (loaded.value) return
    if (!loadingOnce) {
      loadingOnce = refresh(false).finally(() => { loadingOnce = null })
    }
    await loadingOnce
  }

  async function setEnabled(packId: string, enabled: boolean): Promise<string> {
    try {
      await SetPluginEnabled(packId, enabled)
      await refresh(false)
      return ''
    } catch (e) {
      return normalizeApiError(e).message
    }
  }

  /**
   * 卸载 = 让 Go 删掉那个包目录。返回空串表示成功，否则是人话错误。
   * 调用方成功后要重跑一轮注入（`syncPluginScripts`），否则那个包加过的侧边栏、
   * 样式、设置页分组会留在界面上——磁盘没了、界面还有，比反过来更难解释。
   */
  async function uninstall(packId: string): Promise<string> {
    try {
      await UninstallPlugin(packId)
      await refresh(false)
      return ''
    } catch (e) {
      return normalizeApiError(e).message
    }
  }

  /** 读包内文本文件（着色器源码）。失败返回空串，错误进 console 与调用方。 */
  async function readText(packId: string, path: string): Promise<string> {
    const ck = `${packId}::${path}`
    const hit = textCache.get(ck)
    if (hit !== undefined) return hit
    try {
      const text = await ReadPluginFile(packId, path)
      textCache.set(ck, text ?? '')
      return text ?? ''
    } catch (e) {
      console.error('[Plugins] 读取包文件失败', ck, e)
      return ''
    }
  }

  /** 读包内二进制资源（主题背景图），返回 data URL。 */
  async function readAsset(packId: string, path: string): Promise<string> {
    const ck = `${packId}::${path}`
    const hit = assetCache.get(ck)
    if (hit !== undefined) return hit
    try {
      const url = await ReadPluginAsset(packId, path)
      assetCache.set(ck, url ?? '')
      return url ?? ''
    } catch (e) {
      console.error('[Plugins] 读取包资源失败', ck, e)
      return ''
    }
  }

  async function shaderSource(o: ShaderOption): Promise<string> {
    return readText(o.packId, o.shader.entry)
  }

  function markShaderBroken(key: string, message: string): void {
    if (brokenShaders.value[key] === message) return
    brokenShaders.value = { ...brokenShaders.value, [key]: message }
    writeStorage(BROKEN_KEY, brokenShaders.value)
  }

  function clearShaderBroken(key: string): void {
    if (!(key in brokenShaders.value)) return
    const next = { ...brokenShaders.value }
    delete next[key]
    brokenShaders.value = next
    writeStorage(BROKEN_KEY, next)
  }

  /** 用户在设置页手动重试一个被本机隔离的着色器档位。 */
  function retryShader(key: string): void {
    clearShaderBroken(key)
  }

  function brokenMessage(key: string): string {
    return brokenShaders.value[key] ?? ''
  }

  /** 记下一个跑挂的脚本包：整包隔离，下一轮不再注入，直到用户在扩展面板点重试。 */
  function markScriptBroken(packId: string, message: string): void {
    if (brokenScripts.value[packId] === message) return
    brokenScripts.value = { ...brokenScripts.value, [packId]: message }
    writeStorage(BROKEN_SCRIPT_KEY, brokenScripts.value)
  }

  function retryScript(packId: string): void {
    if (!(packId in brokenScripts.value)) return
    const next = { ...brokenScripts.value }
    delete next[packId]
    brokenScripts.value = next
    writeStorage(BROKEN_SCRIPT_KEY, next)
  }

  function scriptError(packId: string): string {
    return brokenScripts.value[packId] ?? ''
  }

  function stateLabel(status: string): string {
    if (status === 'ready') return tr('settings.extensionsStateReady')
    if (status === 'disabled') return tr('settings.extensionsStateDisabled')
    return tr('settings.extensionsStateInvalid')
  }

  return {
    packs,
    loaded,
    refreshing,
    directory,
    registryError,
    brokenShaders,
    brokenScripts,
    readyPacks,
    disabledPacks,
    invalidPacks,
    allShaderOptions,
    shaderOptions,
    themePresets,
    sourceAdapters,
    scriptPacks,
    localized,
    shaderLabel,
    strategyText,
    refresh,
    ensureLoaded,
    setEnabled,
    uninstall,
    readText,
    readAsset,
    shaderSource,
    markShaderBroken,
    clearShaderBroken,
    retryShader,
    brokenMessage,
    markScriptBroken,
    retryScript,
    scriptError,
    stateLabel,
  }
})
