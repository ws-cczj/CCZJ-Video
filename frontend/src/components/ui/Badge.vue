<script setup lang="ts">
defineProps<{
  variant?: 'default' | 'primary' | 'success' | 'warning' | 'danger'
  /** 圆点样式（小圆点） */
  dot?: boolean
}>()
</script>

<template>
  <span
    class="ui-badge cczj-inline-flex cczj-items-center cczj-gap-2"
    :class="[
      `ui-badge--${variant || 'default'}`,
      { 'ui-badge--dot': dot },
    ]"
  >
    <span v-if="dot" class="ui-badge__dot cczj-flex-shrink-0"></span>
    <span class="ui-badge__content"><slot /></span>
  </span>
</template>

<style scoped>
.ui-badge {
  @apply cczj-px-5-py-2;
  border-radius: 10px;
  @apply cczj-text-11 cczj-font-medium;
  line-height: 1.4;
  @apply cczj-whitespace-nowrap;
  transition: background 0.15s ease;
}

/* Dot mode */
.ui-badge--dot {
  padding: 0;
  @apply cczj-gap-3;
  background: transparent !important;
}
.ui-badge__dot {
  @apply cczj-w-2 cczj-h-2 cczj-rounded-50;
  background: currentColor;
}

/* Variants */
.ui-badge--default {
  background: var(--bg-hover);
  @apply cczj-text-secondary;
}
.ui-badge--primary {
  background: var(--accent-alpha-10);
  @apply cczj-text-accent;
}
.ui-badge--success {
  background: var(--success-alpha-10);
  @apply cczj-text-success;
}
.ui-badge--warning {
  background: var(--warning-alpha-10);
  color: var(--warning-text);
}
.ui-badge--danger {
  @apply cczj-bg-danger-alpha cczj-text-danger;
}
</style>
