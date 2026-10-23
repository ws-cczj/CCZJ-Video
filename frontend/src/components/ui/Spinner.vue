<script setup lang="ts">
defineProps<{ size?: 'sm' | 'md' | 'lg'; label?: string }>()
</script>

<template>
  <div class="spinner-wrap cczj-flex cczj-flex-col cczj-items-center cczj-justify-center cczj-gap-6" :class="size || 'md'">
    <div class="spinner-ring"></div>
    <span v-if="label" class="label">{{ label }}</span>
  </div>
</template>

<style scoped>
.spinner-wrap {
  padding: 60px 20px;
  @apply cczj-text-muted cczj-text-center;
}

.spinner-ring {
  border: 3px solid var(--border);
  border-top-color: var(--accent);
  @apply cczj-rounded-50;
  animation: cczj-spin 800ms linear infinite;
}

.spinner-wrap.sm .spinner-ring {
  width: 20px;
  height: 20px;
  border-width: 2px;
}

.spinner-wrap.md .spinner-ring {
  @apply cczj-w-32 cczj-h-32;
}

.spinner-wrap.lg .spinner-ring {
  width: 48px;
  height: 48px;
  border-width: 4px;
}

.spinner-wrap .label {
  @apply cczj-text-13 cczj-text-muted;
}

</style>
