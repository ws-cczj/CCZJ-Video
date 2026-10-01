/**
 * 扩展包的授权闸门：管住两件事——「改应用的数据」和「自己往外发请求」。
 *
 * 规则只有一条：包必须先在 manifest 的 `permissions` 里声明，然后第一次真用到的
 * 那一刻弹窗问一次用户；同意与否记进 settings 表的 `plugin_permissions`，重启还在，
 * 在设置页 → 扩展包里能看见也能撤销。没声明的能力连问都不问，直接拒——那才是
 * 声明的意义：作者没承诺要用的东西，不该有个弹窗替他要。
 *
 * 这不是沙箱，也别当成沙箱：包代码跑在应用自己的 realm 里，想绕过闸门还有一堆办法
 * （`XMLHttpRequest` 没拦，那是 hls.js 取分片在用的；`WebSocket` 也没拦——补丁本身在严格
 * 模式下给原生构造函数的静态属性赋值就会抛，真机日志里第一个装上的包就是被它连坐隔离的）。
 * 出网这一侧拦的只有「包往应用外面发」：打到应用自己主机上的请求直通，因为 Wails 的后端调用
 * 本身就是一次 `fetch`，包只要调个绑定就会撞进闸门。这一层的价值在于「用户看得见、点过一次
 * 才算数」，以及让坏代码的破坏停在第一次调用上。
 */
import { ref } from 'vue'
import * as bindings from '../api/app'
import { GetSetting, SetSetting, WriteLog } from '../api/app'
import { tr } from '../locales'
import { useConfirmStore } from '../stores/confirm'
import { callingPack } from './attribution'

export type PackPermission = 'write' | 'network'
export type PermissionDecision = 'granted' | 'denied'

/** packId -> 能力 -> 用户给过什么答复。没有条目就是「还没问过」。 */
type Ledger = Record<string, Partial<Record<PackPermission, PermissionDecision>>>

const LEDGER_KEY = 'plugin_permissions'

const ledger = ref<Ledger>({})
let ledgerLoad: Promise<void> | null = null

interface PackIdentity {
  name: string
  declared: PackPermission[]
}

const identities = new Map<string, PackIdentity>()

/** 注入运行时创建时登记一次：闸门拿到 packId 之后还得知道它叫什么、声明了什么。 */
export function registerPack(packId: string, name: string, declared: string[]): void {
  const known: PackPermission[] = []
  for (const item of declared) {
    if ((item === 'write' || item === 'network') && !known.includes(item)) known.push(item)
  }
  identities.set(packId, { name: name || packId, declared: known })
}

/** 界面用：这个包这项能力此刻的状态。 */
export function permissionDecision(packId: string, perm: PackPermission): PermissionDecision | '' {
  return ledger.value[packId]?.[perm] ?? ''
}

function packLabel(packId: string): string {
  return identities.get(packId)?.name || packId
}

function permLabel(perm: PackPermission): string {
  return tr(perm === 'network' ? 'permissions.networkLabel' : 'permissions.writeLabel')
}

async function ensureLedger(): Promise<void> {
  if (!ledgerLoad) {
    ledgerLoad = (async () => {
      try {
        const raw = await GetSetting(LEDGER_KEY)
        if (raw) ledger.value = JSON.parse(raw) as Ledger
      } catch {
        // 读不到账本就当什么都没授权过：闸门照常拦，只是用户会被重新问一次。
      }
    })()
  }
  await ledgerLoad
}

function record(packId: string, perm: PackPermission, decision: PermissionDecision | null): void {
  const next: Partial<Record<PackPermission, PermissionDecision>> = { ...ledger.value[packId] }
  if (decision) next[perm] = decision
  else delete next[perm]
  const all = { ...ledger.value }
  if (Object.keys(next).length) all[packId] = next
  else delete all[packId]
  ledger.value = all
  SetSetting(LEDGER_KEY, JSON.stringify(all)).catch(() => { /* 存不下也别把包卡住 */ })
}

function writeLine(level: 'INFO' | 'WARN', message: string, packId: string, detail: string): void {
  WriteLog({ level, message, source: packId, detail }).catch(() => { /* 日志失败不该再抛 */ })
}

/**
 * 弹窗一次排一个：几个包同时在第一次调用上撞见闸门时，串行才不会把确认框叠成一摞
 * （confirm store 会先取消旧的那个，那样前一个包的答复就成了「被取消」，凭空替用户做了决定）。
 */
let promptChain: Promise<unknown> = Promise.resolve()

function ask(packId: string, perm: PackPermission, detail: string): Promise<boolean> {
  const run = async (): Promise<boolean> => {
    const name = packLabel(packId)
    const ok = await useConfirmStore().confirm({
      title: tr('permissions.title'),
      message: tr(perm === 'network' ? 'permissions.promptNetwork' : 'permissions.promptWrite', { name, perm: permLabel(perm), detail }),
      okText: tr('permissions.allow'),
      cancelText: tr('permissions.deny'),
      level: 'warn',
    })
    record(packId, perm, ok ? 'granted' : 'denied')
    const params = { name, perm: permLabel(perm), detail }
    writeLine(ok ? 'INFO' : 'WARN', tr(ok ? 'permissions.grantedLog' : 'permissions.deniedLog', params), packId, '')
    return ok
  }
  promptChain = promptChain.then(run, run)
  return promptChain as Promise<boolean>
}

/**
 * 过闸：允许就返回，拒绝或没声明就抛错。抛的是普通 Error，消息已经是给人看的——
 * 包没接住的话会走应用那条统一的错误反馈，用户看得见是哪个包被拦了。
 */
export async function requirePermission(packId: string, perm: PackPermission, detail: string): Promise<void> {
  const identity = identities.get(packId)
  const name = packLabel(packId)
  if (!identity?.declared.includes(perm)) {
    const message = tr('permissions.undeclared', { name, perm: permLabel(perm), detail })
    writeLine('WARN', message, packId, '')
    throw new Error(message)
  }
  await ensureLedger()
  const current = ledger.value[packId]?.[perm]
  if (current === 'granted') return
  if (current === 'denied') throw new Error(tr('permissions.denied', { name, perm: permLabel(perm) }))
  const ok = await ask(packId, perm, detail)
  if (!ok) throw new Error(tr('permissions.denied', { name, perm: permLabel(perm) }))
}

/**
 * 读账本：闸门在包真动手时自己会读，界面（扩展包卡片）一挂上来就需要它——不然一个
 * 早就被拒过的包在卡片上会显示成「还没问过」，那正好是把最该看见的状态藏起来。
 * 重复调用只会命中那一次已发出的读取。
 */
export { ensureLedger as loadPermissionLedger }

/** 用户在扩展包面板撤销授权：清掉这一条，下次这个包再用到时会重新问。 */
export async function resetPermission(packId: string, perm: PackPermission): Promise<void> {
  await ensureLedger()
  record(packId, perm, null)
  writeLine('INFO', tr('permissions.resetLog', { name: packLabel(packId), perm: permLabel(perm) }), packId, '')
}

// ---------- cczj.bindings 的写侧闸门 ----------

/**
 * 认「只读」的前缀。判不准时一律算写：宁可让用户多点一次「允许」，也不能放过一次
 * 悄悄改了库的调用。新增的绑定如果名字起得奇怪，落在这张表外面就是需要授权，
 * 那是安全方向的错，改名字就行。
 */
const READONLY_PREFIXES = ['Get', 'List', 'Find', 'Read', 'Search', 'Check', 'Is', 'Has', 'Count', 'Query']

/** 名字不带 Get/List 这类前缀、但确实只是读或纯算的那些。 */
const READONLY_NAMES = new Set([
  'normalizeApiError',
  'ProxyImage',
  'DoubanChart',
  'DoubanChartResolve',
  'DoubanDetail',
  'DoubanGetAll',
  'DoubanSearch',
  'DoubanStatus',
  'SourceProbeTimeline',
  'SpeedTestPlayLines',
  'WindowGetResizable',
  'WindowGetSize',
  'WindowIsFs',
  'WindowIsMax',
  'CompressDetailJSONBrotli',
  'DecompressDetailJSONBrotli',
  'FileExists',
])

function isReadOnlyBinding(name: string): boolean {
  if (READONLY_NAMES.has(name)) return true
  return READONLY_PREFIXES.some(prefix => name.startsWith(prefix))
}

type BindingFn = (...args: unknown[]) => unknown

/**
 * 把整个绑定命名空间包一层：只读的直通，其余的每次调用先过闸门。
 * Wails 的绑定本来就返回 Promise，所以套成 async 不改变包的写法。
 */
export function guardBindings(namespace: typeof bindings, packId: string): typeof bindings {
  const target = namespace as unknown as Record<string, unknown>
  return new Proxy(target, {
    get(receiverTarget, prop): unknown {
      const value = Reflect.get(receiverTarget, prop)
      if (typeof prop !== 'string' || typeof value !== 'function' || isReadOnlyBinding(prop)) return value
      const call = value as BindingFn
      return (...args: unknown[]): unknown =>
        requirePermission(packId, 'write', `${prop}()`).then(() => call.apply(namespace, args))
    },
  }) as unknown as typeof bindings
}

// ---------- 出网闸门 ----------

let guardsInstalled = false

/** 在第一次注入包代码之前装好；重复调用无副作用。 */
export function installNetworkGuards(): void {
  if (guardsInstalled) return
  guardsInstalled = true
  guardFetch()
}

function targetText(input: RequestInfo | URL): string {
  if (typeof input === 'string') return input
  if (input instanceof URL) return input.href
  return input.url
}

/**
 * 打到应用自己主机上的请求一律直通。真机日志里那句被拦的 URL 是
 * `http://wails.localhost/wails/runtime`：Wails 调后端绑定就是往这里发一次 `fetch`，而这次
 * `fetch` 的调用栈顶上就是包自己的代码——icon-gallery / drop-probe / script-ui-tweaks 三个包
 * 都是这么被「你没声明出网」误伤的。出网问的是「包往外发不发」，递给应用自己不算。
 *
 * `wails.localhost` 要单独认：它是 WebView2 给应用挂的虚拟主机名，而 Wails 自己的请求拦截规则
 * 除了无端口的那条，还并列写了一条带端口的版本（见 `request_cancellation_windows.go`），所以
 * 页面 origin 未必与 IPC 目标逐字相等。只比 origin，端口一不同就会把「调后端绑定」的包再次误伤。
 */
const APP_HOSTNAME = 'wails.localhost'

function isAppOwnOrigin(text: string): boolean {
  try {
    const url = new URL(text, window.location.href)
    return url.hostname === APP_HOSTNAME || url.origin === window.location.origin
  } catch {
    return false
  }
}

function guardFetch(): void {
  const raw = window.fetch
  if (typeof raw !== 'function') return
  window.fetch = (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    const target = targetText(input)
    if (isAppOwnOrigin(target)) return raw.call(window, input, init)
    const packId = callingPack()
    // 不是包发起的（播放器取分片、探测、图片代理）一律原样放行，闸门不能替播放器做决定。
    if (!packId) return raw.call(window, input, init)
    return requirePermission(packId, 'network', target).then(() => raw.call(window, input, init))
  }
}
