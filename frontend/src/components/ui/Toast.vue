<script setup lang="ts">
import { tr } from '../../locales'
defineProps<{
  /** 等级：info / warn / error */
  level?: 'info' | 'warn' | 'error'
  /** 标题 */
  title?: string
  /** 时间文本 */
  time?: string
  /** 明细内容（可展开） */
  detail?: string
}>()

const emit = defineEmits<{
  (e: 'close'): void
}>()

const iconMap: Record<string, string> = {
  info: 'i',
  warn: '!',
  error: '!',
}
const levelIcon = (lvl: string) => iconMap[lvl] || 'i'
</script>

<template>
  <div class="ui-toast cczj-flex cczj-items-start cczj-gap-5" :class="`ui-toast--${level || 'info'}`" role="alert">
    <!-- 左侧图标 -->
    <div class="ui-toast__icon cczj-flex cczj-items-center cczj-justify-center cczj-flex-shrink-0" :class="`ui-toast__icon--${level || 'info'}`">
      <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
        <template v-if="level === 'error' || level === 'warn'">
          <line x1="18" y1="6" x2="6" y2="18" />
          <line x1="6" y1="6" x2="18" y2="18" />
        </template>
        <template v-else>
          <circle cx="12" cy="12" r="10" />
          <line x1="12" y1="16" x2="12" y2="12" />
          <line x1="12" y1="8" x2="12.01" y2="8" />
        </template>
      </svg>
    </div>

    <!-- 内容区 -->
    <div class="ui-toast__body cczj-min-w-0">
      <div class="ui-toast__header cczj-flex cczj-items-center cczj-justify-between cczj-gap-4">
        <span v-if="title" class="ui-toast__title">{{ title }}</span>
        <span v-if="time" class="ui-toast__time cczj-flex-shrink-0">{{ time }}</span>
      </div>
      <slot>
        <div v-if="detail" class="ui-toast__detail">
          <details>
            <summary>{{ tr('common.details') }}</summary>
            <pre>{{ detail }}</pre>
          </details>
        </div>
      </slot>
    </div>

    <!-- 关闭按钮 -->
    <button class="ui-toast__close cczj-flex cczj-items-center cczj-justify-center cczj-flex-shrink-0" type="button" @click.stop="emit('close')" :aria-label="tr('common.close')">
      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
        <line x1="18" y1="6" x2="6" y2="18" /><line x1="6" y1="6" x2="18" y2="18" />
      </svg>
    </button>
  </div>
</template>

<style scoped>
.ui-toast {
  min-width: 280px;
  max-width: 420px;
  background: var(--bg-card);
  @apply cczj-border;
  border-radius: var(--radius-lg);
  padding: 12px 14px;
  @apply cczj-shadow-lg cczj-text-primary cczj-overflow-hidden cczj-pointer-events-auto;
}

/* 等级边框色 —— 用左侧 accent bar */
.ui-toast::before {
  content: '';
  @apply cczj-absolute cczj-top-0 cczj-left-0 cczj-bottom-0;
  width: 4px;
  border-top-left-radius: var(--radius-lg);
  border-bottom-left-radius: var(--radius-lg);
  background: var(--accent);
}
.ui-toast--warn::before { background: var(--warning); }
.ui-toast--error::before { background: var(--danger); }

/* 图标 */
.ui-toast__icon {
  width: 28px;
  height: 28px;
  @apply cczj-rounded-50;
  color: var(--accent-contrast);
  background: var(--accent);
}
.ui-toast__icon--warn { background: var(--warning); }
.ui-toast__icon--warn { color: var(--warning-contrast); }
.ui-toast__icon--error { background: var(--danger); color: var(--danger-contrast); }

/* 正文 */
.ui-toast__body {
  @apply cczj-flex-1;
}
.ui-toast__header {
  @apply cczj-mb-2;
}
.ui-toast__title {
  font-size: var(--font);
  @apply cczj-font-bold;
}
.ui-toast__time {
  font-size: var(--font-xs);
  @apply cczj-text-muted;
}

/* Detail 展开 */
.ui-toast__detail {
  @apply cczj-mt-3;
  font-size: var(--font-xs);
}
.ui-toast__detail summary {
  @apply cczj-cursor-pointer cczj-text-accent;
  user-select: none;
  -webkit-user-select: none;
}
.ui-toast__detail summary:hover { @apply cczj-underline; }
.ui-toast__detail pre {
  margin: 6px 0 0 0;
  padding: 8px 10px;
  background: var(--bg-secondary);
  border-radius: var(--radius-sm);
  @apply cczj-text-secondary;
  font-family: 'SF Mono', Consolas, 'Courier New', monospace;
  font-size: var(--font-xs);
  @apply cczj-whitespace-pre;
  word-break: break-word;
  max-height: 160px;
  @apply cczj-overflow-y-auto;
  user-select: text;
  -webkit-user-select: text;
}

/* 关闭按钮 */
.ui-toast__close {
  background: transparent;
  border: none;
  @apply cczj-cursor-pointer;
  padding: 2px;
  border-radius: var(--radius-sm);
  @apply cczj-text-muted;
  transition: background var(--transition-fast), color var(--transition-fast);
}
.ui-toast__close:hover {
  background: var(--bg-hover);
  @apply cczj-text-primary;
}
</style>
