import { nextTick } from 'vue'
import { prepareThemeReveal } from '../api/themeReveal'
import { readRgb } from '../utils/colors'

export interface RevealOrigin { x: number; y: number; radius?: number }
let activeReveal: (()=>void) | undefined
const colorToken=/^--(?:bg-|text-|accent|border|btn-|tag-|episode-|cczj-mobile-|success|warning|danger|info|carousel-)/
function captureColors(root:HTMLElement):Record<string,string> {
  const result:Record<string,string>={},style=getComputedStyle(root)
  const colors=Array.from(root.style).filter(key=>colorToken.test(key))
    .map(key=>[key,style.getPropertyValue(key).trim()] as const)
    .filter(([,value])=>/^(?:#[\da-f]{3}(?:[\da-f]{3})?|rgba?\([\d.,\s]+\))$/i.test(value))
  for(const [key,value] of colors.slice(0,96))result[key]=value
  return result
}
function channels(color:string):number[]{
  const alpha=/^rgba\([^,]+,[^,]+,[^,]+,\s*([\d.]+)\s*\)$/i.exec(color)
  return [...readRgb(color),alpha?Number(alpha[1]):1]
}

/** One bounded DOM snapshot transition. Persistence remains with the theme store. */
export function createThemeReveal() {
  let revision = 0
  let transition: ViewTransition | undefined
  let animation: Animation | undefined
  let restoreSurface: (()=>void) | undefined
  let canvasAnimation:Animation|undefined
  let nativeReveal:ReturnType<typeof prepareThemeReveal>
  let stopInterpolation:(()=>void)|undefined

  function stop(): void {
    nativeReveal?.cancel();nativeReveal=undefined
    stopInterpolation?.();stopInterpolation=undefined
    canvasAnimation?.cancel();canvasAnimation=undefined
    animation?.cancel()
    animation = undefined
    restoreSurface?.();restoreSurface=undefined
    transition?.skipTransition()
    transition = undefined
    if(activeReveal===stop){activeReveal=undefined;document.documentElement.removeAttribute('data-cczj-theme-reveal')}
    document.removeEventListener('visibilitychange', hidden)
    window.removeEventListener('pagehide', stop)
    window.removeEventListener('resize', stop)
    window.removeEventListener('hashchange',stop)
  }
  function hidden(): void { if (document.hidden) stop() }

  async function run(update: () => void, origin: RevealOrigin | undefined, duration: number, surface?:HTMLElement): Promise<void> {
    const token = ++revision
    const root=document.documentElement
    const fallback=typeof document.startViewTransition!=='function'
    const interpolate=fallback&&!!origin&&duration>1&&!document.hidden
    const from=interpolate&&!surface?captureColors(root):{}
    const previous=interpolate&&surface?getComputedStyle(surface):undefined
    const surfaceFrom=previous?{backgroundColor:previous.backgroundColor,color:previous.color}:undefined
    stop()
    activeReveal?.()
    if (!origin || duration <= 1 || document.hidden) {
      update()
      return
    }
    if(fallback){
      if(!interpolate){update();return}
      activeReveal=stop
      root.setAttribute('data-cczj-theme-reveal','')
      document.addEventListener('visibilitychange',hidden)
      window.addEventListener('pagehide',stop)
      window.addEventListener('resize',stop)
      window.addEventListener('hashchange',stop)
      try{
        nativeReveal=prepareThemeReveal(origin.x,origin.y,origin.radius??0,surface?.getBoundingClientRect())
        if(nativeReveal && await nativeReveal.ready){
          if(token!==revision)return
          if(activeReveal!==stop){update();return}
          update()
          await nextTick()
          if(token!==revision||activeReveal!==stop)return
          nativeReveal.play(duration)
          await nativeReveal.done
          return
        }
        if(token!==revision)return
        if(activeReveal!==stop){update();return}
        update()
        await nextTick()
        if(token!==revision||activeReveal!==stop)return
        const timing={duration,easing:'cubic-bezier(.4,0,.2,1)'}
        if(surface&&surfaceFrom){
          const next=getComputedStyle(surface)
          animation=surface.animate([surfaceFrom,{backgroundColor:next.backgroundColor,color:next.color}],timing)
        }else{
          const to=captureColors(root)
          // Inline roles work in WebViews without typed custom properties.
          // Read once, then write only changed roles; never walk the page per frame.
          const roles=Object.keys(to).filter(key=>from[key]&&from[key]!==to[key]).map(key=>({key,from:channels(from[key]),to:channels(to[key]),written:''}))
          let frame=0,settled=false
          const completed=new Promise<void>(resolve=>{
            const finish=()=>{
              if(settled)return
              settled=true;cancelAnimationFrame(frame)
              for(const role of roles)if(root.style.getPropertyValue(role.key)===role.written)root.style.setProperty(role.key,to[role.key])
              resolve()
            }
            stopInterpolation=finish
            const began=performance.now()
            const draw=(now:number)=>{
              const progress=Math.min(1,Math.max(0,(now-began)/duration))
              const eased=1-(1-progress)**3
              for(const role of roles){
                const values=role.from.map((value,index)=>value+(role.to[index]-value)*eased)
                role.written=`rgba(${values.slice(0,3).map(Math.round).join(', ')}, ${Number(values[3].toFixed(4))})`
                root.style.setProperty(role.key,role.written)
                role.written=root.style.getPropertyValue(role.key)
              }
              if(progress<1)frame=requestAnimationFrame(draw)
              else finish()
            }
            draw(began)
          })
          animation=root.animate([
            {backgroundColor:from['--bg-primary'],color:from['--text-primary']},
            {backgroundColor:to['--bg-primary'],color:to['--text-primary']},
          ],timing)
          canvasAnimation=document.body.animate([
            {backgroundColor:from['--bg-primary'],color:from['--text-primary']},
            {backgroundColor:to['--bg-primary'],color:to['--text-primary']},
          ],timing)
          await completed
        }
        await animation.finished
      }catch{/* The selected palette remains authoritative if interpolation fails. */}
      finally{if(token===revision&&activeReveal===stop)stop()}
      return
    }
    const bounds=surface?.getBoundingClientRect()
    const width=bounds?.width??innerWidth,height=bounds?.height??innerHeight
    const x=Math.max(0,Math.min(width,origin.x-(bounds?.left??0))),y=Math.max(0,Math.min(height,origin.y-(bounds?.top??0)))
    const radius = Math.hypot(Math.max(x,width-x),Math.max(y,height-y))
    // Theme cards retain their own sample colours. Reveal beyond that card
    // immediately so the first frame also exposes the surrounding appearance.
    const startRadius = Math.min(radius, Math.max(0, origin.radius ?? 0))
    const snapshot=surface?'motion-preview-surface':'root'
    if(surface){const previous=surface.style.viewTransitionName;surface.style.viewTransitionName=snapshot;restoreSurface=()=>{surface.style.viewTransitionName=previous}}
    activeReveal=stop
    root.setAttribute('data-cczj-theme-reveal', '')
    document.addEventListener('visibilitychange', hidden)
    window.addEventListener('pagehide', stop)
    window.addEventListener('resize', stop)
    window.addEventListener('hashchange',stop)
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
      animation = root.animate({ clipPath: [`circle(${startRadius}px at ${x}px ${y}px)`, `circle(${radius}px at ${x}px ${y}px)`] }, {
        duration, easing: 'cubic-bezier(.4,0,.2,1)', pseudoElement: `::view-transition-new(${snapshot})`,
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
