<script setup lang="ts">
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
  <div class="ui-toast" :class="`ui-toast--${level || 'info'}`" role="alert">
    <!-- 左侧图标 -->
    <div class="ui-toast__icon" :class="`ui-toast__icon--${level || 'info'}`">
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
    <div class="ui-toast__body">
      <div class="ui-toast__header">
        <span v-if="title" class="ui-toast__title">{{ title }}</span>
        <span v-if="time" class="ui-toast__time">{{ time }}</span>
      </div>
      <slot>
        <div v-if="detail" class="ui-toast__detail">
          <details>
            <summary>详细信息</summary>
            <pre>{{ detail }}</pre>
          </details>
        </div>
      </slot>
    </div>

    <!-- 关闭按钮 -->
    <button class="ui-toast__close" type="button" @click.stop="emit('close')" aria-label="关闭">
      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
        <line x1="18" y1="6" x2="6" y2="18" /><line x1="6" y1="6" x2="18" y2="18" />
      </svg>
    </button>
  </div>
</template>

<style scoped>
.ui-toast {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  min-width: 280px;
  max-width: 420px;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  padding: 12px 14px;
  box-shadow: var(--shadow-lg);
  color: var(--text-primary);
  overflow: hidden;
  pointer-events: auto;
}

/* 等级边框色 —— 用左侧 accent bar */
.ui-toast::before {
  content: '';
  position: absolute;
  top: 0;
  left: 0;
  bottom: 0;
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
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  color: #fff;
  background: var(--accent);
}
.ui-toast__icon--warn { background: var(--warning); }
.ui-toast__icon--error { background: var(--danger); }

/* 正文 */
.ui-toast__body {
  flex: 1;
  min-width: 0;
}
.ui-toast__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  margin-bottom: 4px;
}
.ui-toast__title {
  font-size: var(--font);
  font-weight: 700;
}
.ui-toast__time {
  font-size: var(--font-xs);
  color: var(--text-muted);
  flex-shrink: 0;
}

/* Detail 展开 */
.ui-toast__detail {
  margin-top: 6px;
  font-size: var(--font-xs);
}
.ui-toast__detail summary {
  cursor: pointer;
  color: var(--accent);
  user-select: none;
  -webkit-user-select: none;
}
.ui-toast__detail summary:hover { text-decoration: underline; }
.ui-toast__detail pre {
  margin: 6px 0 0 0;
  padding: 8px 10px;
  background: var(--bg-secondary);
  border-radius: var(--radius-sm);
  color: var(--text-secondary);
  font-family: 'SF Mono', Consolas, 'Courier New', monospace;
  font-size: var(--font-xs);
  white-space: pre-wrap;
  word-break: break-word;
  max-height: 160px;
  overflow-y: auto;
  user-select: text;
  -webkit-user-select: text;
}

/* 关闭按钮 */
.ui-toast__close {
  background: transparent;
  border: none;
  cursor: pointer;
  padding: 2px;
  border-radius: var(--radius-sm);
  color: var(--text-muted);
  display: flex;
  align-items: center;
  justify-content: center;
  transition: background var(--transition-fast), color var(--transition-fast);
  flex-shrink: 0;
}
.ui-toast__close:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}
</style>