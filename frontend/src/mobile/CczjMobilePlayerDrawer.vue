<script setup lang="ts">
defineOptions({ inheritAttrs: false })
import './player/presentation.css'
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../components/Icon.vue'

const props = defineProps<{ open:boolean; title:string; fullscreen:boolean; host?:HTMLElement; contained?:boolean; solid?:boolean; initialHeight?:number; kind?:'quick'|'more'; lazy?:boolean; placement?:'right'|'bottom' }>()
const emit = defineEmits<{close:[]}>()
const { t } = useI18n()
const panel = ref<HTMLElement>()
const placement = computed(() => props.placement ?? (props.fullscreen ? 'right' : 'bottom'))
const sheetHeight = ref(60)
const sheetStyle = computed(() => placement.value === 'bottom' ? {'--cczj-player-sheet-height': `${sheetHeight.value}dvh`} : undefined)
let drag: {id:number;y:number;height:number;viewport:number;handle:HTMLElement} | undefined
function stopResize(event?: PointerEvent): void {
  if (!drag || (event && event.pointerId !== drag.id)) return
  const previous = drag
  drag = undefined
  if (previous.handle.hasPointerCapture(previous.id)) previous.handle.releasePointerCapture(previous.id)
}
function startResize(event: PointerEvent): void {
  if (placement.value !== 'bottom' || event.button !== 0 || !panel.value) return
  stopResize()
  const handle = event.currentTarget as HTMLElement
  drag = {id:event.pointerId,y:event.clientY,height:panel.value.getBoundingClientRect().height,
    viewport:window.visualViewport?.height || window.innerHeight,handle}
  handle.setPointerCapture(event.pointerId)
  event.preventDefault()
}
function resize(event: PointerEvent): void {
  if (!drag || event.pointerId !== drag.id) return
  sheetHeight.value = Math.min(90,Math.max(30,(drag.height + drag.y - event.clientY) / drag.viewport * 100))
}
function resizeWithKey(event: KeyboardEvent): void {
  const deltas:Record<string,number> = {ArrowUp:5,ArrowDown:-5,PageUp:10,PageDown:-10}
  const delta = deltas[event.key]
  if (event.key === 'Home' || event.key === 'End') {
    event.preventDefault()
    sheetHeight.value = event.key === 'Home' ? 30 : 90
    return
  }
  if (delta === undefined) return
  event.preventDefault()
  sheetHeight.value = Math.min(90,Math.max(30,sheetHeight.value + delta))
}
const contentPresent = ref(props.open)
let returnFocus: HTMLElement | null = null
watch(() => props.open, async open => {
  if (open) {
    sheetHeight.value = Math.min(90,Math.max(30,props.initialHeight ?? 60))
    contentPresent.value = true
    returnFocus = document.activeElement as HTMLElement | null
    await nextTick()
    if (props.open) panel.value?.querySelector<HTMLButtonElement>('header button')?.focus({preventScroll:true})
  } else {
    stopResize()
    if (returnFocus?.isConnected) returnFocus.focus({preventScroll:true})
  }
}, {immediate:true})
watch(placement,() => stopResize())
function onKey(event: KeyboardEvent): void {
  if (event.key === 'Escape') { event.preventDefault(); emit('close'); return }
  if (event.key !== 'Tab') return
  const targets = Array.from(panel.value?.querySelectorAll<HTMLElement>('button:not(:disabled),input:not(:disabled),[tabindex="0"]') || []).filter(el => el.getBoundingClientRect().width > 0)
  const first = targets[0], last = targets[targets.length-1]
  if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus() }
  else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus() }
}
function afterClose(): void { if (!props.open && props.lazy) contentPresent.value = false }
onBeforeUnmount(() => { stopResize();if (props.open && returnFocus?.isConnected) returnFocus.focus({preventScroll:true}) })
</script>

<template>
  <Teleport :to="(fullscreen || contained) && host ? host : 'body'">
    <Transition :name="placement==='bottom'?'cczj-mobile-player-sheet':'cczj-mobile-player-drawer'" @after-leave="afterClose">
      <div v-bind="$attrs" v-show="open" class="cczj-mobile-quick-mask" :class="{'cczj-mobile-quick-fullscreen':fullscreen || contained,'cczj-player-contained-drawer':contained,'cczj-player-solid-drawer':solid,'cczj-mobile-player-more-mask':kind==='more','cczj-mobile-player-more-fullscreen':kind==='more'&&fullscreen,'cczj-mobile-player-bottom-mask':placement==='bottom'}" @click.self="emit('close')" @pointerdown.stop @wheel.stop @keydown.stop="onKey">
        <section ref="panel" class="cczj-mobile-drawer-panel cczj-mobile-quick-panel" :class="{'cczj-mobile-player-more':kind==='more'}" :style="sheetStyle" role="dialog" :aria-label="title" aria-modal="true" @click.stop @dblclick.stop>
          <button v-if="placement==='bottom'" type="button" class="cczj-mobile-sheet-handle" role="slider" :aria-label="t('mobile.resizeDrawer')" aria-orientation="vertical" :aria-valuemin="30" :aria-valuemax="90" :aria-valuenow="Math.round(sheetHeight)" :aria-valuetext="t('mobile.drawerHeight',{percent:Math.round(sheetHeight)})" @pointerdown.stop="startResize" @pointermove.stop="resize" @pointerup.stop="stopResize" @pointercancel.stop="stopResize" @lostpointercapture.stop="stopResize" @keydown="resizeWithKey"><span aria-hidden="true" /></button>
          <header><strong>{{title}}</strong><button :aria-label="t('common.close')" @click="emit('close')"><Icon name="close" :size="22" /></button></header>
          <slot name="navigation" />
          <div class="cczj-mobile-quick-body" :class="{'cczj-mobile-player-more-body':kind==='more'}"><slot v-if="!lazy || contentPresent" /></div>
        </section>
      </div>
    </Transition>
  </Teleport>
</template>
