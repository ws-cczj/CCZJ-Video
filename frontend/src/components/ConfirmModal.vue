<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted } from 'vue'
import { useConfirmStore } from '../stores/confirm'
import { Modal, Button, Tag } from './ui'

const store = useConfirmStore()

const current = computed(() => store.active)

function ok(): void { store.handle(true) }
function cancel(): void { store.handle(false) }

function levelLabel(): string {
  if (!current.value) return ''
  if (current.value.level === 'danger') return '危险'
  if (current.value.level === 'warn') return '警告'
  return '提示'
}

function levelVariant(): 'primary' | 'danger' | 'warning' {
  if (!current.value) return 'primary'
  if (current.value.level === 'danger') return 'danger'
  if (current.value.level === 'warn') return 'primary'
  return 'primary'
}

function levelTagVariant(): 'default' | 'primary' | 'success' | 'warning' | 'danger' {
  if (!current.value) return 'primary'
  if (current.value.level === 'danger') return 'danger'
  if (current.value.level === 'warn') return 'warning'
  return 'primary'
}

function onKeydown(e: KeyboardEvent): void {
  if (!current.value) return
  if (e.key === 'Enter') {
    e.preventDefault()
    ok()
  } else if (e.key === 'Escape') {
    e.preventDefault()
    cancel()
  }
}

onMounted(() => window.addEventListener('keydown', onKeydown))
onBeforeUnmount(() => window.removeEventListener('keydown', onKeydown))
</script>

<template>
  <Modal
    :model-value="!!current"
    :title="current?.title"
    :show-footer="true"
    :ok-text="current?.okText || '确认'"
    :cancel-text="current?.cancelText || '取消'"
    :mask-closable="true"
    :closable="true"
    width="420px"
    @ok="ok"
    @cancel="cancel"
  >
    <div class="confirm-body">
      <Tag
        v-if="current?.level"
        :variant="levelTagVariant()"
        size="sm"
      >
        {{ levelLabel() }}
      </Tag>
      <p class="confirm-message">{{ current?.message }}</p>
    </div>
  </Modal>
</template>

<style scoped>
.confirm-body {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: var(--spacing-sm);
}
.confirm-message {
  margin: 0;
  font-size: var(--font);
  color: var(--text-secondary);
  line-height: 1.6;
}
</style>