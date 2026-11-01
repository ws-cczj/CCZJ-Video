<script setup lang="ts">
import { computed } from 'vue'
import { useMotionStore } from '../../stores/motion'
export type MotionPreset = 'fade' | 'page' | 'dialog' | 'modal' | 'sheet' | 'dropdown' | 'slide-up' | 'toast' | 'toast-bottom' | 'list'

const props = withDefaults(defineProps<{
  preset?: MotionPreset
  appear?: boolean
  mode?: 'in-out' | 'out-in' | 'default'
}>(), {
  preset: 'fade',
  appear: false,
  mode: 'default',
})
const motion = useMotionStore()
const enabled = computed(() => motion.allows(['dialog','modal','sheet','dropdown','slide-up'].includes(props.preset) ? 'overlays' : props.preset.startsWith('toast') ? 'feedback' : 'pages'))
const leaving = new WeakMap<Element, { inert: boolean; hidden: string | null }>()
function beforeLeave(element: Element): void {
  leaving.set(element, { inert: element.hasAttribute('inert'), hidden: element.getAttribute('aria-hidden') })
  element.setAttribute('inert', '')
  element.setAttribute('aria-hidden', 'true')
}
function beforeEnter(element: Element): void {
  const previous = leaving.get(element)
  if (!previous) return
  if (!previous.inert) element.removeAttribute('inert')
  if (previous.hidden === null) element.removeAttribute('aria-hidden')
  else element.setAttribute('aria-hidden', previous.hidden)
  leaving.delete(element)
}
</script>

<template>
  <Transition
    :name="`cczj-motion-${preset}`"
    :appear="appear"
    :mode="mode === 'default' ? undefined : mode"
    :css="enabled"
    @before-leave="beforeLeave"
    @before-enter="beforeEnter"
  >
    <slot />
  </Transition>
</template>
