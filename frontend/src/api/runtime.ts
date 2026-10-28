/** Runtime adapter kept beside generated binding adapters. */
import './androidTransport'
export { Dialogs, Window } from '@wailsio/runtime'
import { Browser, System } from '@wailsio/runtime'
import { object, integer, text, boolean } from './decode'

let runtimeOS: string | undefined

/** Android's beta host does not preload the environment used by sync helpers. */
export async function initializeRuntimeEnvironment(): Promise<void> {
  try { runtimeOS = (await System.Environment()).OS } catch { /* Browser previews have no native host. */ }
}

/** The Wails 3 runtime uses its native environment, rather than window.go. */
export function isDesktopRuntime(): boolean {
  if (runtimeOS) return ['windows', 'darwin', 'linux'].includes(runtimeOS)
  return System.IsWindows() || System.IsMac() || System.IsLinux()
}

export function isMobileRuntime(): boolean {
  if (runtimeOS) return runtimeOS === 'android' || runtimeOS === 'ios'
  return System.IsMobile()
}

/** Platform-specific capabilities use the initialized host, never the user agent. */
export function isIOSRuntime(): boolean {
  return runtimeOS === 'ios'
}

export interface AndroidMemoryState {
  available: boolean
  complete: false
  scope: 'app_process'
  source: 'ActivityManager.getProcessMemoryInfo'
  pid: number
  queried_at_ms: number
  webview_provider: string
  webview_version: string
  pss_bytes?: number
  private_resident_bytes?: number
  total_swap_bytes?: number
  java_heap_bytes?: number
  native_heap_bytes?: number
  code_bytes?: number
  stack_bytes?: number
  graphics_bytes?: number
  private_other_bytes?: number
  system_bytes?: number
  package_name?: string
  dump_permission?: boolean
  usage_access?: boolean
  attributed?: {
    available: boolean
    source: 'dumpsys.meminfo.local'
    scope: 'confirmed_owned_processes'
    queried_at_ms: number
    pss_bytes?: number
    processes?: Array<{pid:number;role:'app'|'renderer'|'app_child';pss_bytes:number;rss_bytes?:number}>
    error_code?: string
  }
}

/** Reads a cached native sample. Missing support/data stays unavailable; never
 * substitutes the app process or JS heap for the whole Android application. */
export function readAndroidMemoryState(): AndroidMemoryState | null {
  if (runtimeOS !== 'android') return null
  const bridge = (window as unknown as {wails?:{memoryState?:()=>string}}).wails
  if (!bridge?.memoryState) return null
  try {
    const row = object(JSON.parse(bridge.memoryState()))
    if (row.scope !== 'app_process' || row.source !== 'ActivityManager.getProcessMemoryInfo' || row.complete !== false) return null
    boolean(row.available)
    for (const field of ['pid', 'queried_at_ms']) integer(row[field], 1)
    for (const [key, value] of Object.entries(row)) if (key.endsWith('_bytes')) integer(value)
    text(row.webview_provider); text(row.webview_version)
    if (row.package_name !== undefined && !/^[a-zA-Z][\w]*(?:\.[a-zA-Z][\w]*)+$/.test(text(row.package_name))) return null
    for (const key of ['dump_permission', 'usage_access']) if (row[key] !== undefined) boolean(row[key])
    if (row.attributed !== undefined) {
      const sample = object(row.attributed)
      boolean(sample.available); integer(sample.queried_at_ms, 1)
      if (sample.source !== 'dumpsys.meminfo.local' || sample.scope !== 'confirmed_owned_processes') return null
      if (sample.error_code !== undefined) text(sample.error_code)
      if (sample.available) {
        integer(sample.pss_bytes)
        if (!Array.isArray(sample.processes) || !sample.processes.length || sample.processes.length > 8) return null
        const pids = new Set<number>(); let total = 0
        for (const entry of sample.processes) {
          const process = object(entry), pid = integer(process.pid, 1)
          if (pids.has(pid) || !['app', 'renderer', 'app_child'].includes(text(process.role))) return null
          pids.add(pid); total += integer(process.pss_bytes)
          if (process.rss_bytes !== undefined) integer(process.rss_bytes)
        }
        if (total !== sample.pss_bytes) return null
      }
    }
    return row as unknown as AndroidMemoryState
  } catch { return null }
}

export function openAndroidMemoryAccessSettings(): void {
  const bridge = (window as unknown as {wails?:{memoryAccessSettings?:()=>string}}).wails
  if (runtimeOS !== 'android' || !bridge?.memoryAccessSettings) throw new Error('UNSUPPORTED')
  const error = bridge.memoryAccessSettings()
  if (error) throw new Error(error)
}

/** Wails' generic browser call uses xdg-open on Android; use the host Intent. */
export async function openExternalURL(url: string): Promise<void> {
  const target = new URL(url)
  if (!['https:', 'http:'].includes(target.protocol)) throw new Error('Unsupported external URL')
  if (isMobileRuntime() && !isIOSRuntime()) {
    const bridge = (window as unknown as {wails?:{openExternalURL?:(url:string)=>string}}).wails
    if (!bridge?.openExternalURL) throw new Error('Android browser host unavailable')
    const error = bridge.openExternalURL(target.href)
    if (error) throw new Error(error)
    return
  }
  if (isDesktopRuntime() || isIOSRuntime()) await Browser.OpenURL(target.href)
  else window.open(target.href, '_blank', 'noopener,noreferrer')
}


/** Full document navigation also works inside the Android WebView. */
export function reloadApplication(url: string): void { window.location.replace(url) }

/** Android owns the Activity after the Go service has requested shutdown. */
export function closeMobileApplication(): void {
  if (isMobileRuntime()) (window as unknown as {wails?:{exitApp?:()=>void}}).wails?.exitApp?.()
}
