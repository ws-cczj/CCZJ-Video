<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../../components/Icon.vue'
import { MotionTransition } from '../../components/ui'
const props=defineProps<{open:boolean;title:string;variant:'info'|'shortcut'}>()
const emit=defineEmits<{close:[];keydown:[event:KeyboardEvent]}>()
const { t }=useI18n()
const overlay=ref<HTMLElement>()
let previousFocus:HTMLElement|null=null
function restoreFocus():void {
  if(previousFocus?.isConnected)previousFocus.focus({preventScroll:true})
  previousFocus=null
}
watch(()=>props.open,async open=>{
  if(!open){restoreFocus();return}
  previousFocus=document.activeElement instanceof HTMLElement?document.activeElement:null
  await nextTick()
  if(props.open)overlay.value?.focus({preventScroll:true})
},{immediate:true,flush:'post'})
function onKeyDown(event:KeyboardEvent):void {
  emit('keydown',event)
  event.stopPropagation()
  if(event.defaultPrevented)return
  if(event.key==='Escape'){event.stopPropagation();emit('close')}
  if(event.key!=='Tab')return
  const items=Array.from(overlay.value?.querySelectorAll<HTMLElement>('button:not(:disabled),input:not(:disabled),select:not(:disabled),[tabindex="0"]')||[]).filter(el=>el.getClientRects().length>0)
  if(!items.length){event.preventDefault();return}
  const first=items[0],last=items[items.length-1]
  if(event.shiftKey&&(document.activeElement===first||document.activeElement===overlay.value)){event.preventDefault();last.focus()}
  else if(!event.shiftKey&&(document.activeElement===last||document.activeElement===overlay.value)){event.preventDefault();first.focus()}
}
onBeforeUnmount(restoreFocus)
</script>
<template>
  <MotionTransition preset="fade">
    <div v-if="open" ref="overlay" class="vp-modal-overlay" tabindex="-1" role="dialog" aria-modal="true" :aria-label="title" @click="emit('close')" @keydown="onKeyDown" @wheel.stop>
      <MotionTransition preset="dialog" appear>
        <div class="vp-modal" :class="`vp-modal-${variant}`" @click.stop>
          <div class="vp-modal-header"><span>{{title}}</span><button class="vp-modal-close" :aria-label="t('common.close')" @click="emit('close')"><Icon name="x" :size="16" /></button></div>
          <slot />
        </div>
      </MotionTransition>
    </div>
  </MotionTransition>
</template>
<style scoped>
.vp-modal-overlay {
  @apply cczj-absolute;
  inset: 0;
  background: rgba(0, 0, 0, 0.6);
  @apply cczj-flex cczj-items-center cczj-justify-center cczj-z-100;
}

.vp-modal {
  background: rgba(28, 28, 32, 0.97);
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: 14px;
  min-width: 320px;
  max-width: 90vw;
  width: 440px;
  box-shadow: 0 12px 40px rgba(0, 0, 0, 0.7);
  @apply cczj-overflow-hidden cczj-flex cczj-flex-col;
  max-height: 85vh;
}

.vp-modal-info {
  max-width: 480px;
  min-width: 360px;
  width: 480px;
}

.vp-modal-shortcut {
  max-width: 90vw;
  min-width: 380px;
  width: 520px;
}

.vp-modal-shortcut :deep(.vp-modal-body) {
  max-height: calc(85vh - 140px);
}

.vp-modal-header {
  @apply cczj-flex cczj-items-center cczj-justify-between;
  padding: 14px 18px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.08);
  font-size: calc(15rem / 14);
  @apply cczj-font-semibold cczj-text-white;
}

.vp-modal-close {
  width: 28px;
  height: 28px;
  @apply cczj-rounded;
  border: none;
  background: transparent;
  color: rgba(255, 255, 255, 0.5);
  @apply cczj-cursor-pointer cczj-flex cczj-items-center cczj-justify-center;
  transition: all 0.15s;
}

.vp-modal-close:hover {
  background: rgba(255, 255, 255, 0.1);
  @apply cczj-text-white;
}

:deep(.vp-modal-body) {
  padding: 14px 18px;
  max-height: calc(85vh - 120px);
  @apply cczj-overflow-y-auto;
  scrollbar-width: thin;
  scrollbar-color: rgba(255, 255, 255, 0.12) transparent;
}

:deep(.vp-modal-body::-webkit-scrollbar) {
  width: 5px;
}

:deep(.vp-modal-body::-webkit-scrollbar-thumb) {
  background: rgba(255, 255, 255, 0.12);
  border-radius: 3px;
}

:deep(.vp-modal-footer) {
  @apply cczj-flex cczj-justify-end cczj-gap-4;
  padding: 12px 18px;
  border-top: 1px solid rgba(255, 255, 255, 0.08);
}

</style>
