<script setup lang="ts">
export type MotionPreset = 'fade' | 'page' | 'dialog' | 'dropdown' | 'slide-up' | 'toast' | 'toast-bottom' | 'list'

withDefaults(defineProps<{
  preset?: MotionPreset
  appear?: boolean
  mode?: 'in-out' | 'out-in' | 'default'
}>(), {
  preset: 'fade',
  appear: false,
  mode: 'default',
})
</script>

<template>
  <Transition
    :name="`cczj-motion-${preset}`"
    :appear="appear"
    :mode="mode === 'default' ? undefined : mode"
  >
    <slot />
  </Transition>
</template>
