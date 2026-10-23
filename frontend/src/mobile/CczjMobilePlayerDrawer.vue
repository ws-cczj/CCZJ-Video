<script setup lang="ts">
import './player/presentation.css'
import { nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '../components/Icon.vue'

const props = defineProps<{ open:boolean; title:string; fullscreen:boolean; host?:HTMLElement; kind?:'quick'|'more'; lazy?:boolean; placement?:'right'|'bottom' }>()
const emit = defineEmits<{close:[]}>()
const { t } = useI18n()
const panel = ref<HTMLElement>()
const contentPresent = ref(props.open)
let returnFocus: HTMLElement | null = null
watch(() => props.open, async open => {
  if (open) {
    contentPresent.value = true
    returnFocus = document.activeElement as HTMLElement | null
    await nextTick()
    if (props.open) panel.value?.querySelector<HTMLButtonElement>('header button')?.focus({preventScroll:true})
  } else if (returnFocus?.isConnected) returnFocus.focus({preventScroll:true})
}, {immediate:true})
function onKey(event: KeyboardEvent): void {
  if (event.key === 'Escape') { event.preventDefault(); emit('close'); return }
  if (event.key !== 'Tab') return
  const targets = Array.from(panel.value?.querySelectorAll<HTMLElement>('button:not(:disabled),input:not(:disabled),[tabindex="0"]') || []).filter(el => el.getBoundingClientRect().width > 0)
  const first = targets[0], last = targets[targets.length-1]
  if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus() }
  else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus() }
}
function afterClose(): void { if (!props.open && props.lazy) contentPresent.value = false }
onBeforeUnmount(() => { if (props.open && returnFocus?.isConnected) returnFocus.focus({preventScroll:true}) })
</script>

<template>
  <Teleport :to="fullscreen && host ? host : 'body'">
    <Transition :name="placement==='bottom'?'cczj-mobile-player-sheet':'cczj-mobile-player-drawer'" @after-leave="afterClose">
      <div v-show="open" class="cczj-mobile-quick-mask" :class="{'cczj-mobile-quick-fullscreen':fullscreen,'cczj-mobile-player-more-mask':kind==='more','cczj-mobile-player-more-fullscreen':kind==='more'&&fullscreen,'cczj-mobile-player-bottom-mask':placement==='bottom'}" @click.self="emit('close')" @pointerdown.stop @wheel.stop @keydown.stop="onKey">
        <section ref="panel" class="cczj-mobile-drawer-panel cczj-mobile-quick-panel" :class="{'cczj-mobile-player-more':kind==='more'}" role="dialog" :aria-label="title" aria-modal="true" @click.stop @dblclick.stop>
          <header><strong>{{title}}</strong><button :aria-label="t('common.close')" @click="emit('close')"><Icon name="close" :size="22" /></button></header>
          <slot name="navigation" />
          <div class="cczj-mobile-quick-body" :class="{'cczj-mobile-player-more-body':kind==='more'}"><slot v-if="!lazy || contentPresent" /></div>
        </section>
      </div>
    </Transition>
  </Teleport>
</template>
