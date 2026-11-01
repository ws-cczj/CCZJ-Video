import { nextTick, onBeforeUnmount, ref, watch, type Ref } from 'vue'

export interface NovelPageTurn {
  current: HTMLElement
  target: HTMLElement | null
  commit: () => boolean | Promise<boolean>
  releaseBase?: () => void
}

/** Whole-page cover, independent of text fragmentation. At most two inert DOM
 * faces exist, only during a gesture/transition; no second reader is mounted. */
export function useNovelPageTurn(host:Ref<HTMLElement|null>,duration:()=>number,prepare:(direction:number)=>NovelPageTurn|null,fallback:(direction:number)=>void,onError:(error:unknown)=>void) {
  const active=ref(false),settling=ref(false),committing=ref(false)
  let state: {faces:NovelPageTurn;direction:number;width:number;distance:number;velocity:number;reversed:number;last:number;time:number;moving:HTMLElement;warmed:boolean;warm:Promise<void>;finishWarm:()=>void}|null=null
  let generation=0,drawFrame=0,paintFrame=0,warmFrame=0,paintDone:(()=>void)|undefined,animation:Animation|undefined
  let observer:ResizeObserver|undefined
  function cancel():void {
    generation++;cancelAnimationFrame(drawFrame);cancelAnimationFrame(paintFrame);cancelAnimationFrame(warmFrame);paintDone?.();paintDone=undefined
    state?.finishWarm();state?.faces.releaseBase?.();animation?.cancel();animation=undefined;state=null
    if(host.value){host.value.replaceChildren();host.value.style.opacity=''}
    active.value=false;settling.value=false;committing.value=false
  }
  function stage(direction:number,faces:NovelPageTurn):void {
    const el=host.value
    if(!el)return
    const width=el.clientWidth
    if(width<1)return
    const moving=direction>0||!faces.target?faces.current:faces.target
    for(const face of [faces.current,faces.target])if(face){
      face.inert=true;face.setAttribute('aria-hidden','true');face.style.visibility='visible'
      face.classList.add('novel-page-face');face.style.transform='translate3d(0px,0,0)'
    }
    if(faces.target){moving.classList.add('novel-page-cover');if(direction<0)moving.style.transform=`translate3d(${-width}px,0,0)`}
    // Keep the real page visible while new column fragments and compositor
    // layers paint. Exposing a fresh clone and moving it in the same frame can
    // reveal an unpainted surface in Android WebView.
    el.style.opacity='.001'
    el.replaceChildren(...(direction>0?[faces.target,faces.current]:[faces.current,faces.target]).filter((face):face is HTMLElement=>!!face))
    for(const face of [faces.current,faces.target])face?.getBoundingClientRect()
    let finishWarm=()=>{}
    const warm=new Promise<void>(resolve=>{finishWarm=resolve})
    state={faces,direction,width,distance:0,velocity:0,reversed:0,last:0,time:performance.now(),moving,warmed:false,warm,finishWarm}
    active.value=true
    const current=state,token=generation
    warmFrame=requestAnimationFrame(()=>{warmFrame=requestAnimationFrame(()=>{
      if(token!==generation||state!==current)return
      current.warmed=true;el.style.opacity='1';current.moving.style.transform=`translate3d(${position(current.distance)}px,0,0)`;faces.releaseBase?.();finishWarm()
    })})
  }
  function position(distance:number):number {
    if(!state||!state.faces.target)return 0
    return state.direction>0?Math.max(-state.width,Math.min(0,distance)):Math.max(-state.width,Math.min(0,distance-state.width))
  }
  function drag(distance:number):void {
    if(settling.value||Math.abs(distance)<1)return
    const direction=distance<0?1:-1
    if(!state||state.direction!==direction){
      cancel();const faces=prepare(direction);if(!faces)return;stage(direction,faces)
      if(state){state.last=distance;state.time=performance.now()}
    }
    if(!state)return
    const now=performance.now(),elapsed=now-state.time
    const delta=distance-state.last
    state.reversed=-state.direction*delta<0?state.reversed+Math.abs(delta):0
    state.velocity=elapsed>0?state.velocity*.25+delta/elapsed*.75:0;state.last=distance;state.time=now
    state.distance=distance
    if(!drawFrame)drawFrame=requestAnimationFrame(()=>{drawFrame=0;if(state?.warmed)state.moving.style.transform=`translate3d(${position(state.distance)}px,0,0)`})
  }
  async function move(to:number,token:number):Promise<void> {
    if(!state||!state.faces.target)return
    cancelAnimationFrame(drawFrame);drawFrame=0
    const from=position(state.distance),el=state.moving
    const remaining=Math.abs(to-from),ms=remaining<.5?0:duration()*Math.max(.25,Math.min(1,remaining/state.width))
    el.style.transform=`translate3d(${to}px,0,0)`
    if(!ms||typeof el.animate!=='function')return
    const running=el.animate([{transform:`translate3d(${from}px,0,0)`},{transform:`translate3d(${to}px,0,0)`}],{duration:ms,easing:'cubic-bezier(.2,.65,.3,1)'})
    animation=running
    try{await running.finished}catch{/* Cancellation invalidates this gesture. */}
    if(token===generation)animation=undefined
  }
  async function finish(complete:boolean):Promise<void> {
    const current=state
    if(!current||settling.value)return
    settling.value=true
    const token=generation,{direction,width,faces}=current
    await current.warm
    if(token!==generation)return
    await move(complete?(direction>0?-width:0):(direction>0?0:-width),token)
    if(token!==generation)return
    if(complete&&faces.target){
      committing.value=true
      let accepted=false
      try{accepted=await faces.commit()}catch(error){if(token===generation)onError(error)}
      if(token!==generation)return
      committing.value=false
      if(!accepted){
        current.distance=direction>0?-width:width
        await move(direction>0?0:-width,token)
      }
      await nextTick()
      if(token!==generation)return
      await new Promise<void>(resolve=>{paintDone=resolve;paintFrame=requestAnimationFrame(()=>{paintDone=undefined;resolve()})})
    }
    if(token!==generation)return
    const needsLoad=complete&&!faces.target
    cancel()
    if(needsLoad)fallback(direction)
  }
  function release(abort=false):void {
    if(!state)return
    const age=performance.now()-state.time
    const distance=-state.direction*state.distance,velocity=age>100?0:-state.direction*state.velocity
    // A reversal near release returns the sheet even after a long initial drag.
    const complete=!abort&&!(state.reversed>=12&&velocity<-.12)&&(distance>=Math.min(96,state.width*.24)||distance>12&&velocity>.35)
    void finish(complete)
  }
  function turn(direction:number,faces?:NovelPageTurn):boolean {
    if(active.value)return true
    const prepared=faces??prepare(direction)
    if(!prepared?.target)return false
    stage(direction,prepared)
    if(!state)return false
    void finish(true)
    return true
  }
  watch(host,el=>{
    observer?.disconnect()
    if(!el)return
    let width=el.clientWidth,height=el.clientHeight
    observer=new ResizeObserver(()=>{
      if(width!==el.clientWidth||height!==el.clientHeight){width=el.clientWidth;height=el.clientHeight;cancel()}
    })
    observer.observe(el)
  },{flush:'post'})
  onBeforeUnmount(()=>{observer?.disconnect();cancel()})
  return {active,settling,committing,drag,release,turn,cancel}
}
