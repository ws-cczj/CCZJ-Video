/**
 * script 段的注入运行时：把注册表里「已启用且带 script 段」的包读进来、当成 ES 模块跑一遍，
 * 并把它们登记的东西在重扫时收回。
 *
 * 三条规则撑起全部行为：
 * 1. 一轮只跑一个注入过程（schedule 里的 Promise 链），避免重扫和启动撞车；
 * 2. 单个包炸掉只隔离它自己——记进 plugin_script_broken，下一轮跳过，直到用户点重试；
 * 3. 注入过程中做过的每一件事都能撤销，所以半途失败的包不会留下半个页面或一段样式。
 */
import { WriteLog } from '../api/app'
import { usePluginStore, type ScriptPack } from '../stores/plugins'
import { createPackRuntime, type CczjApi, type PackRuntime } from './api'
import { claimBlobUrl, releaseBlobUrls } from './attribution'
import { installNetworkGuards } from './permissions'

/** 当前生效的包 -> 它的撤销入口 + 注入时那份内容的指纹 + 它那份代码的 blob URL。 */
interface Active {
  runtime: PackRuntime
  sig: string
  /** 授权闸门靠这几个 URL 把一次调用认到某个包头上（plugins/attribution.ts）。 */
  urls: string[]
}

const active = new Map<string, Active>()

/** 串成一条链：后一轮一定等在前一轮之后，不会出现两个 pass 交错登记。 */
let queue: Promise<void> = Promise.resolve()

type Store = ReturnType<typeof usePluginStore>

/**
 * 让界面上的注入型扩展包跟注册表对齐：启动时跑一次，设置页重扫、开关某个包之后再跑一次。
 * 一轮下来没变的包一个字节都不会被重跑，所以开关一个包不会闪另外几个包的界面。
 */
export function syncPluginScripts(): Promise<void> {
  queue = queue.then(runPass).catch((e) => {
    console.error('[plugin] 扩展包注入轮次失败', e)
  })
  return queue
}

/** 一个包这轮读到的内容：样式、入口源码，以及判断「要不要重建」用的指纹。 */
interface Loaded {
  css: string[]
  source: string
  sig: string
}

async function runPass(): Promise<void> {
  const store = usePluginStore()
  await store.ensureLoaded()
  const wanted = new Map<string, ScriptPack>()
  for (const entry of store.scriptPacks) {
    // 本机跑挂过的包不再自动重试：坏代码每次都炸一遍的话，用户只会看到应用一直卡。
    if (store.scriptError(entry.packId)) continue
    wanted.set(entry.packId, entry)
  }
  for (const [packId, current] of [...active]) {
    if (!wanted.has(packId)) disposeOne(packId, current)
  }
  // 先把所有要用的文件并行读出来，再逐个对照落地：读包走的是绑定来回，是一个包接一个
  // 包串起来的唯一慢活儿，而「启用」那点延迟正好落在用户看得见的地方。
  const reads = new Map<string, Promise<Loaded>>()
  for (const [packId, entry] of wanted) {
    const read = readPack(store, entry)
    // 并行预读时某个包可能先失败，但它的处理器要等前面的包逐个落地才挂得上；这段空档会被
    // App.vue 的 unhandledrejection 记成一条红色错误弹窗。先派一个吞掉的分支占位，
    // 真正的失败仍在下面 await 处按原样隔离。
    read.catch(() => {})
    reads.set(packId, read)
  }
  for (const [packId, entry] of wanted) {
    const current = active.get(packId)
    let loaded: Loaded
    try {
      loaded = await reads.get(packId)!
    } catch (e) {
      isolate(store, entry, current, e)
      continue
    }
    // 指纹对得上就说明这个包一行都不必重跑：样式、路由、侧边栏条目全留在原处。
    if (current && current.sig === loaded.sig) continue
    disposeOne(packId, current)
    await inject(store, entry, loaded)
  }
}

async function readPack(store: Store, entry: ScriptPack): Promise<Loaded> {
  const styles = entry.script.styles ?? []
  const texts = await Promise.all([
    ...styles.map(style => store.readText(entry.packId, style)),
    store.readText(entry.packId, entry.script.entry),
  ])
  const css: string[] = []
  for (let i = 0; i < styles.length; i++) {
    if (!texts[i]) throw new Error(`读取样式文件失败：${styles[i]}`)
    css.push(texts[i])
  }
  const source = texts[styles.length]
  if (!source) throw new Error(`读取入口脚本失败：${entry.script.entry}`)
  return { css, source, sig: fingerprint(css.join('\n') + '\n' + source) }
}

/**
 * FNV-1a。这里只要回答「同一个包这轮读到的和上轮是不是同一份」，不抗碰撞，所以不必上 crypto。
 */
function fingerprint(text: string): string {
  let hash = 0x811c9dc5
  for (let i = 0; i < text.length; i++) {
    hash ^= text.charCodeAt(i)
    hash = Math.imul(hash, 0x01000193)
  }
  return `${text.length}:${(hash >>> 0).toString(16)}`
}

async function inject(store: Store, entry: ScriptPack, loaded: Loaded): Promise<void> {
  const runtime = createPackRuntime(entry.pack)
  const urls: string[] = []
  try {
    // 出网闸门要在第一行包代码跑起来之前就在位，否则那几行正好是没人管的那一段。
    installNetworkGuards()
    // 先把模块取进来，再动界面：动态 import 是这一轮里唯一长的异步段。反过来先挂样式，
    // 界面上就会出现几百毫秒「包的外观已经生效、它的页面和入口还没登记上」的中间态。
    const module = await importSource(loaded.source, entry.packId, urls)
    for (const text of loaded.css) runtime.cczj.css(text)
    await setupOf(module)(runtime.cczj)
    active.set(entry.packId, { runtime, sig: loaded.sig, urls })
  } catch (e) {
    isolate(store, entry, { runtime, sig: loaded.sig, urls }, e)
  }
}

/** 注入失败的收场：半个包都不许留在界面上，并且要在日志时间线上留痕。 */
function isolate(store: Store, entry: ScriptPack, partial: Active | undefined, e: unknown): void {
  // 已经注入成功的部分要收干净，否则界面上会留下一个没有后端的按钮。
  disposeOne(entry.packId, partial)
  const message = describe(e)
  store.markScriptBroken(entry.packId, message)
  // 生产构建里 console.error 是没人看得见的地方，而「这个包为什么没生效」正是会去
  // 日志面板找的那一条，所以隔离这件事必须自己在时间线上留痕。
  WriteLog({
    level: 'ERROR',
    message: `扩展包注入失败，已隔离：${message}`,
    source: entry.packId,
    detail: entry.script.entry,
  }).catch(() => { /* 日志失败不该再抛 */ })
  console.error(`[plugin] ${entry.packId} 注入失败，已隔离`, e)
}

/**
 * 包源码走 blob URL 动态 import：应用没有 CSP（也不会有），所以这是唯一能在
 * 运行时吃下用户自己的 JS 又不必依赖 eval 的办法。URL 每次都是新的，改完包
 * 重扫一定能拿到新代码。
 *
 * URL 用完就 revoke，但那个字符串要留在 `urls` 里登记给闸门：模块实例化之后
 * 脚本名就固化了，栈里的每一帧都还带着它，revoke 不影响认领。
 */
async function importSource(source: string, packId: string, urls: string[]): Promise<Record<string, unknown>> {
  const url = URL.createObjectURL(new Blob([source], { type: 'text/javascript;charset=utf-8' }))
  urls.push(url)
  claimBlobUrl(url, packId)
  try {
    return (await import(/* @vite-ignore */ url)) as Record<string, unknown>
  } finally {
    URL.revokeObjectURL(url)
  }
}

/** 入口既可以是 default 导出的函数，也可以是具名 setup——包作者怎么写顺手怎么来。 */
function setupOf(module: Record<string, unknown>): (cczj: CczjApi) => void | Promise<void> {
  const named = module.setup
  if (typeof named === 'function') return named as (cczj: CczjApi) => void | Promise<void>
  const def = module.default
  if (typeof def === 'function') return def as (cczj: CczjApi) => void | Promise<void>
  if (def && typeof (def as { setup?: unknown }).setup === 'function') {
    return (def as { setup: (cczj: CczjApi) => void | Promise<void> }).setup
  }
  throw new Error('入口脚本没有导出可调用的 setup 或 default 函数')
}

function disposeOne(packId: string, current: Active | undefined): void {
  if (!current) return
  active.delete(packId)
  // 认领先撤，这个包迟到的回调就不会再被算成「它又在出网」。
  releaseBlobUrls(current.urls)
  try {
    current.runtime.dispose()
  } catch (e) {
    console.error('[plugin] 收回扩展包失败', packId, e)
  }
}

/** 当前真正注入成功的包 id，供界面判断「这个包现在到底生效没有」。 */
export function activeScriptPacks(): string[] {
  return [...active.keys()]
}

function describe(e: unknown): string {
  if (e instanceof Error) return e.message || e.name
  return String(e)
}
