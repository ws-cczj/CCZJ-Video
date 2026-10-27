<script setup lang="ts">
import { computed } from 'vue'
const props = defineProps<{ icon?: string; title: string; description?: string }>()
const descriptionText = computed(() => props.description?.trim() === props.title.trim() ? '' : props.description)
</script>

<template>
  <div class="empty-state cczj-flex cczj-flex-col cczj-items-center cczj-justify-center cczj-gap-6">
    <div class="empty-icon"><slot name="icon">{{ icon || '📭' }}</slot></div>
    <h3 class="empty-title">{{ title }}</h3>
    <p v-if="descriptionText" class="empty-desc">{{ descriptionText }}</p>
    <slot></slot>
  </div>
</template>

<style scoped>
.empty-state {
  padding: 80px 20px;
  @apply cczj-text-center;
  animation: cczj-fade-in-up var(--cczj-motion-slow) var(--cczj-motion-ease-enter) backwards;
}

.empty-icon {
  font-size: 56px;
  @apply cczj-leading-none cczj-opacity-70;
  animation: cczj-float 2s ease-in-out infinite;
}

.empty-title {
  @apply cczj-text-lg cczj-font-semibold cczj-text-primary;
  margin: 0;
}

.empty-desc {
  @apply cczj-text-13 cczj-text-secondary;
  margin: 0;
  max-width: 320px;
  line-height: 1.6;
}

</style>
