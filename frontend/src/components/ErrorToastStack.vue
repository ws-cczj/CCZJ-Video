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
  <div v-if="visible.length > 0" class="toast-stack" aria-live="polite">
    <MotionList preset="toast" tag="div" class="toast-stack__list">
      <Toast
        v-for="t in visible"
        :key="t.id"
        :level="t.level as any"
        :title="t.title"
        :time="formatTime(t.time)"
        :detail="t.detail"
        @close="dismiss(t.id)"
      >
        <div v-if="t.message" class="toast-message">{{ t.message }}</div>
      </Toast>
    </MotionList>
  </div>
</template>

<style scoped>
.toast-stack {
  position: fixed;
  top: 48px;
  right: 24px;
  z-index: var(--z-toast);
  pointer-events: none;
}

.toast-stack__list {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 12px;
}

.toast-message {
  font-size: var(--font-sm);
  color: var(--text-secondary);
  line-height: 1.5;
  word-break: break-word;
}

</style>
