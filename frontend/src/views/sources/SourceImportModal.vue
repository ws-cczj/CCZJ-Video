<script setup lang="ts">
import { ref, watch, onBeforeUnmount } from 'vue'
import type { ComponentPublicInstance } from 'vue'
import { tr } from '../../locales'
import { ImportSourceFromBase64 } from '../../api/app'
import { useSourceStore } from '../../stores/source'
import Icon from '../../components/Icon.vue'
import { Button, Modal } from '../../components/ui'
import { readFileDataURL } from '../../api/fileTransfer'
import { createRequestScope } from '../../api/requestScope'
import { normalizeApiError } from '../../api/errors'
import { useGeneration } from '../../composables/useGeneration'
import { useErrorStore } from '../../stores/error'

/**
 * 导入采集源弹窗：拖拽 / 选择 json、json.gz、json.br，读成 base64 交后端。
 *
 * 打开与关闭仍由页面决定（modelValue），文件状态留在这里；导入成功后仍走
 * 页面同款 sourceStore.loadSources(true)，顺序与拆分前一致。
 */
const props = defineProps<{ modelValue: boolean }>()

const emit = defineEmits<{ 'update:modelValue': [value: boolean] }>()

const sourceStore = useSourceStore()
const requests = createRequestScope()
const generation = useGeneration()
const errorStore = useErrorStore()
function releaseImport(): void { generation.invalidate(); requests.cancel(); importLoading.value = false }
onBeforeUnmount(releaseImport)

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
    releaseImport()
    importFile.value = null
  }
})

function closeImportDialog(): void {
  releaseImport()
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
  if (importLoading.value) return
  const file = e.dataTransfer?.files?.[0]
  if (!file) return
  const name = file.name.toLowerCase()
  if (!name.endsWith('.json') && !name.endsWith('.json.gz') && !name.endsWith('.json.br') && !file.type.includes('json')) return
  importFile.value = file
}

function onImportFileClick(): void {
  if (importLoading.value) return
  importFileInput.value?.click()
}

function onImportFileSelected(e: Event): void {
  if (importLoading.value) return
  const file = (e.target as HTMLInputElement).files?.[0]
  if (!file) return
  importFile.value = file
  ;(e.target as HTMLInputElement).value = ''
}

async function doImportSource(): Promise<void> {
  if (!importFile.value || importLoading.value) return
  importLoading.value = true
  const ticket = generation.begin()
  const file = importFile.value
  try {
    const data = await requests.run(() => readFileDataURL(file))
    if (!generation.isCurrent(ticket)) return
    await requests.run(() => ImportSourceFromBase64(file.name, data.split(',')[1] || ''))
    if (!generation.isCurrent(ticket)) return
    await sourceStore.loadSources(true)
    if (generation.isCurrent(ticket)) closeImportDialog()
  } catch (error) {
    const failure=normalizeApiError(error)
    if (generation.isCurrent(ticket) && failure.code !== 'CANCELLED') errorStore.fromError(tr('sources.importFailed'), failure.code==='SOURCE_URL_DUPLICATE' ? {...failure,message:tr('sources.duplicateApiUrl')} : error, 'SourceImportModal')
  } finally {
    if (generation.isCurrent(ticket)) importLoading.value = false
  }
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
      role="button"
      :tabindex="importLoading ? -1 : 0"
      :aria-label="tr('sources.dropHint')"
      :aria-disabled="importLoading"
      :class="{ dragging: importDragging, filled: !!importFile }"
      @dragover="onImportDragOver"
      @dragleave="onImportDragLeave"
      @drop="onImportDrop"
      @click="onImportFileClick"
      @keydown.enter.prevent="onImportFileClick"
      @keydown.space.prevent="onImportFileClick"
    >
      <input type="file" accept=".json,.json.gz,.json.br" :ref="bindFileInput" class="cczj-hidden" :disabled="importLoading" @change="onImportFileSelected" />
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
.modal-desc { @apply cczj-text-13 cczj-text-muted; margin: 0 0 18px; }

/* === 导入弹窗 === */
.import-drop-zone {
  border: 2px dashed var(--border);
  border-radius: 14px;
  padding: 40px 20px;
  @apply cczj-text-center cczj-cursor-pointer cczj-transition cczj-text-muted;
  background: rgba(255,255,255,0.02);
  @apply cczj-flex cczj-flex-col cczj-items-center cczj-justify-center cczj-gap-4;
}
.import-drop-zone:hover {
  @apply cczj-border-primary;
  background: var(--accent-alpha-10);
  @apply cczj-text-primary;
}
.import-drop-zone:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 2px;
}
.import-drop-zone.dragging {
  @apply cczj-border-primary;
  background: var(--accent-alpha-15);
  transform: scale(1.02);
}
.import-drop-zone.filled {
  border-style: solid;
  @apply cczj-border-primary cczj-text-primary;
}
.import-hint-main {
  margin: 0;
  font-size: calc(15rem / 14);
  @apply cczj-font-medium cczj-text-primary;
}
.import-hint-sub {
  margin: 0;
  @apply cczj-text-sm cczj-text-muted;
}
.import-file-name {
  margin: 4px 0 0;
  @apply cczj-text-base cczj-font-semibold cczj-text-accent;
}

</style>
