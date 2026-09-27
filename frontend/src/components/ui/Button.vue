<script setup lang="ts">
import { computed, useAttrs } from 'vue'

const props = defineProps<{
  /** 按钮变体 */
  variant?: 'primary' | 'secondary' | 'success' | 'warning' | 'danger' | 'info' | 'ghost' | 'text' | 'overlay'
  /** 按钮尺寸 */
  size?: 'sm' | 'md' | 'lg'
  /** 是否为图标按钮（正方形） */
  icon?: boolean
  /** 是否禁用 */
  disabled?: boolean
  /** 是否加载中 */
  loading?: boolean
  /** 是否块级按钮 */
  block?: boolean
  /** 图标按钮的无障碍名称；同时保留 title 作为可见提示。 */
  ariaLabel?: string
}>()

const attrs = useAttrs()
const accessibleLabel = computed(() => props.ariaLabel
  || (typeof attrs['aria-label'] === 'string' ? attrs['aria-label'] : undefined)
  || (typeof attrs.title === 'string' ? attrs.title : undefined))
</script>

<template>
  <button
    class="ui-btn"
    :class="[
      `ui-btn--${variant || 'secondary'}`,
      `ui-btn--${size || 'md'}`,
      {
        'ui-btn--icon': icon,
        'ui-btn--block': block,
        'ui-btn--disabled': disabled,
        'ui-btn--loading': loading,
      },
    ]"
    :disabled="disabled || loading"
    :aria-busy="loading"
    :aria-label="accessibleLabel"
  >
    <span v-if="loading" class="ui-btn__spinner"></span>
    <span v-else class="ui-btn__content">
      <slot />
    </span>
  </button>
</template>

<style scoped>
/* ========== Button Base ========== */
.ui-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  border: 1px solid transparent;
  border-radius: 4px;
  font-weight: 600;
  line-height: 1;
  white-space: nowrap;
  transition: background-color var(--transition-fast), border-color var(--transition-fast), color var(--transition-fast), box-shadow var(--transition-fast), transform var(--transition-fast), opacity var(--transition-fast);
  font-family: inherit;
  user-select: none;
  -webkit-user-select: none;
  text-decoration: none;
  cursor: pointer;
  touch-action: manipulation;
}
.ui-btn:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 2px;
}

/* ========== Size ========== */
.ui-btn--sm  { padding: 5px 12px; font-size: 12px; border-radius: 3px; }
.ui-btn--md  { padding: 8px 16px; font-size: 13px; }
.ui-btn--lg  { padding: 10px 22px; font-size: 14px; }

.ui-btn--icon.ui-btn--sm  { padding: 5px; width: 28px; height: 28px; }
.ui-btn--icon.ui-btn--md  { padding: 7px; width: 34px; height: 34px; }
.ui-btn--icon.ui-btn--lg  { padding: 9px; width: 40px; height: 40px; }

/* ========== Variant: Primary ========== */
.ui-btn--primary {
  background: var(--btn-solid);
  color: var(--btn-solid-text);
  border-color: var(--btn-solid);
  box-shadow: 0 2px 8px var(--btn-solid-alpha-20);
}
.ui-btn--primary:hover {
  background: var(--btn-solid-hover);
  border-color: var(--btn-solid-hover);
  transform: translateY(-1px);
  box-shadow: 0 4px 12px var(--btn-solid-alpha-35);
}
.ui-btn--primary:active {
  transform: translateY(0);
  box-shadow: 0 1px 4px var(--btn-solid-alpha-20);
}

/* ========== Variant: Secondary ========== */
.ui-btn--secondary {
  background: var(--bg-secondary);
  color: var(--text-secondary);
  border-color: var(--border);
}
.ui-btn--secondary:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
  border-color: var(--accent);
}
.ui-btn--secondary:active { background: var(--border); }

/* ========== Semantic variants ========== */
.ui-btn--success,
.ui-btn--warning,
.ui-btn--info {
  color: var(--semantic-contrast);
  background: var(--semantic-color);
  border-color: var(--semantic-color);
  box-shadow: 0 2px 8px var(--semantic-alpha);
}
.ui-btn--success {
  --semantic-color: var(--success);
  --semantic-contrast: var(--success-contrast);
  --semantic-alpha: var(--success-alpha-10);
}
.ui-btn--success:hover { --semantic-color: var(--success-hover); transform: translateY(-1px); }
.ui-btn--warning {
  --semantic-color: var(--warning);
  --semantic-contrast: var(--warning-contrast);
  --semantic-alpha: var(--warning-alpha-10);
}
.ui-btn--warning:hover { --semantic-color: var(--warning-hover); transform: translateY(-1px); }
.ui-btn--info {
  --semantic-color: var(--info);
  --semantic-contrast: var(--info-contrast);
  --semantic-alpha: var(--info-alpha-10);
}
.ui-btn--info:hover { --semantic-color: var(--info-hover); transform: translateY(-1px); }
.ui-btn--success:active,
.ui-btn--warning:active,
.ui-btn--info:active { transform: translateY(0); }

/* ========== Variant: Danger ========== */
.ui-btn--danger {
  background: transparent;
  color: var(--danger);
  border-color: var(--danger);
}
.ui-btn--danger:hover {
  background: var(--danger);
  color: var(--danger-contrast);
  transform: translateY(-1px);
}
.ui-btn--danger:active { transform: translateY(0); opacity: 0.9; }

/* ========== Variant: Ghost ========== */
.ui-btn--ghost {
  background: transparent;
  color: var(--accent);
  border-color: var(--accent);
}
.ui-btn--ghost:hover { background: var(--accent-alpha-10); }
.ui-btn--ghost:active { background: var(--accent-alpha-20); }

/* ========== Variant: Text ========== */
.ui-btn--text {
  background: transparent;
  color: var(--text-secondary);
  border-color: transparent;
}
.ui-btn--text:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}
.ui-btn--text:active { color: var(--accent); }

/* ========== Variant: Overlay ========== */
/* 浮动在卡片/图片上方的按钮（如删除、关闭标记）。
 * 不设置 position/overflow，不影响外部 absolute 定位和 stacking context。
 * 背景半透明，hover 时变色。 */
.ui-btn--overlay {
  background: rgba(0, 0, 0, 0.6);
  color: #fff;
  border: none;
  backdrop-filter: blur(4px);
  -webkit-backdrop-filter: blur(4px);
}
.ui-btn--overlay:hover {
  background: var(--danger);
  color: var(--danger-contrast);
}
.ui-btn--overlay:active {
  background: var(--danger-hover);
}

/* ========== Block / Disabled / Loading ========== */
.ui-btn--block { width: 100%; }
.ui-btn--disabled { opacity: 0.5; cursor: not-allowed; pointer-events: none; }

/* loading 状态需要 position:relative 给 spinner 定位，overflow:hidden 裁剪 spinner 动画 */
.ui-btn--loading {
  cursor: wait;
  pointer-events: none;
  position: relative;
  overflow: hidden;
}

.ui-btn__spinner {
  display: inline-block;
  width: 14px;
  height: 14px;
  border: 2px solid currentColor;
  border-top-color: transparent;
  border-radius: 50%;
  animation: cczj-spin 800ms linear infinite;
}

.ui-btn__content {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

</style>
