<script setup lang="ts">
defineOptions({ name: 'FavFolderModal' })
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from './Icon.vue'
import { Modal, Button } from './ui'
import { readStorage } from '../platform/storage'

export interface FavFolder {
  id: string
  name: string
  default: boolean
}

const props = withDefaults(
  defineProps<{
    teleportTo?: string | HTMLElement
    modelValue: boolean
    initialFolderId?: string
  }>(),
  {
    initialFolderId: 'default'
  }
)

const emit = defineEmits<{
  (e: 'update:modelValue', value: boolean): void
  (e: 'confirm', folderId: string): void
}>()

const { t } = useI18n()
const selectedFolderId = ref(props.initialFolderId)
const favFolders = ref<FavFolder[]>([
  { id: 'default', name: t('detail.defaultFolder'), default: true }
])

function loadFolders(): void {
  try {
    const raw = readStorage<unknown>('cczj_fav_folders', null)
    if (raw) {
      const parsed = (typeof raw === 'string' ? JSON.parse(raw) : raw) as FavFolder[]
      if (Array.isArray(parsed) && parsed.length > 0) {
        favFolders.value = parsed
        return
      }
    }
  } catch {
    /* ignore parse error */
  }
  favFolders.value = [{ id: 'default', name: t('detail.defaultFolder'), default: true }]
}

watch(
  () => props.modelValue,
  (open) => {
    if (open) {
      loadFolders()
      selectedFolderId.value = props.initialFolderId || 'default'
    }
  },
  { immediate: true }
)

function onCancel(): void {
  emit('update:modelValue', false)
}

function onConfirm(): void {
  emit('confirm', selectedFolderId.value)
  emit('update:modelValue', false)
}
</script>

<template>
  <Modal
    :model-value="modelValue"
    :teleport-to="teleportTo"
    :title="t('detail.favToFolder')"
    width="420px"
    :show-footer="true"
    @update:model-value="(v: boolean) => emit('update:modelValue', v)"
  >
    <div class="folder-select-list cczj-flex cczj-flex-col cczj-gap-2">
      <label
        v-for="folder in favFolders"
        :key="folder.id"
        class="folder-select-item cczj-flex cczj-items-center cczj-gap-2 cczj-p-3 cczj-rounded cczj-transition cczj-cursor-pointer"
        :class="{ active: selectedFolderId === folder.id }"
      >
        <input
          type="radio"
          v-model="selectedFolderId"
          :value="folder.id"
          class="cczj-hidden"
        />
        <span class="folder-radio cczj-rounded-full cczj-border cczj-flex cczj-items-center cczj-justify-center" />
        <Icon :name="folder.default ? 'star' : 'list'" :size="14" />
        <span class="folder-name cczj-flex-1 cczj-truncate">
          {{ folder.default ? t('detail.defaultFolder') : folder.name }}
        </span>
      </label>
    </div>
    <template #footer>
      <Button variant="secondary" size="md" @click="onCancel">{{ t('common.cancel') }}</Button>
      <Button variant="primary" size="md" @click="onConfirm">{{ t('detail.confirmFav') }}</Button>
    </template>
  </Modal>
</template>

<style scoped>
.folder-select-item {
  background: var(--cczj-bg-secondary);
  border: 1px solid var(--cczj-border);
  color: var(--cczj-text);
  @apply cczj-text-base;
}
.folder-select-item:hover {
  background: var(--cczj-bg-tertiary);
  border-color: var(--cczj-border-hover);
}
.folder-select-item.active {
  border-color: var(--cczj-accent);
  background: var(--cczj-accent-alpha-10, rgba(var(--cczj-accent-rgb, 99, 102, 241), 0.1));
}
.folder-radio {
  @apply cczj-w-16 cczj-h-16;
  border-color: var(--cczj-border-hover);
  @apply cczj-relative cczj-flex-shrink-0;
}
.folder-select-item.active .folder-radio {
  border-color: var(--cczj-accent);
}
.folder-select-item.active .folder-radio::after {
  content: '';
  @apply cczj-w-2 cczj-h-2 cczj-rounded-50;
  background: var(--cczj-accent);
  @apply cczj-block;
}
</style>
