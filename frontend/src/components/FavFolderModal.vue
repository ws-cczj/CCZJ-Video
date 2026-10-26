<script setup lang="ts">
defineOptions({ name: 'FavFolderModal' })
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from './Icon.vue'
import { Modal, Button } from './ui'
import { storeToRefs } from 'pinia'
import { useFavoriteFoldersStore } from '../stores/favoriteFolders'
import { useErrorStore } from '../stores/error'

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
    busy?: boolean
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
const folderStore=useFavoriteFoldersStore()
const {folders:favFolders,ready}=storeToRefs(folderStore)
const loading=ref(false)
const errors=useErrorStore()
let generation=0
async function loadFolders():Promise<void>{
 const token=++generation;loading.value=true
 try{await folderStore.load();if(token===generation&&!favFolders.value.some(row=>row.id===selectedFolderId.value))selectedFolderId.value='default'}
 catch(error){errors.fromError(t('favorites.loadFailed'),error,'FavoriteFolders')}
 finally{if(token===generation)loading.value=false}
}

watch(
  () => props.modelValue,
  (open) => {
    if (open) {
      void loadFolders()
      selectedFolderId.value = props.initialFolderId || 'default'
    }
  },
  { immediate: true }
)

function onCancel(): void {
  emit('update:modelValue', false)
}

function onConfirm(): void {
  if(props.busy||loading.value||!ready.value)return
  emit('confirm', selectedFolderId.value)
}
</script>

<template>
  <Modal
    :model-value="modelValue"
    :teleport-to="teleportTo"
    :title="t('detail.favToFolder')"
    width="420px"
    :show-footer="true"
    :closable="!busy" :mask-closable="!busy"
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
          :disabled="busy || loading"
          type="radio"
          v-model="selectedFolderId"
          :value="folder.id"
          class="folder-input"
        />
        <span aria-hidden="true" class="folder-radio cczj-rounded-50 cczj-border cczj-flex cczj-items-center cczj-justify-center" />
        <Icon :name="folder.default ? 'star' : 'list'" :size="14" />
        <span class="folder-name cczj-flex-1 cczj-truncate">
          {{ folder.default ? t('detail.defaultFolder') : folder.name }}
        </span>
      </label>
    </div>
    <template #footer>
      <Button variant="secondary" size="md" :disabled="busy" @click="onCancel">{{ t('common.cancel') }}</Button>
      <Button variant="primary" size="md" :loading="busy || loading" :disabled="!ready" @click="onConfirm">{{ t('detail.confirmFav') }}</Button>
    </template>
  </Modal>
</template>

<style scoped>
.folder-input { @apply cczj-absolute cczj-opacity-0; width:1px; height:1px; }
.folder-select-item { @apply cczj-relative; min-height:44px; }
.folder-select-item:has(input:focus-visible) { outline:2px solid var(--accent); outline-offset:2px; }
.folder-select-item {
  background: var(--bg-secondary);
  border: 1px solid var(--border);
  color: var(--text-primary);
  @apply cczj-text-base;
}
.folder-select-item:hover {
  background: var(--bg-card);
  border-color: var(--border-hover);
}
.folder-select-item.active {
  border-color: var(--accent);
  background: var(--accent-alpha-10);
}
.folder-radio {
  width:18px; height:18px;
  border-color: var(--border-hover);
  @apply cczj-relative cczj-flex-shrink-0;
}
.folder-select-item.active .folder-radio {
  border-color: var(--accent);
}
.folder-select-item.active .folder-radio::after {
  content: '';
  @apply cczj-w-2 cczj-h-2 cczj-rounded-50;
  background: var(--accent);
  @apply cczj-block;
}
</style>
