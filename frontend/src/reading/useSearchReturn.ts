import { nextTick,onActivated,onDeactivated,type Ref } from 'vue'
/** KeepAlive retains one result page and its cursor; it is not a persistent
 * search index. Cancel work while away and expire the snapshot after 15 min. */
export function useSearchReturn(root:Ref<HTMLElement|null>,options:{loading:()=>boolean;cancel:()=>void;refresh:()=>unknown},ttl=15*60*1000){
 let capturedAt=0,scrollTop=0
 onDeactivated(()=>{
  scrollTop=root.value?.closest<HTMLElement>('.main-content')?.scrollTop??0
  if(options.loading())options.cancel()
 })
 onActivated(()=>{
  if(capturedAt&&Date.now()-capturedAt>ttl){void options.refresh();return}
  void nextTick(()=>{const el=root.value?.closest<HTMLElement>('.main-content');if(el)el.scrollTop=scrollTop})
 })
 return {capture:()=>{capturedAt=Date.now()},fresh:()=>!!capturedAt&&Date.now()-capturedAt<=ttl}
}
