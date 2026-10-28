<script setup lang="ts">
import { computed, defineComponent, h, nextTick, onBeforeUnmount, onMounted, provide, ref, shallowReactive, shallowRef, watch, type Component, type PropType, type Slot, type VNode } from 'vue'
import { useRoute,useRouter,loadRouteLocation,routeLocationKey,routerViewLocationKey,type RouteLocationNormalizedLoaded } from 'vue-router'
import { primaryTabs,routeDirection,routeScreenKey } from './routeMotion'
import { useTabSwipe } from './useTabSwipe'
import { routeBackdropFocus, routeBackdropFrame } from './routeBackdrop'
import { useBackdropSystemBars } from '../platform/backdropSystemBars'
import { routeVisualActivityKey, routeWorkActivityKey } from '../composables/useViewActivity'
const props=withDefaults(defineProps<{mobile:boolean;cache?:string[]}>(),{cache:()=>[]})
const router=useRouter(),route=useRoute(),direction=ref<'forward'|'back'>('forward')
const initialScreen=routeScreenKey(route)
const primary=(path:string,query:RouteLocationNormalizedLoaded['query']={})=>props.mobile&&!(['/settings','/novel/settings','/comic/settings'].includes(path)&&query.group)&&primaryTabs.some(tabs=>tabs.includes(path))
const entries=shallowReactive(new Map<string,RouteLocationNormalizedLoaded>())
const primaryEntries=computed(()=>Array.from(entries.values()))
const secondary=shallowRef<RouteLocationNormalizedLoaded>({...route})
const settling=ref(false),committing=ref(false),dragFrom=ref(''),previewPath=ref(''),previewEdge=ref(1)
let offset=0,dragFrame:number|undefined
let gestureWidth=1
const dragging=computed(()=>!!dragFrom.value)
const backdropElement=ref<HTMLElement>()
const viewportElement=ref<HTMLElement>()
const bars=useBackdropSystemBars(backdropElement,()=>routeScreenKey(route),()=>route.name==='player')
function backdropStyle():Record<string,string> {
 const origin=routeBackdropFocus(route.path,String(route.query.group||''))
 const target=previewPath.value?routeBackdropFocus(previewPath.value):origin
 const fraction=dragging.value?Math.min(1,Math.abs(offset)/gestureWidth):0
 const x=origin.x+(target.x-origin.x)*fraction,y=origin.y+(target.y-origin.y)*fraction
 return routeBackdropFrame(route.path,{x,y})
}
const chapterChange=ref(false)
let previous={...route} as RouteLocationNormalizedLoaded,position=Number(router.options.history.state.position)||0
let revision=0,alive=true
let preparing:Promise<void>|undefined
let scrollFrame:number|undefined
function cancelDragFrame():void {if(dragFrame!==undefined)cancelAnimationFrame(dragFrame);dragFrame=undefined}
function reset():void {cancelDragFrame();revision++;dragFrom.value='';previewPath.value='';offset=0;settling.value=false;bars.finish()}
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
function reducedPages():boolean {return matchMedia('(prefers-reduced-motion: reduce)').matches||document.documentElement.dataset.motionPages==='off'||document.documentElement.dataset.cczjMotion==='off'}
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
  // Ionic waits for the entering view before handing off; Vant separates
  // preparation from movement. A slow lazy import must not expose the canvas.
  // https://github.com/ionic-team/ionic-framework/blob/main/core/src/utils/transition/index.ts
  // https://github.com/vant-ui/vant/blob/main/packages/vant/src/swipe/Swipe.tsx
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
  if(!reduce&&leaving instanceof HTMLElement)bars.settle(dragFrom.value,previewPath.value||dragFrom.value,leaving,false,true)
  if(!reduce){
    const elements=[...Array.from(viewportElement.value?.children||[]),backdropElement.value]
    const transitions=elements.flatMap(element=>element instanceof HTMLElement?element.getAnimations():[])
    await Promise.allSettled(transitions.map(animation=>animation.finished))
  }
  // A Back/tab action during the settle animation supersedes this gesture.
  if(!alive||token!==revision)return
  committing.value=true
  try { if(path)await router.push(path) } catch { /* Leave the current page usable after a rejected navigation. */ }
  reset();await nextTick();committing.value=false;notifyScroll()
}
const swipe=useTabSwipe(()=>props.mobile,{preview:(path,edge)=>void preview(path,edge),move,settle})
watch(router.currentRoute,()=>{
  const fromPath=previous.path
  chapterChange.value=/^\/(novel|comic)\/read\//.test(route.path)&&/^\/(novel|comic)\/read\//.test(previous.path)&&routeScreenKey(route)===routeScreenKey(previous)
  const next=Number(router.options.history.state.position)||0
  direction.value=routeDirection(previous,route,position,next)
  if(!committing.value&&dragging.value&&route.path!==dragFrom.value)reset()
  if(primary(route.path,route.query))entries.set(route.path,{...route})
  else secondary.value={...route}
  previous={...route} as RouteLocationNormalizedLoaded;position=next
  if(props.mobile&&!committing.value)void nextTick(()=>{
    if(route.path===fromPath||reducedPages())return
    const entering=Array.from(viewportElement.value?.children||[]).find(element=>element instanceof HTMLElement&&element.dataset.path===route.path)
    if(entering instanceof HTMLElement)bars.settle(fromPath,route.path,entering,true)
  })
},{flush:'sync',immediate:true})
const name=computed(()=>props.mobile?(direction.value==='back'?'cczj-mobile-route-back':'cczj-mobile-route-forward'):'cczj-motion-page')
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
const homeOrigins=new WeakSet<Element>()
const seenEntrances=new WeakMap<Element,Set<string>>()
const entranceName=/^(?:cczj-fade-in(?:-up)?|cczj-reveal-scale|room-(?:sheet-turn|shelf-unfold|book-arrive)|manga-(?:cover-pop|panel-arrive))(?:-|$)/
function rememberEntrance(event:AnimationEvent):void {
  if(props.mobile&&event.target instanceof Element&&entranceName.test(event.animationName))rememberAnimation(event.target,event.animationName)
}
function rememberAnimation(target:Element,name:string):void {
  const names=seenEntrances.get(target)||new Set<string>()
  names.add(name);seenEntrances.set(target,names)
}
function entranceAnimations(element:Element):CSSAnimation[] {
  return props.mobile?element.getAnimations({subtree:true}).filter((animation):animation is CSSAnimation=>animation instanceof CSSAnimation&&entranceName.test(animation.animationName)):[]
}
function restoreEntrances(element:Element):void {
  const visible=sceneVisibility.get(element);if(visible)visible.value=true
  // KeepAlive reinserts the same DOM, restarting CSS animations. Their first
  // entrance belongs to that content instance, not to every route activation.
  // New elements have no record and keep their normal entrance animation.
  for(const animation of entranceAnimations(element)){
    const target=(animation.effect as KeyframeEffect|null)?.target
    if(target instanceof Element&&seenEntrances.get(target)?.has(animation.animationName)&&Number.isFinite(animation.effect?.getComputedTiming().endTime))animation.finish()
  }
}
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
      provide(routeWorkActivityKey,computed(()=>router.currentRoute.value.fullPath===shown.fullPath))
      watch(()=>sceneProps.location,value=>Object.assign(shown,value),{flush:'sync'})
      provide(routeLocationKey,shown);provide(routerViewLocationKey,computed(()=>shown))
      return()=>h('div',{class:'cczj-mobile-route-scene','data-path':shown.path,'data-screen':routeScreenKey(shown),'data-route-active':'true',onVnodeMounted:(node:VNode)=>{if(node.el instanceof Element)sceneVisibility.set(node.el,visible)}},sceneProps.frame({Component:sceneProps.view,route:shown}))
    },
  })
  definitions.set(view.type,definition);return definition
}
function notifyScroll():void {
  if(scrollFrame!==undefined||!alive)return
  scrollFrame=requestAnimationFrame(()=>{scrollFrame=undefined;if(alive)window.dispatchEvent(new Event('cczj:route-scroll-container'))})
}
function activate(element:Element):void {
  leavingScenes.delete(element)
  const visible=sceneVisibility.get(element);if(visible)visible.value=true
  if(sceneActivity.get(element)===true)return
  sceneActivity.set(element,true)
  if(props.mobile&&element.getAttribute('data-path')==='/')homeOrigins.add(element)
  element.removeAttribute('inert');element.removeAttribute('aria-hidden');element.setAttribute('data-route-active','true')
  notifyScroll()
}
function deactivate(element:Element):boolean {
  const visible=sceneVisibility.get(element)
  if(visible&&!leavingScenes.has(element))visible.value=false
  if(sceneActivity.get(element)===false)return false
  for(const animation of entranceAnimations(element)){
    const target=(animation.effect as KeyframeEffect|null)?.target
    if(target instanceof Element)rememberAnimation(target,animation.animationName)
  }
  sceneActivity.set(element,false)
  homeOrigins.delete(element)
  // Keep the scroll host's layout classes stable during the slide. Consumers
  // choose the active scene instead of restyling every descendant on exit.
  element.setAttribute('inert','');element.setAttribute('aria-hidden','true');element.setAttribute('data-route-active','false')
  return true
}
function beginLeave(element:Element):void {
  // Vue's KeepAlive deactivation runs before the CSS leave has finished.
  // Stop committed work now, but keep the outgoing pixels until after-leave.
  leavingScenes.add(element)
  const visible=sceneVisibility.get(element);if(visible)visible.value=true
  deactivate(element)
}
function finishLeave(element:Element):void {leavingScenes.delete(element);deactivate(element)}
function syncTab(node:VNode):void {
  const element=node.el as HTMLElement;if(!element)return
  if(primary(route.path,route.query)&&element.dataset.path===route.path){
    activate(element)
    if(homeOrigins.has(element)&&element.isConnected){
      const scroll=element.querySelector<HTMLElement>('.main-content')
      if(scroll){
        if(dragging.value||committing.value||reducedPages())scroll.dataset.cczjScrollRestore='instant'
        else {delete scroll.dataset.cczjScrollRestore;scroll.scrollTop=0}
      }
      homeOrigins.delete(element)
    }
  }
  // The router owns saved positions. Home only establishes the origin of its
  // requested smooth return; gesture handoff keeps the already visible view.
  else {
    deactivate(element)
    if(dragging.value&&element.dataset.path===previewPath.value){const visible=sceneVisibility.get(element);if(visible)visible.value=true}
  }
}
function enterTab(element:Element):void {
  if(primary(route.path,route.query)&&element.getAttribute('data-path')===route.path)activate(element)
  else {deactivate(element);const visible=sceneVisibility.get(element);if(visible)visible.value=true}
}
onMounted(notifyScroll)
onBeforeUnmount(()=>{alive=false;revision++;cancelDragFrame();if(scrollFrame!==undefined)cancelAnimationFrame(scrollFrame)})
</script>
<template><div class="cczj-mobile-route-stage" :class="{'cczj-mobile-shared-backdrop':mobile,'cczj-mobile-tab-dragging':dragging,'cczj-mobile-tab-settling':settling}" @touchstart.passive="swipe.touchStart" @touchend.passive="swipe.touchEnd" @touchcancel.passive="swipe.cancel" @click.capture="swipe.click" @animationend.capture="rememberEntrance">
 <div v-if="mobile" ref="backdropElement" class="cczj-mobile-route-backdrop" aria-hidden="true" :style="backdropStyle()" />
 <div ref="viewportElement" class="cczj-mobile-route-viewport">
  <template v-for="entry in primaryEntries" :key="entry.path"><router-view :route="entry" v-slot="{Component}"><Transition :name="name" :appear="routeScreenKey(entry)!==initialScreen" :css="!dragging&&!committing" @before-enter="enterTab" @enter="restoreEntrances" @before-leave="beginLeave" @after-leave="finishLeave" @after-enter="notifyScroll"><KeepAlive><component v-if="Component&&tabVisible(entry.path)" :is="scene(Component)" :key="entry.path" :view="Component" :location="entry" :frame="$slots.default" :class="{'cczj-mobile-route-current':primary(route.path,route.query)&&entry.path===route.path,'cczj-mobile-route-preview':dragging&&entry.path===previewPath&&entry.path!==route.path}" :style="tabStyle(entry.path)" @vue:mounted="syncTab" @vue:updated="syncTab" /></KeepAlive></Transition></router-view></template>
  <router-view v-if="!mobile||!primary(secondary.path,secondary.query)" :route="secondary" v-slot="{Component}"><Transition :name="name" :appear="mobile && routeScreenKey(secondary)!==initialScreen" :css="!chapterChange" :mode="mobile?undefined:'out-in'" @before-enter="activate" @enter="restoreEntrances" @before-leave="beginLeave" @after-leave="finishLeave" @after-enter="notifyScroll" @enter-cancelled="deactivate"><KeepAlive :include="cache"><component v-if="Component&&(!mobile||!primary(route.path,route.query))" :is="scene(Component)" :key="routeScreenKey(secondary)" :view="Component" :location="secondary" :frame="$slots.default" /></KeepAlive></Transition></router-view>
 </div>
 <slot name="navigation" :route="route" />
</div></template>
<style src="./route-motion.css"></style>
