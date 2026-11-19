<script setup lang="ts">
import { useMotionStore } from '../../stores/motion'

const emit = defineEmits<{ finished: [] }>()
const motion = useMotionStore()
</script>

<template>
  <span class="danmaku-like-feedback" :class="{ 'is-animated': motion.allows('feedback') }"
    aria-hidden="true" @animationend.stop="emit('finished')">+1</span>
</template>

<style scoped>
.danmaku-like-feedback {
  @apply cczj-inline-block cczj-rounded-full cczj-text-sm cczj-font-semibold cczj-pointer-events-none;
  padding: 2px 5px;
  background: var(--btn-solid);
  color: var(--btn-solid-text);
  animation: danmaku-like-hold 620ms step-end both;
}

.danmaku-like-feedback.is-animated { animation: danmaku-like 620ms ease-out both; }

@keyframes danmaku-like-hold {
  from { opacity: 1; }
  to { opacity: 0; }
}

@keyframes danmaku-like {
  0% { opacity: 0; transform: translateY(3px) scale(.8); }
  22% { opacity: 1; transform: translateY(-3px) scale(1.12); }
  65% { opacity: 1; }
  100% { opacity: 0; transform: translateY(-14px) scale(1); }
}
</style>
