import { ref } from 'vue'

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
      const timer=setTimeout(()=>{timedOut=true;controller.abort()},30000)
      try {
        if(next.startsWith('data:')){
          const response=await fetch(next,{signal:controller.signal})
          candidate=URL.createObjectURL(await response.blob())
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
        if(previous)URL.revokeObjectURL(previous)
        return true
      }catch(cause){if(!disposed&&token===revision){error.value=true;failed(timedOut?new DOMException('Image loading timed out','TimeoutError'):cause)}return false}
      finally{
        clearTimeout(timer)
        if(candidate)URL.revokeObjectURL(candidate)
        if(token===revision){loading.value=false;if(image)image.src='';image=undefined;abort=undefined}
      }
    })()
    return pending
  }
  function pageHide(event:PageTransitionEvent):void {if(!event.persisted)dispose()}
  window.addEventListener('pagehide',pageHide)
  function dispose():void {
    if(disposed)return
    disposed=true;revision++;abort?.abort();if(image)image.src=''
    if(objectURL)URL.revokeObjectURL(objectURL)
    window.removeEventListener('pagehide',pageHide)
    objectURL=undefined;image=undefined;abort=undefined;source=undefined;displayedSource=undefined;url.value=undefined;loading.value=false;error.value=false;commit(undefined)
  }
  return {url,loading,error,prepare,dispose}
}
