<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { danmakuTypefaceStatus } from './typeface'
import type { DanmakuPreferences } from './preferences'
import { danmakuFont, danmakuFontSize, paintDanmaku, danmakuPixelRatio } from './textStyle'
const props=defineProps<{preferences:DanmakuPreferences;text:string;viewportWidth:number}>()
const canvas=ref<HTMLCanvasElement>()
let observer:ResizeObserver|undefined,disposed=false
function draw():void{
 const node=canvas.value,context=node?.getContext('2d')
 if(disposed||!node||!context)return
 const {width,height}=node.getBoundingClientRect(),ratio=danmakuPixelRatio(width,height)
 node.width=Math.round(width*ratio);node.height=Math.round(height*ratio);context.setTransform(ratio,0,0,ratio,0,0)
 const size=danmakuFontSize(props.viewportWidth,props.preferences.fontSize)
 context.font=danmakuFont(props.preferences,size)
 paintDanmaku(context,props.text,Math.max(4,(width-context.measureText(props.text).width)/2),(height+size*.7)/2,size,0xffffff,props.preferences)
}
watch(()=>[props.preferences,props.text,props.viewportWidth],draw,{deep:true})
watch(danmakuTypefaceStatus,draw)
onMounted(()=>{observer=new ResizeObserver(draw);if(canvas.value)observer.observe(canvas.value);draw()})
onBeforeUnmount(()=>{disposed=true;observer?.disconnect();if(canvas.value){canvas.value.width=0;canvas.value.height=0}})
</script>
<template><canvas ref="canvas" class="cczj-danmaku-preview" role="img" :aria-label="text" /></template>
