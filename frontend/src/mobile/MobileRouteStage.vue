<script setup lang="ts">
import { computed, defineComponent, h, nextTick, onBeforeUnmount, onMounted, provide, ref, shallowReactive, shallowRef, watch, type Component, type PropType, type Slot, type VNode } from 'vue'
import { useRoute,useRouter,loadRouteLocation,routeLocationKey,routerViewLocationKey,type RouteLocationNormalizedLoaded } from 'vue-router'
import { primaryTabs,routeDirection,routeScreenKey } from './routeMotion'
import { useTabSwipe } from './useTabSwipe'
import { routeBackdropFocus } from './routeBackdrop'
const props=withDefaults(defineProps<{mobile:boolean;cache?:string[]}>(),{cache:()=>[]})
const router=useRouter(),route=useRoute(),direction=ref<'forward'|'back'>('forward')
const primary=(path:string,query:RouteLocationNormalizedLoaded['query']={})=>props.mobile&&!(['/settings','/novel/settings','/comic/settings'].includes(path)&&query.group)&&primaryTabs.some(tabs=>tabs.includes(path))
const entries=shallowReactive(new Map<string,RouteLocationNormalizedLoaded>())
const primaryEntries=computed(()=>Array.from(entries.values()))
const secondary=shallowRef<RouteLocationNormalizedLoaded>({...route})
const offset=ref(0),settling=ref(false),committing=ref(false),dragFrom=ref(''),previewPath=ref(''),previewEdge=ref(1)
const dragging=computed(()=>!!dragFrom.value)
const backdropStyle=computed(()=>{
 const origin=routeBackdropFocus(route.path,String(route.query.group||''))
 const target=previewPath.value?routeBackdropFocus(previewPath.value):origin
 const fraction=dragging.value?Math.min(1,Math.abs(offset.value)/Math.max(1,innerWidth)):0
 const x=origin.x+(target.x-origin.x)*fraction,y=origin.y+(target.y-origin.y)*fraction
 return {transform:`translate3d(${(0.5-x)*20}%,${(0.5-y)*12}%,0)`}
})
const chapterChange=ref(false)
let previous={...route} as RouteLocationNormalizedLoaded,position=Number(router.options.history.state.position)||0
let revision=0,alive=true,timer:ReturnType<typeof setTimeout>|undefined,finishWait:(()=>void)|undefined
const snapshots=new Map<string,number>()
function remember():void { snapshots.set(route.path,document.querySelector('.main-content')?.scrollTop||0) }
function reset():void {revision++;dragFrom.value='';previewPath.value='';offset.value=0;settling.value=false}
async function preview(path:string,edge:number):Promise<void> {
  previewEdge.value=edge
  if(path===previewPath.value&&dragFrom.value)return
  if(!dragFrom.value){dragFrom.value=route.path;remember()}
  previewPath.value=path;const token=++revision
  if(!path||entries.has(path))return
  try {
    const loaded=await loadRouteLocation(router.resolve(path))
    if(alive&&token===revision)entries.set(path,loaded)
  } catch { /* Failed lazy loads remain on the current page. */ }
}
async function settle(path?:string):Promise<void> {
  const token=revision
  settling.value=true
  const width=document.querySelector('.cczj-mobile-route-stage')?.clientWidth||innerWidth
  const reduce=matchMedia('(prefers-reduced-motion: reduce)').matches||document.documentElement.dataset.motionPages==='off'||document.documentElement.dataset.cczjMotion==='off'
  offset.value=path?-previewEdge.value*width:0
  await new Promise<void>(resolve=>{finishWait=resolve;timer=setTimeout(resolve,reduce?0:230)})
  finishWait=undefined;timer=undefined
  // A Back/tab action during the settle animation supersedes this gesture.
  if(!alive||token!==revision)return
  committing.value=true
  try { if(path)await router.push(path) } catch { /* Leave the current page usable after a rejected navigation. */ }
  reset();await nextTick();committing.value=false;notifyScroll()
}
const swipe=useTabSwipe(()=>props.mobile,{preview:(path,edge)=>void preview(path,edge),move:distance=>{offset.value=distance},settle})
watch(router.currentRoute,()=>{
  chapterChange.value=/^\/(novel|comic)\/read\//.test(route.path)&&/^\/(novel|comic)\/read\//.test(previous.path)&&routeScreenKey(route)===routeScreenKey(previous)
  const next=Number(router.options.history.state.position)||0
  direction.value=routeDirection(previous,route,position,next)
  if(!committing.value&&dragging.value&&route.path!==dragFrom.value)reset()
  if(primary(route.path,route.query))entries.set(route.path,{...route})
  else secondary.value={...route}
  previous={...route} as RouteLocationNormalizedLoaded;position=next
},{flush:'sync',immediate:true})
const name=computed(()=>props.mobile?(direction.value==='back'?'cczj-mobile-route-back':'cczj-mobile-route-forward'):'cczj-motion-page')
function tabVisible(path:string):boolean {return (primary(route.path,route.query)&&route.path===path)||(dragging.value&&(path===previewPath.value||path===dragFrom.value))}
function tabStyle(path:string):Record<string,string>|undefined {
  if(!dragging.value)return
  if(path===dragFrom.value)return{transform:`translate3d(${offset.value}px,0,0)`}
  if(path===previewPath.value)return{transform:`translate3d(calc(${previewEdge.value*100}% + ${offset.value}px),0,0)`}
}
const definitions=new Map<VNode['type'],Component>()
function scene(view:VNode):Component {
  const existing=definitions.get(view.type);if(existing)return existing
  const source=view.type as {name?:string;__name?:string}
  const definition=defineComponent({
    name:source.name||source.__name||'MobileRoutePage',
    props:{view:{type:Object as PropType<VNode>,required:true},location:{type:Object as PropType<RouteLocationNormalizedLoaded>,required:true},frame:{type:Function as PropType<Slot>,required:true}},
    setup(sceneProps){
      const shown=shallowReactive({...sceneProps.location}) as RouteLocationNormalizedLoaded
      watch(()=>sceneProps.location,value=>Object.assign(shown,value),{flush:'sync'})
      provide(routeLocationKey,shown);provide(routerViewLocationKey,computed(()=>shown))
      return()=>h('div',{class:'cczj-mobile-route-scene','data-path':shown.path,'data-screen':routeScreenKey(shown),'data-route-active':'true'},sceneProps.frame({Component:sceneProps.view,route:shown}))
    },
  })
  definitions.set(view.type,definition);return definition
}
function notifyScroll():void {window.dispatchEvent(new Event('cczj:route-scroll-container'))}
function activate(element:Element):void {
  element.removeAttribute('inert');element.removeAttribute('aria-hidden');element.setAttribute('data-route-active','true')
  element.querySelectorAll<HTMLElement>('[data-cczj-scroll-host]').forEach(el=>el.classList.add('main-content'))
  requestAnimationFrame(notifyScroll)
}
function deactivate(element:Element):void {
  element.setAttribute('inert','');element.setAttribute('aria-hidden','true');element.setAttribute('data-route-active','false')
  element.querySelectorAll<HTMLElement>('.main-content').forEach(el=>{el.dataset.cczjScrollHost='true';el.classList.remove('main-content')})
}
function leaveTab(element:Element):void {
  const scroll=element.querySelector<HTMLElement>('.main-content')
  if(scroll)snapshots.set(element.getAttribute('data-path')||'',scroll.scrollTop)
  deactivate(element)
}
function syncTab(node:VNode):void {
  const element=node.el as HTMLElement;if(!element)return
  if(primary(route.path,route.query)&&element.dataset.path===route.path)activate(element)
  else {
    deactivate(element)
    const scroll=element.querySelector<HTMLElement>('[data-cczj-scroll-host]')
    if(scroll)scroll.scrollTop=snapshots.get(element.dataset.path||'')||0
  }
}
function enterTab(element:Element):void {if(primary(route.path,route.query)&&element.getAttribute('data-path')===route.path)activate(element);else deactivate(element)}
onMounted(()=>requestAnimationFrame(notifyScroll))
onBeforeUnmount(()=>{alive=false;revision++;clearTimeout(timer);finishWait?.()})
</script>
<template><div class="cczj-mobile-route-stage" :class="{'cczj-mobile-shared-backdrop':mobile,'cczj-mobile-tab-dragging':dragging,'cczj-mobile-tab-settling':settling}" @touchstart.passive="swipe.touchStart" @touchmove="swipe.touchMove" @touchend="swipe.touchEnd" @touchcancel="swipe.cancel" @click.capture="swipe.click">
 <div v-if="mobile" class="cczj-mobile-route-backdrop" aria-hidden="true" :style="backdropStyle" />
 <div class="cczj-mobile-route-viewport">
  <template v-for="entry in primaryEntries" :key="entry.path"><router-view :route="entry" v-slot="{Component}"><Transition :name="name" :css="!dragging&&!committing" @before-enter="enterTab" @before-leave="leaveTab" @after-enter="notifyScroll"><KeepAlive><component v-if="Component&&tabVisible(entry.path)" :is="scene(Component)" :key="entry.path" :view="Component" :location="entry" :frame="$slots.default" :class="{'cczj-mobile-route-current':primary(route.path,route.query)&&entry.path===route.path,'cczj-mobile-route-preview':dragging&&entry.path===previewPath&&entry.path!==route.path}" :style="tabStyle(entry.path)" @vue:mounted="syncTab" @vue:updated="syncTab" /></KeepAlive></Transition></router-view></template>
  <router-view v-if="!mobile||!primary(secondary.path,secondary.query)" :route="secondary" v-slot="{Component}"><Transition :name="name" :css="!chapterChange" :mode="mobile?undefined:'out-in'" @before-enter="activate" @before-leave="deactivate" @after-enter="notifyScroll" @enter-cancelled="deactivate"><KeepAlive :include="cache"><component v-if="Component&&(!mobile||!primary(route.path,route.query))" :is="scene(Component)" :key="routeScreenKey(secondary)" :view="Component" :location="secondary" :frame="$slots.default" /></KeepAlive></Transition></router-view>
 </div>
 <slot name="navigation" :route="route" />
</div></template>
<style src="./route-motion.css"></style>
