<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from './Icon.vue'
import { Button } from './ui'
import {
  BackupArchives,
  BackupExport,
  BackupExports,
  BackupImportFile,
  BackupImportFromBase64,
  BackupRestoreArchive,
  OpenFolder,
  RestartApp,
} from '../api/app'
import { useErrorStore } from '../stores/error'
import { useConfirmStore } from '../stores/confirm'

const { t } = useI18n()
const errorStore = useErrorStore()
const confirmStore = useConfirmStore()

// 后端返回的是逐类计数，界面按数字讲结果：绑定里的模型类型不直接 import，
// 与同页其他面板保持一致用宽松类型接。
const busy = ref('')
const lastResult = ref<any>(null)
const exportPath = ref('')
const archives = ref<any[]>([])
const archivesLoading = ref(false)
const files = ref<any[]>([])
const filesLoading = ref(false)
const fileInput = ref<HTMLInputElement | null>(null)

function fmtBytes(value: number): string {
  const bytes = Number(value || 0)
  if (bytes < 1024) return `${bytes} B`
  const units = ['KB', 'MB', 'GB']
  let size = bytes / 1024
  let unit = 0
  while (size >= 1024 && unit < units.length - 1) {
    size /= 1024
    unit++
  }
  return `${size.toFixed(1)} ${units[unit]}`
}

// 导出不再走原生保存对话框：WebView2 里 Dialogs.SaveFile 点了既不弹窗口也不报错，
// 静默失败比少一个「另存为」难查得多。文件一律落在数据目录的 exports 下，
// 界面上给路径和「打开所在文件夹」，要挪去别处由文件管理器负责。
async function doExport(): Promise<void> {
  busy.value = 'export'
  try {
    exportPath.value = await BackupExport('') as string
    errorStore.info(t('backup.exportOkTitle'), t('backup.exportOk', { path: exportPath.value }))
    await loadFiles()
  } catch (e: any) {
    errorStore.fromError(t('backup.exportFailed'), e, 'DataBackupPanel')
  } finally {
    busy.value = ''
  }
}

async function openExportFolder(): Promise<void> {
  if (!exportPath.value) return
  try {
    await OpenFolder(exportPath.value)
  } catch (e: any) {
    errorStore.fromError(t('backup.openFolderFailed'), e, 'DataBackupPanel')
  }
}

function pickFile(): void {
  fileInput.value?.click()
}

async function onFileSelected(event: Event): Promise<void> {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return
  const ok = await confirmStore.confirm({
    title: t('backup.import'),
    message: t('backup.importConfirm', { name: file.name }),
  })
  if (!ok) return
  busy.value = 'import'
  try {
    const dataUrl = await readAsDataURL(file)
    const result = await BackupImportFromBase64(file.name, dataUrl.split(',')[1] || '')
    lastResult.value = result
  } catch (e: any) {
    errorStore.fromError(t('backup.importFailed'), e, 'DataBackupPanel')
  } finally {
    busy.value = ''
  }
}

function readAsDataURL(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(String(reader.result || ''))
    reader.onerror = () => reject(reader.error || new Error('read failed'))
    reader.readAsDataURL(file)
  })
}

async function loadArchives(): Promise<void> {
  archivesLoading.value = true
  try {
    archives.value = (await BackupArchives()) as any[]
  } catch (e: any) {
    archives.value = []
    errorStore.fromError(t('backup.archivesFailed'), e, 'DataBackupPanel')
  } finally {
    archivesLoading.value = false
  }
}

async function restoreArchive(archive: any): Promise<void> {
  const ok = await confirmStore.confirm({
    title: t('backup.archiveRestore'),
    message: t('backup.archiveRestoreConfirm', { name: archive.name }),
  })
  if (!ok) return
  busy.value = `archive:${archive.name}`
  try {
    lastResult.value = await BackupRestoreArchive(archive.name)
  } catch (e: any) {
    errorStore.fromError(t('backup.archiveRestoreFailed'), e, 'DataBackupPanel')
  } finally {
    busy.value = ''
  }
}

async function restartApp(): Promise<void> {
  try {
    await RestartApp()
  } catch (e: any) {
    errorStore.fromError(t('backup.restartFailed'), e, 'DataBackupPanel')
  }
}

async function loadFiles(): Promise<void> {
  filesLoading.value = true
  try {
    files.value = (await BackupExports()) as any[]
  } catch (e: any) {
    files.value = []
    errorStore.fromError(t('backup.filesFailed'), e, 'DataBackupPanel')
  } finally {
    filesLoading.value = false
  }
}

async function importFile(item: any): Promise<void> {
  const ok = await confirmStore.confirm({
    title: t('backup.import'),
    message: t('backup.importConfirm', { name: item.name }),
  })
  if (!ok) return
  busy.value = `file:${item.name}`
  try {
    lastResult.value = await BackupImportFile(item.name)
  } catch (e: any) {
    errorStore.fromError(t('backup.importFailed'), e, 'DataBackupPanel')
  } finally {
    busy.value = ''
  }
}

// 归档与备份文件列表只在进过一次本页才读，切走不保留：文件名带时间戳，
// 常驻反而会把旧列表当成现状。
onMounted(() => {
  void loadArchives()
  void loadFiles()
})
</script>

<template>
  <section class="block">
    <div class="block-hd cczj-flex cczj-items-center cczj-justify-between">
      <h3>{{ t('backup.title') }}</h3>
      <Button variant="secondary" size="sm" :loading="busy === 'export'" @click="doExport">
        <Icon name="download" :size="12" /> {{ t('backup.export') }}
      </Button>
    </div>
    <p class="desc">{{ t('backup.intro') }}</p>
    <div class="cczj-flex cczj-items-center cczj-gap-4 cczj-flex-wrap">
      <Button variant="secondary" size="sm" :loading="busy === 'import'" @click="pickFile">
        <Icon name="upload" :size="12" /> {{ t('backup.import') }}
      </Button>
      <Button v-if="exportPath" variant="ghost" size="sm" @click="openExportFolder">
        <Icon name="folder" :size="12" /> {{ t('backup.openFolder') }}
      </Button>
    </div>
    <input ref="fileInput" class="hidden-input" type="file" accept=".json,.json.br,.json.gz,.br,.gz" @change="onFileSelected" />

    <div v-if="lastResult" class="result cczj-flex cczj-flex-col cczj-gap-3">
      <div class="result-title">{{ t('backup.importOk') }}</div>
      <ul class="result-list cczj-flex cczj-flex-col cczj-gap-2">
        <li>{{ t('backup.resultFavorites', { count: lastResult.favorites_added }) }}</li>
        <li>{{ t('backup.resultHistory', { count: lastResult.history_applied }) }}</li>
        <li>{{ t('backup.resultSettings', { count: lastResult.settings_applied }) }}</li>
        <li>{{ t('backup.resultSources', { count: lastResult.sources_added }) }}</li>
        <li v-if="lastResult.favorites_skipped || lastResult.history_skipped || lastResult.settings_skipped">
          {{ t('backup.resultSkipped', { count: lastResult.favorites_skipped + lastResult.history_skipped + lastResult.settings_skipped }) }}
        </li>
        <li v-if="lastResult.unresolved">{{ t('backup.resultUnresolved', { count: lastResult.unresolved }) }}</li>
      </ul>
      <p class="desc">{{ t('backup.restartHint') }}</p>
      <div class="cczj-flex cczj-items-center cczj-gap-4">
        <Button variant="secondary" size="sm" @click="restartApp">{{ t('backup.restart') }}</Button>
      </div>
    </div>
  </section>

  <section class="block">
    <div class="block-hd cczj-flex cczj-items-center cczj-justify-between">
      <h3>{{ t('backup.files') }}</h3>
      <Button variant="secondary" size="sm" :loading="filesLoading" @click="loadFiles">
        <Icon name="refresh" :size="12" /> {{ t('common.refresh') }}
      </Button>
    </div>
    <p class="desc">{{ t('backup.filesIntro') }}</p>
    <div v-if="filesLoading" class="desc">{{ t('backup.archivesLoading') }}</div>
    <div v-else-if="!files.length" class="desc">{{ t('backup.filesEmpty') }}</div>
    <ul v-else class="archive-list cczj-flex cczj-flex-col cczj-gap-3">
      <li v-for="item in files" :key="item.name" class="archive-row cczj-flex cczj-items-center cczj-gap-7">
        <div class="archive-info cczj-flex-1 cczj-min-w-0">
          <div class="archive-name">{{ item.name }}</div>
          <div class="archive-meta">{{ t('backup.archiveSize', { size: fmtBytes(item.size_bytes), modified: item.modified_at }) }}</div>
        </div>
        <Button
          variant="secondary"
          size="sm"
          :loading="busy === `file:${item.name}`"
          @click="importFile(item)"
        >
          {{ t('backup.import') }}
        </Button>
      </li>
    </ul>
  </section>

  <section class="block">
    <div class="block-hd cczj-flex cczj-items-center cczj-justify-between">
      <h3>{{ t('backup.archives') }}</h3>
      <Button variant="secondary" size="sm" :loading="archivesLoading" @click="loadArchives">
        <Icon name="refresh" :size="12" /> {{ t('common.refresh') }}
      </Button>
    </div>
    <p class="desc">{{ t('backup.archivesIntro') }}</p>
    <div v-if="archivesLoading" class="desc">{{ t('backup.archivesLoading') }}</div>
    <div v-else-if="!archives.length" class="desc">{{ t('backup.archivesEmpty') }}</div>
    <ul v-else class="archive-list cczj-flex cczj-flex-col cczj-gap-3">
      <li v-for="archive in archives" :key="archive.name" class="archive-row cczj-flex cczj-items-center cczj-gap-7">
        <div class="archive-info cczj-flex-1 cczj-min-w-0">
          <div class="archive-name">{{ archive.name }}</div>
          <div class="archive-meta">{{ t('backup.archiveSize', { size: fmtBytes(archive.size_bytes), modified: archive.modified_at }) }}</div>
        </div>
        <Button
          variant="danger"
          size="sm"
          :loading="busy === `archive:${archive.name}`"
          @click="restoreArchive(archive)"
        >
          {{ t('backup.archiveRestore') }}
        </Button>
      </li>
    </ul>
  </section>
</template>

<style scoped>
/* 卡片外框由 Settings 的 .panel.group-card 统一画（本组件是多根 fragment，父级只能用
   :deep 将外框压平），这里只留标题排版——h3 不是组件根节点，拿不到父级 scope id，
   Settings 里的 .block h3 落不到它身上。 */
.block h3 {
  font-size: 0.97rem;
  font-weight: 700;
  margin: 0 0 12px;
  letter-spacing: 0.3px;
}
.hidden-input {
  display: none;
}
.desc {
  margin: 6px 0 0;
  font-size: 12px;
  line-height: 1.6;
  color: var(--text-muted);
}
.result {
  margin-top: 10px;
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: 10px;
  background: var(--bg-card);
}
.result-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--text-primary);
}
.result-list {
  margin: 0;
  padding-left: 18px;
  font-size: 12px;
  color: var(--text-secondary);
}
.archive-list {
  margin: 10px 0 0;
  padding: 0;
  list-style: none;
}
.archive-row {
  padding: 8px 10px;
  border: 1px solid var(--border);
  border-radius: 10px;
  background: var(--bg-card);
}
.archive-name {
  font-size: 12px;
  color: var(--text-primary);
  word-break: break-all;
}
.archive-meta {
  font-size: 11px;
  color: var(--text-muted);
}
</style>
