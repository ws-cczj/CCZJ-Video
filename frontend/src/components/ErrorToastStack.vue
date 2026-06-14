<script setup lang="ts">
import { computed } from 'vue'
import { useErrorStore, type ErrorItem } from '../stores/error'
import { Toast } from './ui'

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
    <transition-group name="toast" tag="div" class="toast-stack__list">
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
    </transition-group>
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

/* 过渡动画 */
.toast-enter-from {
  opacity: 0;
  transform: translateX(60px);
}
.toast-enter-active {
  transition: opacity 0.3s ease, transform 0.3s cubic-bezier(0.2, 0.8, 0.3, 1);
}
.toast-leave-to {
  opacity: 0;
  transform: translateX(60px);
}
.toast-leave-active {
  transition: opacity 0.25s ease, transform 0.25s ease;
}
.toast-move {
  transition: transform 0.3s ease;
}
</style>