<script setup lang="ts">
defineProps<{
  variant?: 'default' | 'primary' | 'success' | 'warning' | 'danger'
  /** 圆点样式（小圆点） */
  dot?: boolean
}>()
</script>

<template>
  <span
    class="ui-badge"
    :class="[
      `ui-badge--${variant || 'default'}`,
      { 'ui-badge--dot': dot },
    ]"
  >
    <span v-if="dot" class="ui-badge__dot"></span>
    <span class="ui-badge__content"><slot /></span>
  </span>
</template>

<style scoped>
.ui-badge {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 3px 10px;
  border-radius: 10px;
  font-size: 11px;
  font-weight: 500;
  line-height: 1.4;
  white-space: nowrap;
  transition: background 0.15s ease;
}

/* Dot mode */
.ui-badge--dot {
  padding: 0;
  gap: 6px;
  background: transparent !important;
}
.ui-badge__dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
  background: currentColor;
}

/* Variants */
.ui-badge--default {
  background: var(--bg-hover);
  color: var(--text-secondary);
}
.ui-badge--primary {
  background: var(--accent-alpha-10);
  color: var(--accent);
}
.ui-badge--success {
  background: var(--success-alpha-10);
  color: var(--success);
}
.ui-badge--warning {
  background: rgba(245, 158, 11, 0.1);
  color: #b45309;
}
.ui-badge--danger {
  background: var(--danger-alpha-10);
  color: var(--danger);
}
</style>