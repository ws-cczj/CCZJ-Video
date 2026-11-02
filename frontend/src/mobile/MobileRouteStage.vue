<script setup lang="ts">
import { computed, defineComponent, h, nextTick, onBeforeUnmount, onMounted, provide, ref, shallowReactive, shallowRef, watch, type Component, type PropType, type Slot, type VNode } from 'vue'
import { useRoute,useRouter,loadRouteLocation,routeLocationKey,routerViewLocationKey,type RouteLocationNormalizedLoaded } from 'vue-router'
import { primaryTabs,routeTransition,routeScreenKey } from './routeMotion'
import { useTabSwipe } from './useTabSwipe'
import { routeBackdropFocus, routeBackdropFrame } from './routeBackdrop'
import { useBackdropSystemBars } from '../platform/backdropSystemBars'
import { routeVisualActivityKey, routeWorkActivityKey, routeVisualMotionKey } from '../composables/useViewActivity'
import { sceneMotionKey } from '../motion/sceneTasks'
import { useMotionStore } from '../stores/motion'
import { captureDiagnosticContext, recordTimeline, type DiagnosticContext } from '../diagnostics/session'
const props=withDefaults(defineProps<{mobile:boolean;cache?:string[];wallpaper?:boolean;sections?:readonly string[]}>(),{cache:()=>[],sections:()=>[]})
const secondaryCache=computed(()=>props.mobile?[...props.cache,'Detail']:props.cache)
const router=useRouter(),route=useRoute(),transitionName=ref('cczj-mobile-route-forward')
const motion=useMotionStore()
let visualNavigation={began:performance.now(),context:captureDiagnosticContext()}
const initialScreen=routeScreenKey(route)
const primary=(path:string,query:RouteLocationNormalizedLoaded['query']={})=>props.mobile&&!(['/settings','/novel/settings','/comic/settings'].includes(path)&&query.group)&&primaryTabs.some(tabs=>tabs.includes(path))
const entries=shallowReactive(new Map<string,RouteLocationNormalizedLoaded>())
const primaryEntries=computed(()=>Array.from(entries.values()))
const secondary=shallowRef<RouteLocationNormalizedLoaded>({...route})
const secondaryInstanceKey=ref(routeScreenKey(route))
const settling=ref(false),committing=ref(false),dragFrom=ref(''),previewPath=ref(''),previewEdge=ref(1)
let offset=0,dragFrame:number|undefined
let gestureWidth=1
const dragging=computed(()=>!!dragFrom.value)
const movingScenes=shallowReactive(new Set<Element>())
provide(sceneMotionKey,computed(()=>movingScenes.size>0||dragging.value||settling.value||committing.value))
const backdropElement=ref<HTMLElement>()
const viewportElement=ref<HTMLElement>()
const canvasStyle=shallowRef<Record<string,string>>({})
let canvasObserver:ResizeObserver|undefined,canvasFrame:number|undefined
function measureCanvas():void {
  if(canvasFrame!==undefined)return
  canvasFrame=requestAnimationFrame(()=>{
    canvasFrame=undefined
    const bounds=viewportElement.value?.getBoundingClientRect()
    if(!alive||!bounds)return
    canvasStyle.value={'--route-canvas-left':`${-bounds.left}px`,'--route-canvas-top':`${-bounds.top}px`,'--route-canvas-width':`${innerWidth}px`,'--route-canvas-height':`${innerHeight}px`}
  })
}
// Complete a peer slide before admitting another peer navigation. Newer
// requests replace waiting requests without restarting half-translated pages.
let releasePeerNavigation:((accept:boolean)=>void)|undefined
const removePeerGuard=router.beforeEach((to,from)=>{
  releasePeerNavigation?.(false)
  if(!props.mobile||dragging.value||committing.value||!primary(to.path,to.query)||!primary(from.path,from.query)||movingScenes.size===0)return true
  return new Promise<boolean>(resolve=>{
    let stopWaiting:()=>void=()=>{}
    const finish=(accept:boolean)=>{clearTimeout(deadline);stopWaiting();if(releasePeerNavigation===finish)releasePeerNavigation=undefined;resolve(accept)}
    const deadline=setTimeout(()=>finish(true),Math.min(2700,motion.tokenMs('--cczj-motion-slow')+150))
    releasePeerNavigation=finish
    stopWaiting=watch(()=>movingScenes.size,size=>{if(size===0)finish(true)},{flush:'post'})
  })
})
const bars=useBackdropSystemBars(backdropElement,()=>routeScreenKey(route),()=>route.name==='player')
function backdropStyle():Record<string,string> {
 const origin=routeBackdropFocus(route.path,String(route.query.group||''))
 const target=previewPath.value?routeBackdropFocus(previewPath.value):origin
 const fraction=dragging.value?Math.min(1,Math.abs(offset)/gestureWidth):0
 const x=origin.x+(target.x-origin.x)*fraction,y=origin.y+(target.y-origin.y)*fraction
 return routeBackdropFrame(route.path,{x,y})
}
const chapterChange=ref(false),readerChange=ref(false)
let previous={...route} as RouteLocationNormalizedLoaded,position=Number(router.options.history.state.position)||0
let revision=0,alive=true
let preparing:Promise<void>|undefined
let scrollFrame:number|undefined
let paintFrame:number|undefined,finishPaint:(()=>void)|undefined,finishSettle:(()=>void)|undefined
function cancelPaint():void {if(paintFrame!==undefined)cancelAnimationFrame(paintFrame);paintFrame=undefined;finishPaint?.();finishPaint=undefined}
function cancelDragFrame():void {if(dragFrame!==undefined)cancelAnimationFrame(dragFrame);dragFrame=undefined}
function reset():void {finishSettle?.();cancelDragFrame();revision++;dragFrom.value='';previewPath.value='';offset=0;settling.value=false;bars.finish()}
// Only geometry changes with the finger. Keep frame updates out of Vue's route
// render effect, which also owns the cached pages and their scoped slots.
function move(distance:number):void {
  offset=distance
  if(dragFrame!==undefined)return
  dragFrame=requestAnimationFrame(()=>{
    dragFrame=undefined
    if(!alive||!dragging.value||settling.value)return
    applyDragStyles()
  })
}
function applyDragStyles():void {
    const backdropTransform=backdropStyle().transform
    const children=viewportElement.value?.children
    for(let index=0;children&&index<children.length;index++){
      const element=children[index]
      if(!(element instanceof HTMLElement)||!element.classList.contains('cczj-mobile-route-scene'))continue
      const transform=tabStyle(element.dataset.path||'')?.transform
      if(transform!==undefined&&element.style.transform!==transform)element.style.transform=transform
    }
    const backdrop=backdropElement.value
    if(backdrop)backdrop.style.transform=backdropTransform
    if(!settling.value)bars.move(dragFrom.value,previewPath.value||dragFrom.value,Math.abs(offset)/gestureWidth)
}
function reducedPages():boolean {return !motion.allows('pages')}
async function preview(path:string,edge:number):Promise<void> {
  previewEdge.value=edge
  if(path===previewPath.value&&dragFrom.value)return preparing
  if(!dragFrom.value){gestureWidth=Math.max(1,innerWidth);dragFrom.value=route.path}
  previewPath.value=path;const token=++revision
  if(!path||entries.has(path)){preparing=undefined;return}
  const pending=loadRouteLocation(router.resolve(path)).then(async loaded=>{
    if(alive&&token===revision){entries.set(path,loaded);await nextTick()}
  }).catch(()=>{ /* A failed preview settles back to the current page. */ })
  preparing=pending
  await pending
  if(preparing===pending)preparing=undefined
}
async function settle(path?:string):Promise<void> {
  const token=revision
  cancelDragFrame()
  // Wait for the entering view's lazy import before committing movement so a
  // slow import cannot expose the empty canvas between scenes.
  if(path&&!entries.has(path))await preparing
  if(!alive||token!==revision)return
  if(path&&!entries.has(path))path=undefined
  await nextTick()
  if(!alive||token!==revision)return
  settling.value=true
  const width=gestureWidth
  const reduce=reducedPages()
  await nextTick()
  if(!alive||token!==revision)return
  offset=path?-previewEdge.value*width:0
  // The drag frame writes transforms directly. Its final position must use
  // that same owner rather than relying on Vue to detect an imperative write.
  applyDragStyles()
  const leaving=Array.from(viewportElement.value?.children||[]).find(element=>element instanceof HTMLElement&&element.dataset.path===dragFrom.value)
  if(!reduce&&leaving instanceof HTMLElement)bars.settle(dragFrom.value,previewPath.value||dragFrom.value,leaving,path?1:0,true)
  if(!reduce&&leaving instanceof HTMLElement)await new Promise<void>(resolve=>{
    const done=()=>{clearTimeout(deadline);leaving.removeEventListener('transitionend',ended);if(finishSettle===done)finishSettle=undefined;resolve()}
    const ended=(event:TransitionEvent)=>{if(event.target===leaving&&event.propertyName==='transform')done()}
    const deadline=setTimeout(done,400)
    finishSettle=done
    leaving.addEventListener('transitionend',ended)
  })
  // A Back/tab action during the settle animation supersedes this gesture.
  if(!alive||token!==revision)return
  committing.value=true
  try { if(path)await router.push(path) } catch { /* Leave the current page usable after a rejected navigation. */ }
  // Keep transitions disabled until both Vue and Chromium have committed the
  // destination at its final transform. Otherwise the reset can animate again.
  reset();const paintRevision=revision;await nextTick()
  cancelPaint()
  await new Promise<void>(resolve=>{finishPaint=resolve;paintFrame=requestAnimationFrame(()=>{paintFrame=requestAnimationFrame(()=>{paintFrame=undefined;finishPaint=undefined;resolve()})})})
  if(!alive||paintRevision!==revision)return
  committing.value=false
  notifyScroll()
}
const swipe=useTabSwipe(()=>props.mobile,{preview:(path,edge)=>void preview(path,edge),move,settle})
watch(router.currentRoute,()=>{
  visualNavigation={began:performance.now(),context:captureDiagnosticContext()}
  const fromPath=previous.path
  if(props.mobile&&!committing.value&&route.path!==fromPath)bars.begin(fromPath,route.path)
  // A chapter change keeps its reader and uses the reader's own page turn.
  chapterChange.value=/^\/(novel|comic)\/read\//.test(route.path)&&/^\/(novel|comic)\/read\//.test(previous.path)&&routeScreenKey(route)===routeScreenKey(previous)
  readerChange.value=chapterChange.value
  const next=Number(router.options.history.state.position)||0
  transitionName.value=routeTransition(previous,route,position,next,props.mobile,props.sections)
  if(!committing.value&&dragging.value&&route.path!==dragFrom.value)reset()
  if(primary(route.path,route.query))entries.set(route.path,{...route})
  else {
    // Replacing a verified playback source/line keeps the page's live handoff.
    // Push/back navigation still owns a separate route instance.
    if(!(previous.name==='player'&&route.name==='player'&&next===position))secondaryInstanceKey.value=routeScreenKey(route)
    secondary.value={...route}
  }
  previous={...route} as RouteLocationNormalizedLoaded;position=next
  if(props.mobile&&!committing.value)void nextTick(()=>{
    if(route.path===fromPath)return
    if(reducedPages()||readerChange.value){bars.finish();return}
    const entering=Array.from(viewportElement.value?.children||[]).find(element=>element instanceof HTMLElement&&element.dataset.path===route.path)
    if(entering instanceof HTMLElement)bars.settle(fromPath,route.path,entering)
  })
},{flush:'sync',immediate:true})
const name=computed(()=>transitionName.value)
const sceneTimings=new Map<Element,{began:number;context:DiagnosticContext;frame:number;mount:number;delay?:number}>()
function tabVisible(path:string):boolean {return (primary(route.path,route.query)&&route.path===path)||(dragging.value&&(path===previewPath.value||path===dragFrom.value))}
function tabStyle(path:string):Record<string,string>|undefined {
  if(!dragging.value)return
  if(path===dragFrom.value)return{transform:`translate3d(${offset}px,0,0)`}
  if(path===previewPath.value)return{transform:`translate3d(calc(${previewEdge.value*100}% + ${offset}px),0,0)`}
}
const definitions=new Map<VNode['type'],Component>()
// Pointer movement patches scene styles, but does not activate a page again.
const sceneActivity=new WeakMap<Element,boolean>()
const sceneVisibility=new WeakMap<Element,{value:boolean}>()
const leavingScenes=new WeakSet<Element>()
const seenEntrances=new WeakMap<Element,Set<string>>()
const entranceName=/^(?:cczj-fade-in(?:-up)?|cczj-reveal-scale|room-(?:sheet-turn|shelf-unfold|book-arrive)|manga-(?:cover-pop|panel-arrive))(?:-|$)/
const runningEntrances=new WeakMap<Element,Set<CSSAnimation>>()
function rememberAnimation(target:Element,name:string):void {
  const names=seenEntrances.get(target)||new Set<string>()
  names.add(name);seenEntrances.set(target,names)
}
function trackEntrance(event:AnimationEvent):void {
  if(!props.mobile||!(event.target instanceof Element)||!entranceName.test(event.animationName))return
  const target=event.target,root=target.closest('.cczj-mobile-route-scene')
  if(!root)return
  // animationstart is dispatched after style resolution. Query only this new
  // animation, never the retained tree during activation or deactivation.
  const running=runningEntrances.get(root)||new Set<CSSAnimation>()
  for(const animation of target.getAnimations()){
    if(!(animation instanceof CSSAnimation)||animation.animationName!==event.animationName)continue
    if(dragging.value||committing.value||seenEntrances.get(target)?.has(event.animationName)){
      if(Number.isFinite(animation.effect?.getComputedTiming().endTime))animation.finish()
      rememberAnimation(target,event.animationName)
    }else running.add(animation)
  }
  runningEntrances.set(root,running)
}
function rememberEntrance(event:AnimationEvent):void {
  if(!props.mobile||!(event.target instanceof Element)||!entranceName.test(event.animationName))return
  rememberAnimation(event.target,event.animationName)
  const root=event.target.closest('.cczj-mobile-route-scene')
  const running=root&&runningEntrances.get(root)
  if(running)for(const animation of running)if((animation.effect as KeyframeEffect|null)?.target===event.target&&animation.animationName===event.animationName)running.delete(animation)
}
function restoreEntrances(element:Element):void {
  const visible=sceneVisibility.get(element);if(visible)visible.value=true
}
function endSceneMotion(element:Element):void { movingScenes.delete(element) }
function beginEnter(element:Element):void { movingScenes.add(element);activate(element) }

function scene(view:VNode):Component {
  const existing=definitions.get(view.type);if(existing)return existing
  const source=view.type as {name?:string;__name?:string}
  const definition=defineComponent({
    name:source.name||source.__name||'MobileRoutePage',
    props:{view:{type:Object as PropType<VNode>,required:true},location:{type:Object as PropType<RouteLocationNormalizedLoaded>,required:true},frame:{type:Function as PropType<Slot>,required:true}},
    setup(sceneProps){
      const shown=shallowReactive({...sceneProps.location}) as RouteLocationNormalizedLoaded
      const visible=ref(true)
      provide(routeVisualActivityKey,visible)
      provide(routeVisualMotionKey,computed(()=>props.mobile&&(dragging.value||committing.value)))
      // Query updates change this screen's data without deactivating its work.
      provide(routeWorkActivityKey,computed(()=>routeScreenKey(router.currentRoute.value)===routeScreenKey(shown)))
      watch(()=>sceneProps.location,value=>Object.assign(shown,value),{flush:'sync'})
      provide(routeLocationKey,shown);provide(routerViewLocationKey,computed(()=>shown))
      return()=>{
        return h('div',{class:'cczj-mobile-route-scene','data-route-surface':primaryTabs.some(group=>group.includes(shown.path))&&!shown.query.group?'primary':'stack','data-path':shown.path,'data-screen':routeScreenKey(shown),'data-route-active':'true',onVnodeMounted:(node:VNode)=>{if(node.el instanceof Element)sceneVisibility.set(node.el,visible)}},[
          ...sceneProps.frame({Component:sceneProps.view,route:shown}),
        ])
      }
    },
  })
  definitions.set(view.type,definition);return definition
}
function notifyScroll():void {
  if(scrollFrame!==undefined||!alive)return
  scrollFrame=requestAnimationFrame(()=>{scrollFrame=undefined;if(alive)window.dispatchEvent(new Event('cczj:route-scroll-container'))})
}
function enterScene(element:Element):void {
  if(!props.mobile&&motion.allows('pages')){
    finishSceneTiming(element,true)
    const timing={...visualNavigation,mount:performance.now()-visualNavigation.began,frame:0,delay:undefined as number|undefined}
    sceneTimings.set(element,timing)
    timing.frame=requestAnimationFrame(()=>{timing.frame=0;timing.delay=performance.now()-timing.began})
  }
  restoreEntrances(element)
}
function finishSceneTiming(element:Element,cancelled=false):void {
  const timing=sceneTimings.get(element)
  if(timing){
    sceneTimings.delete(element);if(timing.frame)cancelAnimationFrame(timing.frame)
    recordTimeline('route',{name:'visual_transition',outcome:cancelled?'cancelled':'success',duration_ms:performance.now()-timing.began,data:{mount_ms:timing.mount,...(timing.delay===undefined?{}:{frame_delay_ms:timing.delay})}},timing.context)
  }
}
function enteredScene(element:Element):void {endSceneMotion(element);finishSceneTiming(element);notifyScroll()}
function cancelScene(element:Element):void {endSceneMotion(element);finishSceneTiming(element,true);if(element.getAttribute('data-screen')===routeScreenKey(route))activate(element);else deactivate(element)}
function activate(element:Element):void {
  leavingScenes.delete(element)
  const visible=sceneVisibility.get(element);if(visible)visible.value=true
  if(sceneActivity.get(element)===true)return
  sceneActivity.set(element,true)
  element.removeAttribute('inert');element.removeAttribute('aria-hidden');element.setAttribute('data-route-active','true')
  notifyScroll()
}
function deactivate(element:Element):boolean {
  const visible=sceneVisibility.get(element)
  if(visible&&!leavingScenes.has(element))visible.value=false
  if(sceneActivity.get(element)===false)return false
  for(const animation of runningEntrances.get(element)||[]){
    const target=(animation.effect as KeyframeEffect|null)?.target
    if(target instanceof Element){rememberAnimation(target,animation.animationName);if(Number.isFinite(animation.effect?.getComputedTiming().endTime))animation.finish()}
  }
  runningEntrances.delete(element)
  sceneActivity.set(element,false)
  // Keep the scroll host's layout classes stable during the slide. Consumers
  // choose the active scene instead of restyling every descendant on exit.
  element.setAttribute('inert','');element.setAttribute('aria-hidden','true');element.setAttribute('data-route-active','false')
  return true
}
function beginLeave(element:Element):void {
  movingScenes.add(element)
  // Vue's KeepAlive deactivation runs before the CSS leave has finished.
  // Stop committed work now, but keep the outgoing pixels until after-leave.
  leavingScenes.add(element)
  const visible=sceneVisibility.get(element);if(visible)visible.value=true
  deactivate(element)
}
function finishLeave(element:Element):void {endSceneMotion(element);leavingScenes.delete(element);deactivate(element)}
function syncTab(node:VNode):void {
  const element=node.el as HTMLElement;if(!element)return
  if(primary(route.path,route.query)&&element.dataset.path===route.path){
    activate(element)
  }
  // Saved scroll positions belong to the router. Retained scenes keep their
  // visible position through the transition instead of sweeping from the top.
  else {
    deactivate(element)
    if(dragging.value&&element.dataset.path===previewPath.value){const visible=sceneVisibility.get(element);if(visible)visible.value=true}
  }
}
function enterTab(element:Element):void {
  movingScenes.add(element)
  if(primary(route.path,route.query)&&element.getAttribute('data-path')===route.path)activate(element)
  else {deactivate(element);const visible=sceneVisibility.get(element);if(visible)visible.value=true}
}
function finishEnter(element:Element):void {endSceneMotion(element);notifyScroll()}
onMounted(()=>{
  notifyScroll()
  if(props.wallpaper&&viewportElement.value){canvasObserver=new ResizeObserver(measureCanvas);canvasObserver.observe(viewportElement.value);window.addEventListener('resize',measureCanvas);measureCanvas()}
})
onBeforeUnmount(()=>{removePeerGuard();releasePeerNavigation?.(false);for(const element of sceneTimings.keys())finishSceneTiming(element,true);alive=false;revision++;finishSettle?.();movingScenes.clear();cancelDragFrame();cancelPaint();canvasObserver?.disconnect();window.removeEventListener('resize',measureCanvas);if(canvasFrame!==undefined)cancelAnimationFrame(canvasFrame);if(scrollFrame!==undefined)cancelAnimationFrame(scrollFrame)})
</script>
<template><div class="cczj-mobile-route-stage" :style="canvasStyle" :class="{'cczj-route-wallpaper':wallpaper,'cczj-mobile-shared-backdrop':mobile,'cczj-mobile-tab-dragging':dragging,'cczj-mobile-tab-settling':settling}" @touchstart.passive="swipe.touchStart" @touchend.passive="swipe.touchEnd" @touchcancel.passive="swipe.cancel" @click.capture="swipe.click($event)" @animationstart.capture="trackEntrance" @animationend.capture="rememberEntrance" @animationcancel.capture="rememberEntrance">
 <div v-if="mobile" ref="backdropElement" class="cczj-mobile-route-backdrop" aria-hidden="true" :style="backdropStyle()" />
 <div ref="viewportElement" class="cczj-mobile-route-viewport">
  <template v-for="entry in primaryEntries" :key="entry.path"><router-view :route="entry" v-slot="{Component}"><Transition :name="name" :appear="routeScreenKey(entry)!==initialScreen" :css="motion.allows('pages')&&!dragging&&!committing&&!readerChange" @before-enter="enterTab" @enter="restoreEntrances" @before-leave="beginLeave" @after-leave="finishLeave" @after-enter="finishEnter" @enter-cancelled="cancelScene" @leave-cancelled="cancelScene"><KeepAlive><component v-if="Component&&tabVisible(entry.path)" :is="scene(Component)" :key="entry.path" :view="Component" :location="entry" :frame="$slots.default" :class="{'cczj-mobile-route-current':primary(route.path,route.query)&&entry.path===route.path,'cczj-mobile-route-preview':dragging&&entry.path===previewPath&&entry.path!==route.path}" :style="tabStyle(entry.path)" @vue:mounted="syncTab" @vue:updated="syncTab" /></KeepAlive></Transition></router-view></template>
  <router-view v-if="!mobile||!primary(secondary.path,secondary.query)" :route="secondary" v-slot="{Component}"><Transition :name="name" :appear="mobile && routeScreenKey(secondary)!==initialScreen" :css="motion.allows('pages')&&!chapterChange&&!readerChange" @before-enter="beginEnter" @enter="enterScene" @before-leave="beginLeave" @after-leave="finishLeave" @after-enter="enteredScene" @enter-cancelled="cancelScene" @leave-cancelled="cancelScene"><KeepAlive :include="secondaryCache" :max="mobile ? 1 : undefined"><component v-if="Component&&(!mobile||!primary(route.path,route.query))" :is="scene(Component)" :key="secondaryInstanceKey" :view="Component" :location="secondary" :frame="$slots.default" /></KeepAlive></Transition></router-view>
 </div>
 <slot name="navigation" :route="route" />
</div></template>
<style src="./route-motion.css"></style>
