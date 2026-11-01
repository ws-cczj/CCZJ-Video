<script setup lang="ts">
import type { MotionPreset } from './MotionTransition.vue'
import { useMotionStore } from '../../stores/motion'
import { computed } from 'vue'

const props = withDefaults(defineProps<{
  preset?: Extract<MotionPreset, 'toast' | 'list'>
  appear?: boolean
  tag?: string
}>(), {
  preset: 'list',
  appear: false,
  tag: 'div',
})
const motion = useMotionStore()
const enabled = computed(() => motion.allows(props.preset === 'toast' ? 'feedback' : 'pages'))
function holdToast(element: Element): void {
  if (props.preset !== 'toast' || !(element instanceof HTMLElement)) return
  element.style.width = `${element.offsetWidth}px`
  element.style.top = `${element.offsetTop}px`
}
</script>

<template>
  <TransitionGroup :name="`cczj-motion-${preset}`" :appear="appear" :tag="tag" :css="enabled" :move-class="enabled ? undefined : 'cczj-motion-move-off'" @before-leave="holdToast">
    <slot />
  </TransitionGroup>
</template>
