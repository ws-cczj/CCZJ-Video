<script setup lang="ts">
import { computed } from 'vue'
import { useErrorStore, type ErrorItem } from '../stores/error'
import { MotionList, Toast } from './ui'

const errorStore = useErrorStore()

const visible = computed<ErrorItem[]>(() => errorStore.visibleToasts)

function formatTime(ts: number): string {
  const d = new Date(ts)
  const pad = (n: number) => n.toString().padStart(2, '0')
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

function dismiss(id: string): void {
  errorStore.dismiss(id)
}
</script>

<template>
  <div class="toast-stack" aria-live="polite">
    <MotionList preset="toast" tag="div" class="toast-stack__list">
      <Toast
        v-for="t in visible"
        :key="t.id"
        :level="t.level as any"
        :title="t.title"
        :time="`${formatTime(t.time)}${(t.count || 1)>1?` ×${t.count}`:''}`"
        :detail="[t.requestId && `ID: ${t.requestId}`, t.detail].filter(Boolean).join('\n')"
        :action-label="t.actionLabel"
        @close="dismiss(t.id)"
        @action="dismiss(t.id); t.action?.()"
      >
        <div v-if="t.message" class="toast-message">{{ t.message }}</div>
      </Toast>
    </MotionList>
  </div>
</template>

<style scoped>
.toast-stack {
  @apply cczj-fixed;
  top: 48px;
  right: 24px;
  z-index: var(--z-toast);
  @apply cczj-pointer-events-none;
}

.toast-stack__list {
  @apply cczj-flex cczj-flex-col cczj-items-end cczj-gap-6;
}

.toast-message {
  font-size: var(--font-sm);
  @apply cczj-text-secondary cczj-leading-normal;
  word-break: break-word;
}

@media (max-width: 640px) {
  .toast-stack { top: calc(env(safe-area-inset-top, 0px) + 12px); right: 12px; left: 12px; }
}

</style>
