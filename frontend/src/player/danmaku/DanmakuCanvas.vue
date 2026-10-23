<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { DanmakuTimeline } from './timeline'
import type { DanmakuItem } from '../../api/danmaku'
import type { DanmakuPreferences } from './preferences'
const props=defineProps<{items:DanmakuItem[];offset:number;preferences:DanmakuPreferences;current:number;playing:boolean;video:()=>HTMLVideoElement|null}>()
const canvas=ref<HTMLCanvasElement>()
let context:CanvasRenderingContext2D|null=null,frame:number|undefined,observer:ResizeObserver|undefined,width=0,height=0,disposed=false
const timeline=new DanmakuTimeline((text,size)=>{if(!context)return text.length*size;context.font=`600 ${size}px system-ui,sans-serif`;return context.measureText(text).width})
function stop(){if(frame!==undefined)cancelAnimationFrame(frame);frame=undefined}
function draw(){
 frame=undefined
 if(!context||!canvas.value||disposed)return
 context.clearRect(0,0,width,height)
 const now=(props.video()?.currentTime??props.current)*1000
 const visible=timeline.tick(now,width,height,props.preferences)
 context.globalAlpha=props.preferences.opacity;context.lineWidth=3;context.lineJoin='round';context.textBaseline='alphabetic'
 for(const row of visible){
  const color=row.item.color;const luminance=((color>>16&255)*.2126+(color>>8&255)*.7152+(color&255)*.0722)/255
  context.font=`600 ${row.fontSize}px system-ui,sans-serif`;context.strokeStyle=luminance<.2?'rgba(255,255,255,.8)':'rgba(0,0,0,.85)';context.fillStyle=`#${color.toString(16).padStart(6,'0')}`
  context.strokeText(row.item.text,row.x,row.y);context.fillText(row.item.text,row.x,row.y)
 }
 if(props.playing&&props.preferences.enabled&&props.items.length&&!document.hidden)frame=requestAnimationFrame(draw)
}
function schedule(){stop();if(!disposed)frame=requestAnimationFrame(draw)}
function resize(){const node=canvas.value;if(!node||!context)return;const rect=node.getBoundingClientRect();width=rect.width;height=rect.height;const scale=Math.min(2,devicePixelRatio||1);node.width=Math.round(width*scale);node.height=Math.round(height*scale);context.setTransform(scale,0,0,scale,0,0);schedule()}
watch(()=>[props.items,props.offset] as const,()=>{timeline.setItems(props.items,props.offset);schedule()},{immediate:true})
watch(()=>[props.current,props.playing,props.preferences] as const,schedule,{deep:true})
function visibility(){if(document.hidden)stop();else schedule()}
onMounted(()=>{context=canvas.value?.getContext('2d')??null;observer=new ResizeObserver(resize);if(canvas.value)observer.observe(canvas.value);document.addEventListener('visibilitychange',visibility);resize()})
onBeforeUnmount(()=>{disposed=true;stop();observer?.disconnect();document.removeEventListener('visibilitychange',visibility);timeline.clear();context=null;if(canvas.value){canvas.value.width=0;canvas.value.height=0}})
</script>
<template><canvas ref="canvas" class="cczj-danmaku-canvas" aria-hidden="true" /></template>
<style scoped>.cczj-danmaku-canvas{@apply cczj-absolute;inset:0;@apply cczj-w-full cczj-h-full;z-index:4;@apply cczj-pointer-events-none;contain:strict}</style>
