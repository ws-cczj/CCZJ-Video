import { ref } from 'vue'
import { isMobileRuntime } from '../api/runtime'
import { markStartup } from '../diagnostics/startup'
import { prepareWallpaperImage, readThemeImageSource } from '../utils/themeImage'

const DESKTOP_WALLPAPER_EDGE = 2048
const MOBILE_WALLPAPER_EDGE = 1440

/** Own one displayed image and one replacement. Original data URLs remain in
 * durable theme data; CSS receives short object URLs within engine limits. */
export function createThemeWallpaper(commit:(url:string|undefined)=>void,failed:(error:unknown)=>void) {
  const url=ref<string>(),loading=ref(false),error=ref(false)
  let source:string|undefined,displayedSource:string|undefined,objectURL:string|undefined,abort:AbortController|undefined
  let image:HTMLImageElement|undefined,revision=0,disposed=false,succeeded=true
  let pending:Promise<boolean>=Promise.resolve(true)
  function prepare(next:string|undefined,retry=false):Promise<boolean> {
    if(disposed)return Promise.resolve(false)
    if(next===source&&(!retry||loading.value||succeeded))return pending
    source=next;abort?.abort();if(image){image.src='';image=undefined}
    const token=++revision,controller=new AbortController();abort=controller
    loading.value=!!next;succeeded=false;error.value=false
    if(next&&next===displayedSource&&url.value){loading.value=false;succeeded=true;abort=undefined;commit(url.value);return pending=Promise.resolve(true)}
    if(!next){
      commit(undefined);url.value=undefined
      if(objectURL)URL.revokeObjectURL(objectURL)
      objectURL=undefined;displayedSource=undefined;loading.value=false;succeeded=true;abort=undefined
      return pending=Promise.resolve(true)
    }
    pending=(async()=>{
      let candidate:string|undefined
      let timedOut=false
      const began=performance.now()
      const timer=setTimeout(()=>{timedOut=true;controller.abort()},30000)
      try {
        let original:Blob|undefined
        try {
          original=await readThemeImageSource(next,controller.signal)
        } catch (cause) {
          // Some legacy remote wallpapers can render directly even when their
          // server and the image proxy do not allow reading the image bytes.
          const remote=new URL(next,document.baseURI)
          if(controller.signal.aborted||!['http:','https:'].includes(remote.protocol)||remote.origin===location.origin)throw cause
        }
        if(original){
          const edge=isMobileRuntime()?MOBILE_WALLPAPER_EDGE:DESKTOP_WALLPAPER_EDGE
          const prepared=await prepareWallpaperImage(original,controller.signal,edge)
          candidate=URL.createObjectURL(prepared)
          original=undefined
        }
        if(controller.signal.aborted)throw new DOMException('Cancelled','AbortError')
        const target=new Image();image=target;target.decoding='async';target.src=candidate??next
        await new Promise<void>((resolve,reject)=>{
          const cancel=()=>reject(new DOMException('Cancelled','AbortError'))
          controller.signal.addEventListener('abort',cancel,{once:true})
          void target.decode().then(resolve,reject).finally(()=>controller.signal.removeEventListener('abort',cancel))
        })
        if(disposed||token!==revision||controller.signal.aborted)return false
        const previous=objectURL
        objectURL=candidate;candidate=undefined
        displayedSource=next;url.value=objectURL??next;commit(url.value);succeeded=true
        markStartup('wallpaper_decode',performance.now()-began)
        if(previous)URL.revokeObjectURL(previous)
        return true
      }catch(cause){if(!disposed&&token===revision){error.value=true;markStartup('wallpaper_decode',performance.now()-began,true);failed(timedOut?new DOMException('Image loading timed out','TimeoutError'):cause)}return false}
      finally{
        clearTimeout(timer)
        if(candidate)URL.revokeObjectURL(candidate)
        if(token===revision){loading.value=false;if(image)image.src='';image=undefined;abort=undefined}
      }
    })()
    return pending
  }
  function pageHide(event:PageTransitionEvent):void {if(!event.persisted)dispose()}
  if(typeof window!=='undefined')window.addEventListener('pagehide',pageHide)
  function dispose():void {
    if(disposed)return
    disposed=true;revision++;abort?.abort();if(image)image.src=''
    if(objectURL)URL.revokeObjectURL(objectURL)
    if(typeof window!=='undefined')window.removeEventListener('pagehide',pageHide)
    objectURL=undefined;image=undefined;abort=undefined;source=undefined;displayedSource=undefined;url.value=undefined;loading.value=false;error.value=false;commit(undefined)
  }
  function urlFor(next:string|undefined):string|undefined {
    const current=url.value
    return displayedSource===next?current:undefined
  }
  return {url,urlFor,loading,error,prepare,dispose}
}
