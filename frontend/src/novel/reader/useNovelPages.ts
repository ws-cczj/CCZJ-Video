import { computed, onBeforeUnmount, ref, watch, type Ref } from 'vue'
import { measuredPageCount, pageFromRatio, ratioFromPage } from './pagination'

/** CSS fragments real text into columns; no character-count approximation. */
export function useNovelPages(viewport:Ref<HTMLElement|null>,track:Ref<HTMLElement|null>,enabled:()=>boolean=()=>true) {
  const page=ref(0),count=ref(1),width=ref(0),gap=40
  const ratio=computed(()=>ratioFromPage(page.value,count.value))
  const transform=computed(()=>`translate3d(${-page.value*(width.value+gap)}px,0,0)`)
  let observer:ResizeObserver|undefined,frame=0,disposed=false
  function measure(position=ratio.value):void {
    const el=viewport.value,body=track.value
    if(!enabled() || !el || !body || el.clientWidth<1 || el.clientHeight<1)return
    width.value=el.clientWidth
    body.style.columnWidth=width.value+'px';body.style.columnGap=gap+'px'
    count.value=measuredPageCount(body.scrollWidth,width.value,gap)
    page.value=pageFromRatio(position,count.value)
  }
  function refresh(position?:number):void { cancelAnimationFrame(frame);frame=requestAnimationFrame(()=>{if(!disposed)measure(position??ratio.value)}) }
  function select(next:number):void { page.value=Math.max(0,Math.min(count.value-1,next)) }
  // The viewport is created only after asynchronous chapter loading, and is
  // replaced during chapter changes. onMounted alone sees a null element.
  watch(viewport,element=>{
    observer?.disconnect()
    if(element){observer??=new ResizeObserver(()=>{if(enabled())refresh()});observer.observe(element)}
  },{flush:'post'})
  onBeforeUnmount(()=>{disposed=true;cancelAnimationFrame(frame);observer?.disconnect()})
  return {page,count,ratio,transform,measure,refresh,select}
}
