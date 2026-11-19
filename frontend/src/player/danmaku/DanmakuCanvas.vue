<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useViewActivity } from '../../composables/useViewActivity'
import { danmakuTypefaceStatus } from './typeface'
import { DanmakuTimeline, type DanmakuDraw } from './timeline'
import { DanmakuTextBitmaps } from './textBitmaps'
import DanmakuLikeFeedback from './DanmakuLikeFeedback.vue'
import { danmakuFont, danmakuPixelRatio } from './textStyle'
import type { DanmakuItem } from '../../api/danmaku'
import type { DanmakuPreferences } from './preferences'
const props=defineProps<{items:DanmakuItem[];offset:number;preferences:DanmakuPreferences;current:number;playing:boolean;interactive:boolean;video:()=>HTMLVideoElement|null}>()
const canvas=ref<HTMLCanvasElement>()
const active = useViewActivity(undefined, 'committed')
const hovered = ref(false)
const feedback = ref<{ id: number; x: number; y: number }>()
type Point = { x: number; y: number }
type ClientPoint = Pick<MouseEvent, 'clientX' | 'clientY'>
let pointer: Point | undefined
let pendingHover: ClientPoint | undefined
let pressed: { item: DanmakuItem; x: number; y: number; pointerId: number } | undefined
let visible: DanmakuDraw[] = []
let feedbackId = 0
let context:CanvasRenderingContext2D|null=null,frame:number|undefined,observer:ResizeObserver|undefined,width=0,height=0,disposed=false
let needsPaint = true
let lastPaintTime = -Infinity
const bitmaps = new DanmakuTextBitmaps()
const timeline=new DanmakuTimeline((text,size)=>{if(!context)return text.length*size;context.font=danmakuFont(props.preferences,size);return context.measureText(text).width})

function canInteract(): boolean {
  return props.interactive && active.value && !disposed
}

function pointOf(event: ClientPoint): Point | undefined {
  if (!canInteract() || !canvas.value) return
  const rect = canvas.value.getBoundingClientRect()
  if (!rect.width || !rect.height || event.clientX < rect.left || event.clientX >= rect.right ||
    event.clientY < rect.top || event.clientY >= rect.bottom) return
  return { x: (event.clientX - rect.left) * width / rect.width, y: (event.clientY - rect.top) * height / rect.height }
}

function hit(point?: Point): DanmakuDraw | undefined {
  if (!point) return
  for (let index = visible.length - 1; index >= 0; index--) {
    const row = visible[index]
    if (point.x >= row.x && point.x <= row.x + row.width &&
      point.y >= row.y - row.fontSize && point.y <= row.y + row.fontSize * .24) return row
  }
}

function syncHover(): void {
  const row = hit(pointer)
  timeline.hold(row?.item)
  hovered.value = !!row
}

function leave(): void {
  pendingHover = undefined
  pointer = undefined
  syncHover()
}

function hover(event: PointerEvent): void {
  if (event.pointerType !== 'mouse') return
  if (event.buttons || !canInteract()) { leave(); return }
  pendingHover = { clientX: event.clientX, clientY: event.clientY }
  queueFrame()
}

function press(event: PointerEvent): void {
  pressed = undefined
  if (!event.isPrimary || event.button !== 0) return
  const row = hit(pointOf(event))
  if (row) pressed = { item: row.item, x: event.clientX, y: event.clientY, pointerId: event.pointerId }
}

function like(event: MouseEvent): boolean {
  const contact = pressed
  pressed = undefined
  if (!canInteract() || !event.detail || !contact ||
    Math.hypot(event.clientX - contact.x, event.clientY - contact.y) >= 10 ||
    ('pointerId' in event && event.pointerId !== contact.pointerId)) return false
  const row = visible.find(candidate => candidate.item === contact.item)
  if (!row || row.x >= width || row.x + row.width <= 0) return false
  feedback.value = {
    id: ++feedbackId,
    x: Math.max(16, Math.min(width - 16, row.x + row.width / 2)),
    y: Math.max(20, row.y - row.fontSize),
  }
  return true
}

function cancelPress(): void { pressed = undefined }
function resetInteraction(): void {
  leave()
  cancelPress()
  feedback.value = undefined
  visible = []
  needsPaint = true
}

const interaction = { hover, leave, press, like, cancelPress, get hovered() { return hovered.value } }
defineExpose({ interaction })

function stop(){if(frame!==undefined)cancelAnimationFrame(frame);frame=undefined}
function draw(){
 frame=undefined
 const node=canvas.value
 if(!context||!node||disposed||!active.value||!width||!height)return
 const now=(props.video()?.currentTime??props.current)*1000
 if(needsPaint||now!==lastPaintTime){
   const hadComments = visible.length > 0
   visible=timeline.tick(now,width,height,props.preferences)
   bitmaps.retain(visible)
   if(needsPaint||hadComments||visible.length){
     context.resetTransform()
     context.clearRect(0,0,node.width,node.height)
     const scaleX = node.width / width, scaleY = node.height / height
     for(const row of visible)bitmaps.draw(context,row,scaleX,scaleY,props.preferences)
   }
   needsPaint=false
   lastPaintTime=now
 }
 if(pendingHover){pointer=pointOf(pendingHover);pendingHover=undefined;syncHover()}
 else if(pointer)syncHover()
 if(props.playing&&props.preferences.enabled&&(props.items.length||visible.length))queueFrame()
}
function queueFrame(): void {
  if(frame===undefined&&!disposed&&active.value)frame=requestAnimationFrame(draw)
}
function schedule(): void {
  needsPaint=true
  queueFrame()
}
function resize(){
 const node=canvas.value
 if(!node||!context)return
 // Layout dimensions exclude ancestor route transitions and fullscreen transforms.
 const nextWidth = node.clientWidth, nextHeight = node.clientHeight
 const layoutChanged = width !== nextWidth || height !== nextHeight
 if(layoutChanged)resetInteraction()
 width=nextWidth
 height=nextHeight
 if(!width||!height)return
 const scale=danmakuPixelRatio(width,height)
 const pixelsWide=Math.max(1,Math.round(width*scale)),pixelsHigh=Math.max(1,Math.round(height*scale))
 if(!layoutChanged&&node.width===pixelsWide&&node.height===pixelsHigh)return
 bitmaps.clear()
 if(node.width!==pixelsWide)node.width=pixelsWide
 if(node.height!==pixelsHigh)node.height=pixelsHigh
 needsPaint=true
 stop()
 draw()
}
watch(()=>[props.items,props.offset] as const,()=>{timeline.setItems(props.items,props.offset);schedule()},{immediate:true})
watch(()=>[props.preferences.fontFamily,props.preferences.fontWeight,props.preferences.fontSize,
  props.preferences.textStyle,props.preferences.strokeWidth] as const,()=>{bitmaps.clear();schedule()})
watch(()=>[props.playing,props.preferences] as const,schedule,{deep:true})
watch(()=>props.current,()=>{if(!props.video()||!props.playing)schedule()})
watch(danmakuTypefaceStatus,()=>{bitmaps.clear();timeline.invalidateLayout();schedule()})
watch(() => props.interactive, enabled => { if (!enabled) resetInteraction(); schedule() })
watch(active, value => {
  if (value) schedule()
  else { stop(); resetInteraction(); bitmaps.clear() }
})
watch(()=>props.video(),(video,previous,cleanup)=>{
 if(previous){timeline.resetPosition();resetInteraction();bitmaps.clear();schedule()}
 if(!video)return
 const seeking=()=>{timeline.resetPosition();resetInteraction();bitmaps.clear();schedule()}
 video.addEventListener('seeking',seeking)
 cleanup(()=>video.removeEventListener('seeking',seeking))
},{immediate:true})
onMounted(()=>{context=canvas.value?.getContext('2d')??null;observer=new ResizeObserver(resize);if(canvas.value)observer.observe(canvas.value);resize()})
onBeforeUnmount(()=>{disposed=true;stop();observer?.disconnect();resetInteraction();timeline.clear();bitmaps.clear();context=null;if(canvas.value){canvas.value.width=0;canvas.value.height=0}})
</script>
<template>
  <canvas ref="canvas" class="cczj-danmaku-canvas" :style="{ opacity: preferences.opacity }" aria-hidden="true" />
  <span v-if="feedback" class="danmaku-like-anchor" :style="{ left: `${feedback.x}px`, top: `${feedback.y}px` }">
    <DanmakuLikeFeedback :key="feedback.id" @finished="feedback = undefined" />
  </span>
</template>
<style scoped>
.cczj-danmaku-canvas {
  @apply cczj-absolute cczj-inset-0 cczj-w-full cczj-h-full cczj-pointer-events-none;
  z-index: 4;
  contain: strict;
}
.danmaku-like-anchor {
  @apply cczj-absolute cczj-pointer-events-none cczj-leading-none;
  transform: translate(-50%, -50%);
  z-index: 5;
  --btn-solid: #bce8ff;
  --btn-solid-text: #122732;
}
</style>
