/** Runtime adapter kept beside generated binding adapters. */
import './androidTransport'
export { Dialogs, Window } from '@wailsio/runtime'
import { System } from '@wailsio/runtime'

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


/** Full document navigation also works inside the Android WebView. */
export function reloadApplication(url: string): void { window.location.replace(url) }

/** Android owns the Activity after the Go service has requested shutdown. */
export function closeMobileApplication(): void {
  if (isMobileRuntime()) (window as unknown as {wails?:{exitApp?:()=>void}}).wails?.exitApp?.()
}
