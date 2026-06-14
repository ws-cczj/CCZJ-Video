<script setup lang="ts">
defineProps<{
  /** 标签变体 */
  variant?: 'default' | 'primary' | 'success' | 'warning' | 'danger'
  /** 尺寸 */
  size?: 'sm' | 'md'
  /** 是否为圆角 pill 样式 */
  pill?: boolean
  /** 是否可关闭 */
  closable?: boolean
  /** 是否激活/选中状态 */
  active?: boolean
}>()

const emit = defineEmits<{
  (e: 'close'): void
}>()
</script>

<template>
  <span
    class="ui-tag"
    :class="[
      `ui-tag--${variant || 'default'}`,
      `ui-tag--${size || 'md'}`,
      {
        'ui-tag--pill': pill !== false,
        'ui-tag--active': active,
        'ui-tag--clickable': !!$attrs.onClick,
      },
    ]"
  >
    <span class="ui-tag__label"><slot /></span>
    <button
      v-if="closable"
      class="ui-tag__close"
      type="button"
      @click.stop="emit('close')"
      aria-label="移除"
    >
      <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
        <line x1="18" y1="6" x2="6" y2="18" /><line x1="6" y1="6" x2="18" y2="18" />
      </svg>
    </button>
  </span>
</template>

<style scoped>
.ui-tag {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  border: 1px solid var(--border);
  border-radius: 5px;
  background: var(--bg-secondary);
  color: var(--text-secondary);
  font-family: inherit;
  font-weight: 500;
  white-space: nowrap;
  transition: all 0.15s ease;
  user-select: none;
  -webkit-user-select: none;
}

/* Size */
.ui-tag--sm  { padding: 3px 10px; font-size: 11px; }
.ui-tag--md  { padding: 6px 14px; font-size: 13px; }

/* Pill (rounded) — default */
.ui-tag--pill {
  border-radius: 6px;
}

/* Clickable / Interactive */
.ui-tag--clickable {
  cursor: pointer;
}
.ui-tag--clickable:hover {
  border-color: var(--accent);
  color: var(--accent);
}

/* Active / Selected */
.ui-tag--active {
  background: var(--accent);
  border-color: var(--accent);
  color: var(--accent-contrast);
  box-shadow: 0 2px 8px var(--accent-alpha-35);
}
.ui-tag--active:hover {
  color: var(--accent-contrast);
  border-color: var(--accent);
}

/* Variants */
.ui-tag--primary {
  background: var(--accent-alpha-10);
  border-color: var(--accent-alpha-20);
  color: var(--accent);
}
.ui-tag--success {
  background: var(--success-alpha-10);
  border-color: rgba(22, 163, 74, 0.2);
  color: var(--success);
}
.ui-tag--warning {
  background: rgba(245, 158, 11, 0.1);
  border-color: rgba(245, 158, 11, 0.2);
  color: #b45309;
}
.ui-tag--danger {
  background: var(--danger-alpha-10);
  border-color: rgba(229, 57, 53, 0.2);
  color: var(--danger);
}

/* Close button */
.ui-tag__close {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 16px;
  height: 16px;
  margin: -2px -4px -2px 2px;
  padding: 0;
  border: none;
  background: transparent;
  color: inherit;
  opacity: 0.6;
  cursor: pointer;
  border-radius: 3px;
  transition: opacity 0.15s ease, background 0.15s ease;
}
.ui-tag__close:hover {
  opacity: 1;
  background: rgba(0, 0, 0, 0.1);
}
</style>