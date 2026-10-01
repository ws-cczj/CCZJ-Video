<script setup lang="ts">
import { ref, watch } from 'vue'
import type { ComponentPublicInstance } from 'vue'
import { tr } from '../../locales'
import { ImportSourceFromBase64 } from '../../api/app'
import { useSourceStore } from '../../stores/source'
import Icon from '../../components/Icon.vue'
import { Button, Modal } from '../../components/ui'

/**
 * 导入采集源弹窗：拖拽 / 选择 json、json.gz、json.br，读成 base64 交后端。
 *
 * 打开与关闭仍由页面决定（modelValue），文件状态留在这里；导入成功后仍走
 * 页面同款 sourceStore.loadSources(true)，顺序与拆分前一致。
 */
const props = defineProps<{ modelValue: boolean }>()

const emit = defineEmits<{ 'update:modelValue': [value: boolean] }>()

const sourceStore = useSourceStore()

const importDragging = ref(false)
const importFile = ref<File | null>(null)
const importLoading = ref(false)
const importFileInput = ref<HTMLInputElement | null>(null)

/** 模板 ref 用函数式：字符串 ref 在多实例场景会互相覆盖。 */
function bindFileInput(el: Element | ComponentPublicInstance | null): void {
  importFileInput.value = el as HTMLInputElement | null
}

watch(() => props.modelValue, (open) => {
  if (open) {
    importDragging.value = false
    importFile.value = null
    importLoading.value = false
  } else {
    importFile.value = null
  }
})

function closeImportDialog(): void {
  emit('update:modelValue', false)
}

function onImportDragOver(e: DragEvent): void {
  e.preventDefault()
  importDragging.value = true
}

function onImportDragLeave(): void {
  importDragging.value = false
}

function onImportDrop(e: DragEvent): void {
  e.preventDefault()
  importDragging.value = false
  const file = e.dataTransfer?.files?.[0]
  if (!file) return
  const name = file.name.toLowerCase()
  if (!name.endsWith('.json') && !name.endsWith('.json.gz') && !name.endsWith('.json.br') && !file.type.includes('json')) return
  importFile.value = file
}

function onImportFileClick(): void {
  importFileInput.value?.click()
}

function onImportFileSelected(e: Event): void {
  const file = (e.target as HTMLInputElement).files?.[0]
  if (!file) return
  importFile.value = file
}

async function doImportSource(): Promise<void> {
  if (!importFile.value) return
  importLoading.value = true
  const file = importFile.value
  const reader = new FileReader()
  reader.onload = async () => {
    try {
      const b64 = (reader.result as string).split(',')[1]
      await ImportSourceFromBase64(file.name, b64)
      await sourceStore.loadSources(true)
      closeImportDialog()
    } catch (e) {
      console.error('import failed', e)
      importLoading.value = false
    }
  }
  reader.readAsDataURL(file)
}
</script>

<template>
  <!-- 导入弹窗 -->
  <Modal
    :model-value="props.modelValue"
    :title="tr('sources.importTitle')"
    width="560px"
    :show-footer="true"
    @update:model-value="(v: boolean) => !v && closeImportDialog()"
  >
    <p class="modal-desc">{{ tr('sources.importDesc') }}</p>
    <div
      class="import-drop-zone"
      :class="{ dragging: importDragging, filled: !!importFile }"
      @dragover="onImportDragOver"
      @dragleave="onImportDragLeave"
      @drop="onImportDrop"
      @click="onImportFileClick"
    >
      <input type="file" accept=".json,.json.gz,.json.br" :ref="bindFileInput" style="display:none" @change="onImportFileSelected" />
      <template v-if="importFile">
        <Icon name="database" :size="28" />
        <p class="import-file-name">{{ importFile.name }}</p>
        <small>{{ (importFile.size / 1024).toFixed(1) }} KB · {{ tr('sources.clickToReselect') }}</small>
      </template>
      <template v-else>
        <Icon name="upload" :size="32" />
        <p class="import-hint-main">{{ tr('sources.dropHint') }}</p>
        <p class="import-hint-sub">{{ tr('sources.orClickToChoose') }}</p>
      </template>
    </div>
    <template #footer>
      <Button variant="secondary" size="md" @click="closeImportDialog">{{ tr('common.cancel') }}</Button>
      <Button variant="primary" size="md" :disabled="!importFile || importLoading" :loading="importLoading" @click="doImportSource">{{ tr('common.import') }}</Button>
    </template>
  </Modal>
</template>

<style scoped>

/* 同 SourceFormModal：Modal.vue 会 Teleport 到 body，父作用域选不到，样式自带一份。 */
.modal-desc { font-size: 13px; color: var(--text-muted); margin: 0 0 18px; }

/* === 导入弹窗 === */
.import-drop-zone {
  border: 2px dashed var(--border);
  border-radius: 14px;
  padding: 40px 20px;
  text-align: center;
  cursor: pointer;
  transition: all 0.2s ease;
  color: var(--text-muted);
  background: rgba(255,255,255,0.02);
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
}
.import-drop-zone:hover {
  border-color: var(--accent);
  background: var(--accent-alpha-10);
  color: var(--text-primary);
}
.import-drop-zone.dragging {
  border-color: var(--accent);
  background: var(--accent-alpha-15);
  transform: scale(1.02);
}
.import-drop-zone.filled {
  border-style: solid;
  border-color: var(--accent);
  color: var(--text-primary);
}
.import-hint-main {
  margin: 0;
  font-size: 15px;
  font-weight: 500;
  color: var(--text-primary);
}
.import-hint-sub {
  margin: 0;
  font-size: 12px;
  color: var(--text-muted);
}
.import-file-name {
  margin: 4px 0 0;
  font-size: 14px;
  font-weight: 600;
  color: var(--accent);
}

</style>
