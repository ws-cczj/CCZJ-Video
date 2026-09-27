<script setup lang="ts">
import type { MotionPreset } from './MotionTransition.vue'

withDefaults(defineProps<{
  preset?: Extract<MotionPreset, 'toast' | 'list'>
  appear?: boolean
  tag?: string
}>(), {
  preset: 'list',
  appear: false,
  tag: 'div',
})
</script>

<template>
  <TransitionGroup :name="`cczj-motion-${preset}`" :appear="appear" :tag="tag">
    <slot />
  </TransitionGroup>
</template>
