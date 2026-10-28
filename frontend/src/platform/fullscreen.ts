import { WindowIsFs, WindowIsMax, WindowSetFullscreen, WindowToggleMax } from '../api/app'
import { presentationProfile } from './presentation'
import { isIOSRuntime } from '../api/runtime'
import { prepareDeviceFullscreen, type FullscreenOrientation } from '../api/mobilePlayer'

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
 const host=presentationProfile().host
 const nativeWindow=host==='native-desktop'
 const viewport=isIOSRuntime()
 let viewportRoot:HTMLElement|undefined
 let wasMaximized=false
 let nativeRequest: ReturnType<typeof prepareDeviceFullscreen>
 let reportError: ((error: unknown) => void) | undefined
 return {
      kind: nativeWindow ? 'window' as const : viewport ? 'viewport' as const : 'element' as const,
      supported: (root?:HTMLElement) => nativeWindow || (!!root && (viewport ||
        (document.fullscreenEnabled !== false && typeof root.requestFullscreen === 'function') || typeof (root as FullscreenRoot).webkitRequestFullscreen === 'function')),
      browserActive: (root?:HTMLElement) => viewport ? !!root && viewportRoot===root : browserFullscreen(root),
      onError(listener: (error: unknown) => void): () => void {
        reportError=listener
        return () => { if(reportError===listener)reportError=undefined }
      },
      setLocked(root: HTMLElement | undefined, locked: boolean, current: () => boolean, orientation: FullscreenOrientation = 'preserve'): void {
        if(current()&&browserFullscreen(root))nativeRequest?.lock(orientation,locked)
      },
      async enter(root: HTMLElement | undefined, current: () => boolean, orientation: FullscreenOrientation = 'preserve'): Promise<boolean> {
        if (nativeWindow) {
          try { wasMaximized = await WindowIsMax() } catch { wasMaximized = false }
          if (!current()) return false
          await WindowSetFullscreen(true)
          return true
        }
        if (!root || !current()) return false
        // UIKit already owns the full screen. Existing player/reader classes
        // provide immersion inside its safe area without desktop window calls.
        if (viewport) { viewportRoot=root; return true }
        const element = root as FullscreenRoot
        const request = element.requestFullscreen || element.webkitRequestFullscreen
        if (!request) return false
        const native = host==='native-mobile' ? prepareDeviceFullscreen(orientation,error=>reportError?.(error)) : undefined
        nativeRequest=native
        try {
          const requested=request.call(element)
          if(native)await Promise.all([requested,native.ready])
          else await requested
          if(!current()||!browserFullscreen(root)) {
            await exitBrowserFullscreen(root)
            native?.release()
            if(nativeRequest===native)nativeRequest=undefined
            return false
          }
          return true
        } catch(error) {
          try { await exitBrowserFullscreen(root) }
          finally { native?.release(); if(nativeRequest===native)nativeRequest=undefined }
          throw error
        }
      },
      async exit(root: HTMLElement | undefined, current: () => boolean): Promise<void> {
        if (!current()) return
        if (viewport) { viewportRoot=undefined; return }
        if (nativeWindow) {
          await WindowSetFullscreen(false)
          if (current() && wasMaximized && !await WindowIsMax() && current()) await WindowToggleMax()
        }
        if (current()) {
          await exitBrowserFullscreen(root)
          nativeRequest?.release()
          nativeRequest=undefined
        }
      },
      async release(root: HTMLElement | undefined, unowned: () => boolean): Promise<void> {
        // Native release is scoped by request ID even if a new owner exists.
        nativeRequest?.release()
        nativeRequest=undefined
        if (!unowned()) return
        if (viewport) { viewportRoot=undefined; return }
        await exitBrowserFullscreen(root)
        if (nativeWindow && unowned() && await WindowIsFs() && unowned()) await WindowSetFullscreen(false)
      },
    }
}
