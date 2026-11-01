import { nextTick } from 'vue'

export interface RevealOrigin { x: number; y: number }
let activeReveal: (()=>void) | undefined

/** One bounded DOM snapshot transition. Persistence remains with the theme store. */
export function createThemeReveal() {
  let revision = 0
  let transition: ViewTransition | undefined
  let animation: Animation | undefined
  let restoreSurface: (()=>void) | undefined

  function stop(): void {
    animation?.cancel()
    animation = undefined
    restoreSurface?.();restoreSurface=undefined
    transition?.skipTransition()
    transition = undefined
    if(activeReveal===stop){activeReveal=undefined;document.documentElement.removeAttribute('data-cczj-theme-reveal')}
    document.removeEventListener('visibilitychange', hidden)
    window.removeEventListener('pagehide', stop)
    window.removeEventListener('resize', stop)
  }
  function hidden(): void { if (document.hidden) stop() }

  async function run(update: () => void, origin: RevealOrigin | undefined, duration: number, surface?:HTMLElement): Promise<void> {
    const token = ++revision
    stop()
    activeReveal?.()
    const root = document.documentElement
    if (!origin || duration <= 1 || document.hidden || typeof document.startViewTransition !== 'function') {
      update()
      return
    }
    const bounds=surface?.getBoundingClientRect()
    const width=bounds?.width??innerWidth,height=bounds?.height??innerHeight
    const x=Math.max(0,Math.min(width,origin.x-(bounds?.left??0))),y=Math.max(0,Math.min(height,origin.y-(bounds?.top??0)))
    const radius = Math.hypot(Math.max(x,width-x),Math.max(y,height-y))
    const snapshot=surface?'motion-preview-surface':'root'
    if(surface){const previous=surface.style.viewTransitionName;surface.style.viewTransitionName=snapshot;restoreSurface=()=>{surface.style.viewTransitionName=previous}}
    activeReveal=stop
    root.setAttribute('data-cczj-theme-reveal', '')
    document.addEventListener('visibilitychange', hidden)
    window.addEventListener('pagehide', stop)
    window.addEventListener('resize', stop)
    let applied = false
    try {
      const active = document.startViewTransition(async () => {
        if (token !== revision) return
        update()
        applied = true
        await nextTick()
      })
      transition = active
      // Skipping a visual transition still executes its update callback.
      void active.updateCallbackDone.catch(() => {})
      void active.finished.catch(() => {})
      await active.ready
      if (token !== revision || transition !== active) return
      animation = root.animate({ clipPath: [`circle(0px at ${x}px ${y}px)`, `circle(${radius}px at ${x}px ${y}px)`] }, {
        duration, easing: 'cubic-bezier(.2,0,0,1)', pseudoElement: `::view-transition-new(${snapshot})`,
      })
      await animation.finished
      await active.finished
    } catch {
      // Unsupported snapshots, interruption and Web Animations failures must not
      // prevent the selected appearance from being applied.
      if (token === revision && !applied) update()
    } finally {
      if (token === revision) stop()
    }
  }

  return { run, cancel: stop, dispose: () => { ++revision; stop() } }
}
