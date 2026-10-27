/** Runtime adapter kept beside generated binding adapters. */
import './androidTransport'
export { Dialogs, Window } from '@wailsio/runtime'
import { Browser, System } from '@wailsio/runtime'

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
