<script setup lang="ts">
import { onScopeDispose, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { diagnosticSavesToDownloads, saveDiagnosticExport, supportsDiagnosticSave } from '../platform/diagnosticFiles'
import { notifyExportReady } from '../diagnostics/exportFeedback'
import { useErrorStore } from '../stores/error'
import Icon from './Icon.vue'
import { Button } from './ui'

const props = defineProps<{ path: string }>()
const { t } = useI18n()
const errors = useErrorStore()
const saving = ref(false)
const buttonLabel = diagnosticSavesToDownloads() ? 'diagnostics.saveDownloads' : 'diagnostics.saveExport'
const lifetime = new AbortController()
onScopeDispose(() => lifetime.abort())

async function save(): Promise<void> {
  if (saving.value || !props.path) return
  saving.value = true
  const path = props.path
  try {
    const saved = await saveDiagnosticExport(path, lifetime.signal)
    if (saved) {
      notifyExportReady(t(saved.destination === 'downloads' ? 'diagnostics.saveDownloadsDone' : 'diagnostics.saveDone'), '', saved.open)
    }
  } catch (error) {
    if (!lifetime.signal.aborted) errors.error(t('diagnostics.saveFailed'), error instanceof Error ? error.message : '', '', 'Diagnostics.saveExport')
  } finally { saving.value = false }
}
</script>

<template>
  <Button v-if="path && supportsDiagnosticSave()" variant="secondary" size="sm" :loading="saving" @click="save">
    <Icon name="download" :size="14" /> {{ t(buttonLabel) }}
  </Button>
</template>
