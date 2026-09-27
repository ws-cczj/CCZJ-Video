<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted } from 'vue'
import { useConfirmStore } from '../stores/confirm'
import { tr } from '../locales'
import { Modal, Button, Tag } from './ui'

const store = useConfirmStore()

const current = computed(() => store.active)

function ok(): void { store.handle(true) }
function cancel(): void { store.handle(false) }

function levelLabel(): string {
  if (!current.value) return ''
  if (current.value.level === 'danger') return tr('confirm.danger')
  if (current.value.level === 'warn') return tr('confirm.warn')
  return tr('confirm.info')
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
    :ok-text="current?.okText || tr('common.confirm')"
    :cancel-text="current?.cancelText || tr('common.cancel')"
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