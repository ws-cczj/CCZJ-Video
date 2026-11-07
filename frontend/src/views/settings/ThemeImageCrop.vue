<script setup lang="ts">
import { computed, reactive, ref, onBeforeUnmount, onMounted, watch, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button, Modal, NumberField, Spinner } from '../../components/ui'
import { MIN_THEME_CROP_EDGE, type ThemeCrop } from '../../utils/themeCrop'

const props=defineProps<{open:boolean;src:string;busy:boolean;hasOriginal:boolean}>()
const emit=defineEmits<{apply:[crop:ThemeCrop];cancel:[];restore:[]}>()
const {t}=useI18n()
const crop=reactive({x:0,y:0,width:0,height:0})
const size=reactive({width:0,height:0})
const stage=ref<HTMLElement>(),viewport=ref<HTMLElement>()
const loading=ref(true),loadFailed=ref(false),attempt=ref(0)
const handles=['nw','n','ne','e','se','s','sw','w'] as const
type Handle=typeof handles[number]
type Rect={x:number;y:number;width:number;height:number}
let drag:{id:number;x:number;y:number;before:Rect;action:Handle|'move'}|undefined
const renderWidth=ref(0)
let observer:ResizeObserver|undefined
const minimumEdge=computed(()=>Math.max(MIN_THEME_CROP_EDGE,renderWidth.value?Math.ceil(88*size.width/renderWidth.value):MIN_THEME_CROP_EDGE))
const minWidth=computed(()=>Math.min(minimumEdge.value,size.width))
const minHeight=computed(()=>Math.min(minimumEdge.value,size.height))
const selectionStyle=computed(()=>({
  left:crop.x/size.width*100+'%',top:crop.y/size.height*100+'%',
  width:crop.width/size.width*100+'%',height:crop.height/size.height*100+'%',
}))
const fields=['x','y','width','height'] as const
function clamp(value:number,min:number,max:number):number{return Math.max(min,Math.min(max,Math.round(value)))}
function maximum(key:keyof Rect):number {
  return key==='x'?size.width-crop.width:key==='y'?size.height-crop.height:key==='width'?size.width-crop.x:size.height-crop.y
}
function update(key:keyof Rect,value:number):void {
  crop[key]=clamp(value,key==='width'?minWidth.value:key==='height'?minHeight.value:0,maximum(key))
  void nextTick(centerViewport)
}
function centerViewport():void {
  const area=viewport.value,node=stage.value
  if(area&&node)area.scrollTop=(crop.y+crop.height/2)/size.height*node.getBoundingClientRect().height-area.clientHeight/2
}
function reset():void {
  let width=size.width*.8,height=width*9/16
  if(height>size.height*.8){height=size.height*.8;width=height*16/9}
  width=clamp(width,minWidth.value,size.width);height=clamp(height,minHeight.value,size.height)
  Object.assign(crop,{x:Math.round((size.width-width)/2),y:Math.round((size.height-height)/2),width,height})
  void nextTick(centerViewport)
}
function loaded(event:Event):void {
  if(!props.open)return
  const image=event.target as HTMLImageElement
  if(!image.naturalWidth||!image.naturalHeight){failed();return}
  Object.assign(size,{width:image.naturalWidth,height:image.naturalHeight})
  loading.value=false;loadFailed.value=false;reset()
}
function failed():void {if(props.open){loading.value=false;loadFailed.value=true}}
function retry():void {loading.value=true;loadFailed.value=false;attempt.value++}
function point(event:PointerEvent):{x:number;y:number} {
  const rect=stage.value!.getBoundingClientRect()
  return {x:(event.clientX-rect.left)/rect.width*size.width,y:(event.clientY-rect.top)/rect.height*size.height}
}
function start(event:PointerEvent,action:Handle|'move'):void {
  if(!props.open||event.button!==0||drag||loading.value||props.busy)return
  const p=point(event)
  drag={id:event.pointerId,...p,before:{...crop},action}
  stage.value!.setPointerCapture(event.pointerId)
}
function resized(before:Rect,action:Handle,dx:number,dy:number):Rect {
  let left=before.x,top=before.y,right=left+before.width,bottom=top+before.height
  if(action.includes('w'))left=clamp(left+dx,0,right-minWidth.value)
  if(action.includes('e'))right=clamp(right+dx,left+minWidth.value,size.width)
  if(action.includes('n'))top=clamp(top+dy,0,bottom-minHeight.value)
  if(action.includes('s'))bottom=clamp(bottom+dy,top+minHeight.value,size.height)
  return {x:left,y:top,width:right-left,height:bottom-top}
}
function shifted(before:Rect,dx:number,dy:number):Rect {
  return {...before,x:clamp(before.x+dx,0,size.width-before.width),y:clamp(before.y+dy,0,size.height-before.height)}
}
function move(event:PointerEvent):void {
  if(!drag||drag.id!==event.pointerId)return
  const area=viewport.value
  if(area){
    const bounds=area.getBoundingClientRect()
    if(event.clientY<bounds.top+24)area.scrollTop-=16
    else if(event.clientY>bounds.bottom-24)area.scrollTop+=16
  }
  const p=point(event),dx=p.x-drag.x,dy=p.y-drag.y
  Object.assign(crop,drag.action==='move'?shifted(drag.before,dx,dy):resized(drag.before,drag.action,dx,dy))
}
function finish(event:PointerEvent):void {
  if(drag?.id!==event.pointerId)return
  move(event);drag=undefined
  if(stage.value?.hasPointerCapture(event.pointerId))stage.value.releasePointerCapture(event.pointerId)
}
function cancelPointer():void {
  if(!drag)return
  const previous=drag;drag=undefined;Object.assign(crop,previous.before)
  if(stage.value?.hasPointerCapture(previous.id))stage.value.releasePointerCapture(previous.id)
}
function keyboard(event:KeyboardEvent,action:Handle|'move'):void {
  if(!props.open||props.busy||!event.key.startsWith('Arrow'))return
  event.preventDefault();event.stopPropagation()
  const step=event.shiftKey?10:1
  const dx=event.key==='ArrowLeft'?-step:event.key==='ArrowRight'?step:0
  const dy=event.key==='ArrowUp'?-step:event.key==='ArrowDown'?step:0
  Object.assign(crop,action==='move'?shifted(crop,dx,dy):resized(crop,action,dx,dy))
}
function apply():void {
  if(!props.open||props.busy||loading.value||loadFailed.value)return
  emit('apply',{x:crop.x/size.width*100,y:crop.y/size.height*100,width:crop.width/size.width*100,height:crop.height/size.height*100})
}
onMounted(()=>{
  observer=new ResizeObserver(entries=>{
    cancelPointer();renderWidth.value=entries[0]?.contentRect.width??0
    if(!loading.value&&!loadFailed.value){
      crop.width=clamp(crop.width,minWidth.value,size.width);crop.height=clamp(crop.height,minHeight.value,size.height)
      crop.x=clamp(crop.x,0,size.width-crop.width);crop.y=clamp(crop.y,0,size.height-crop.height)
    }
  })
  if(stage.value)observer.observe(stage.value)
})
watch(stage,(node,previous)=>{if(previous)observer?.unobserve(previous);if(node&&props.open)observer?.observe(node)},{flush:'post'})
watch(()=>props.open,open=>{
  cancelPointer()
  if(open){loading.value=true;loadFailed.value=false;attempt.value++;if(stage.value)observer?.observe(stage.value)}
  else observer?.disconnect()
},{flush:'sync'})
onBeforeUnmount(()=>{observer?.disconnect();cancelPointer()})
</script>

<template>
  <Modal :model-value="open" :title="t('settings.cropImage')" width="min(1080px, 94vw)"
    :closable="!busy" :mask-closable="false" :show-footer="true" @cancel="emit('cancel')">
    <div class="crop-layout cczj-grid cczj-gap-8" :aria-busy="loading || busy">
      <div class="crop-pane cczj-relative cczj-min-w-0">
      <div ref="viewport" class="crop-viewport cczj-relative cczj-min-w-0 cczj-overflow-auto">
        <div ref="stage" class="crop-stage cczj-relative cczj-w-full cczj-select-none"
          @pointermove="move" @pointerup="finish" @pointercancel="cancelPointer" @lostpointercapture="cancelPointer">
          <img :key="attempt" :src="src" :alt="t('settings.backgroundPreview')" draggable="false" @load="loaded" @error="failed" />
          <div v-if="!loading && !loadFailed" class="crop-selection cczj-absolute" :style="selectionStyle" tabindex="0"
            role="group" :aria-label="t('settings.cropSelection')" @pointerdown.stop.prevent="start($event,'move')" @keydown="keyboard($event,'move')">
            <button v-for="handle in handles" :key="handle" type="button" class="crop-handle cczj-absolute"
              :class="'handle-'+handle" :disabled="busy" :aria-label="t('settings.cropResize',{edge:t('settings.cropEdges.'+handle)})"
              @pointerdown.stop.prevent="start($event,handle)" @keydown.stop="keyboard($event,handle)" />
          </div>
        </div>
      </div>
        <div v-if="loading || busy" class="crop-loading cczj-absolute cczj-inset-0 cczj-flex cczj-items-center cczj-justify-center" role="status">
          <Spinner :label="t(busy?'settings.imageProcessing':'common.loading')" />
        </div>
        <div v-if="loadFailed" class="cczj-p-8 cczj-flex cczj-flex-col cczj-items-center cczj-gap-4" role="alert">
          <span>{{t('settings.imageLoadFailed')}}</span><Button @click="retry">{{t('common.retry')}}</Button>
        </div>
      </div>
      <fieldset :disabled="loading || busy || loadFailed" class="crop-values cczj-flex cczj-flex-col cczj-gap-6 cczj-min-w-0">
        <label v-for="key in fields" :key="key" class="cczj-flex cczj-flex-col cczj-gap-2">
          {{t('settings.cropFields.'+key)}} (px)
          <NumberField :model-value="crop[key]" :min="key==='width'?minWidth:key==='height'?minHeight:0" :max="maximum(key)"
            :ariaLabel="t('settings.cropFields.'+key)" @update:model-value="update(key,$event)" />
        </label>
        <Button :disabled="loading || busy || loadFailed" @click="reset">{{t('settings.cropReset')}}</Button>
        <Button v-if="hasOriginal" :disabled="loading || busy" @click="emit('restore')">{{t('settings.restoreOriginalImage')}}</Button>
      </fieldset>
    </div>
    <template #footer>
      <Button :disabled="busy" @click="emit('cancel')">{{t('common.cancel')}}</Button>
      <Button variant="primary" :loading="busy" :disabled="loading || loadFailed" @click="apply">{{t('settings.saveImage')}}</Button>
    </template>
  </Modal>
</template>

<style scoped>
.crop-layout { grid-template-columns:minmax(0,1fr) 180px; }
.crop-viewport { max-height:min(62vh,560px); min-height:280px; background:var(--bg-secondary); @apply cczj-rounded-md; }
.crop-stage { touch-action:pan-y; line-height:0; }
.crop-stage img { @apply cczj-block cczj-w-full; height:auto; }
.crop-selection { box-sizing:border-box; border:2px solid white; box-shadow:0 0 0 99999px rgba(0,0,0,.55); cursor:move; touch-action:none; }
.crop-selection:focus-visible { outline:2px solid var(--accent); outline-offset:3px; }
.crop-handle { width:44px; height:44px; padding:0; border:0; background:transparent; transform:translate(-50%,-50%); touch-action:none; }
.crop-handle::after { content:''; @apply cczj-absolute; left:50%; top:50%; transform:translate(-50%,-50%); width:10px; height:10px; border:1px solid var(--text-primary); background:white; border-radius:2px; }
.crop-handle:focus-visible { outline:2px solid var(--accent); }
.handle-nw {left:0;top:0;cursor:nwse-resize}.handle-n {left:50%;top:0;cursor:ns-resize}.handle-ne {left:100%;top:0;cursor:nesw-resize}
.handle-e {left:100%;top:50%;cursor:ew-resize}.handle-se {left:100%;top:100%;cursor:nwse-resize}.handle-s {left:50%;top:100%;cursor:ns-resize}
.handle-sw {left:0;top:100%;cursor:nesw-resize}.handle-w {left:0;top:50%;cursor:ew-resize}
.handle-n,.handle-s {width:calc(100% - 44px);height:20px}
.handle-e,.handle-w {height:calc(100% - 44px);width:20px}
.crop-loading { background:color-mix(in srgb,var(--bg-card) 80%,transparent); }
.crop-values {border:0;padding:0;margin:0;}
.crop-values :deep(.number-field) { @apply cczj-w-full; }
@media(max-width:640px){
  .crop-layout {grid-template-columns:minmax(0,1fr)}
  .crop-viewport {max-height:42vh;min-height:200px}
  .crop-values {@apply cczj-grid;grid-template-columns:repeat(2,minmax(0,1fr))}
}
</style>
