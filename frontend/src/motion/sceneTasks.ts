import { inject, nextTick, onScopeDispose, watch, type InjectionKey, type Ref } from 'vue'

export const sceneMotionKey:InjectionKey<Readonly<Ref<boolean>>>=Symbol('scene-motion')

/** The route owner publishes completion; retained views never inspect animations. */
export function createSceneSettledTask(active:()=>boolean) {
  const moving=inject(sceneMotionKey,undefined)
  let disposed=false
  let revision=0,frame:number|undefined,deadline:ReturnType<typeof setTimeout>|undefined,release:(()=>void)|undefined,stopWaiting:(()=>void)|undefined
  function cancel():void {
    revision++
    if(frame!==undefined)cancelAnimationFrame(frame)
    if(deadline!==undefined)clearTimeout(deadline)
    stopWaiting?.();stopWaiting=undefined
    frame=undefined;deadline=undefined
    const finish=release;release=undefined;finish?.()
  }
  async function schedule(task:()=>void|Promise<void>):Promise<void> {
    if(disposed)return
    cancel()
    const token=revision
    await nextTick()
    if(token!==revision||!active())return
    await new Promise<void>(resolve=>{
      release=resolve
      frame=requestAnimationFrame(()=>{frame=undefined;release=undefined;resolve()})
    })
    if(token!==revision||!active())return
    if(moving?.value)await new Promise<void>(resolve=>{
      const finish=()=>{if(token!==revision)return;stopWaiting?.();stopWaiting=undefined;if(deadline!==undefined)clearTimeout(deadline);deadline=undefined;release=undefined;resolve()}
      release=resolve
      stopWaiting=watch(moving,value=>{if(!value)finish()},{flush:'post'})
      deadline=setTimeout(finish,2000)
    })
    if(token===revision&&active())await task()
  }
  watch(active,enabled=>{if(!enabled)cancel()},{flush:'sync'})
  onScopeDispose(()=>{disposed=true;cancel()})
  return {schedule,cancel}
}
