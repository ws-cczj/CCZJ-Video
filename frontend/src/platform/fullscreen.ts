import { WindowIsFs, WindowIsMax, WindowSetFullscreen, WindowToggleMax } from '../api/app'
import { presentationProfile } from './presentation'

type FullscreenDocument = Document & { webkitFullscreenElement?: Element; webkitExitFullscreen?: () => void | Promise<void> }
type FullscreenRoot = HTMLElement & { webkitRequestFullscreen?: () => void | Promise<void> }
function browserFullscreen(root?: HTMLElement): boolean {
  const doc = document as FullscreenDocument
  const element = doc.fullscreenElement || doc.webkitFullscreenElement
  return !!root && !!element && (element === root || root.contains(element))
}
async function exitBrowserFullscreen(root?: HTMLElement): Promise<void> {
  if (!browserFullscreen(root)) return
  const doc = document as FullscreenDocument
  if (doc.exitFullscreen) await doc.exitFullscreen()
  else await doc.webkitExitFullscreen?.()
}

/** Shared fullscreen ownership for readers and players; restores maximization. */
export function createFullscreenPlatform(){
 const nativeWindow=presentationProfile().host==='native-desktop'
 let wasMaximized=false
 return {
      kind: nativeWindow ? 'window' as const : 'element' as const,
      supported: (root?:HTMLElement) => nativeWindow || (!!root && (
        (document.fullscreenEnabled !== false && typeof root.requestFullscreen === 'function') || typeof (root as FullscreenRoot).webkitRequestFullscreen === 'function')),
      browserActive: browserFullscreen,
      async enter(root: HTMLElement | undefined, current: () => boolean): Promise<boolean> {
        if (nativeWindow) {
          try { wasMaximized = await WindowIsMax() } catch { wasMaximized = false }
          if (!current()) return false
          await WindowSetFullscreen(true)
          return true
        }
        if (!root || !current()) return false
        const element = root as FullscreenRoot
        const request = element.requestFullscreen || element.webkitRequestFullscreen
        if (!request) return false
        await request.call(element)
        return browserFullscreen(root)
      },
      async exit(root: HTMLElement | undefined, current: () => boolean): Promise<void> {
        if (!current()) return
        if (nativeWindow) {
          await WindowSetFullscreen(false)
          if (current() && wasMaximized && !await WindowIsMax() && current()) await WindowToggleMax()
        }
        if (current()) await exitBrowserFullscreen(root)
      },
      async release(root: HTMLElement | undefined, unowned: () => boolean): Promise<void> {
        if (!unowned()) return
        await exitBrowserFullscreen(root)
        if (nativeWindow && unowned() && await WindowIsFs() && unowned()) await WindowSetFullscreen(false)
      },
    }
}
