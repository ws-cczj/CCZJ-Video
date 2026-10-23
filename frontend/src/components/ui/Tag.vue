<script setup lang="ts">
import { tr } from '../../locales'
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
    class="ui-tag cczj-inline-flex cczj-items-center cczj-gap-2"
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
      class="ui-tag__close cczj-inline-flex cczj-items-center cczj-justify-center"
      type="button"
      @click.stop="emit('close')"
      :aria-label="tr('common.remove')"
    >
      <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
        <line x1="18" y1="6" x2="6" y2="18" /><line x1="6" y1="6" x2="18" y2="18" />
      </svg>
    </button>
  </span>
</template>

<style scoped>
.ui-tag {
  @apply cczj-border;
  border-radius: 5px;
  background: var(--bg-secondary);
  @apply cczj-text-secondary;
  font-family: inherit;
  @apply cczj-font-medium cczj-whitespace-nowrap;
  transition: background-color var(--transition-fast), border-color var(--transition-fast), color var(--transition-fast), box-shadow var(--transition-fast), opacity var(--transition-fast);
  user-select: none;
  -webkit-user-select: none;
}

/* Size */
.ui-tag--sm  { @apply cczj-px-5-py-2 cczj-text-11; }
.ui-tag--md  { padding: 6px 14px; @apply cczj-text-13; }

/* Pill (rounded) — default */
.ui-tag--pill {
  @apply cczj-rounded;
}

/* Clickable / Interactive */
.ui-tag--clickable {
  @apply cczj-cursor-pointer;
}
.ui-tag--clickable:hover {
  @apply cczj-border-primary cczj-text-accent;
}

/* Active / Selected */
.ui-tag--active {
  background: var(--accent);
  @apply cczj-border-primary;
  color: var(--accent-contrast);
  box-shadow: 0 2px 8px var(--accent-alpha-35);
}
.ui-tag--active:hover {
  color: var(--accent-contrast);
  @apply cczj-border-primary;
}

/* Variants */
.ui-tag--primary {
  background: var(--accent-alpha-10);
  border-color: var(--accent-alpha-20);
  @apply cczj-text-accent;
}
.ui-tag--success {
  background: var(--success-alpha-10);
  border-color: var(--success-alpha-10);
  @apply cczj-text-success;
}
.ui-tag--warning {
  background: var(--warning-alpha-10);
  border-color: var(--warning-alpha-10);
  color: var(--warning-text);
}
.ui-tag--danger {
  @apply cczj-bg-danger-alpha;
  border-color: var(--danger-alpha-10);
  @apply cczj-text-danger;
}

/* Close button */
.ui-tag__close {
  @apply cczj-w-16 cczj-h-16;
  margin: -2px -4px -2px 2px;
  padding: 0;
  border: none;
  background: transparent;
  color: inherit;
  @apply cczj-opacity-60 cczj-cursor-pointer;
  border-radius: 3px;
  transition: opacity var(--transition-fast), background-color var(--transition-fast);
}
.ui-tag__close:hover {
  @apply cczj-opacity-100;
  background: rgba(0, 0, 0, 0.1);
}
</style>
