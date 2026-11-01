import { onBeforeUnmount, watch } from 'vue'
import { novelBookSources } from '../api/readingSources'
import { useAsyncResource } from './asyncResource'

/** Cache paints first; the cancellable background call owns its own result and
 * never replaces the current book with late data from another detail page. */
export function useNovelSourceCache(item:()=>number) {
  const cached=useAsyncResource({load:(id:number)=>novelBookSources(id)})
  const probe=useAsyncResource({load:(id:number)=>novelBookSources(id,true),onData:value=>{if(value.itemId===item())void cached.run(value.itemId)}})
  let disposed=false,frame=0,generation=0
  watch(item,async id=>{
    const token=++generation
    cancelAnimationFrame(frame);cached.cancel();probe.cancel();cached.data.value=null
    if(!Number.isInteger(id)||id<1)return
    await cached.run(id)
    if(disposed||token!==generation||!cached.data.value)return
    frame=requestAnimationFrame(()=>{frame=requestAnimationFrame(()=>{if(!disposed&&token===generation)void probe.run(id)})})
  },{immediate:true})
  onBeforeUnmount(()=>{disposed=true;generation++;cancelAnimationFrame(frame)})
  return {cached,probe}
}
