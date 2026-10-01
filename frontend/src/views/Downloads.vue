<script setup lang="ts">
defineOptions({ name: 'Downloads' })
import { computed, onMounted, ref } from 'vue'
import { tr } from '../locales'
import { useDownloadStore, formatBytes, formatSpeed, formatEta, percent as pct, type ChunkProgress } from '../stores/download'
import { GetSetting, SetSetting, GetDownloadDir } from '../api/app'
import Icon from '../components/Icon.vue'
import { Button, Tag } from '../components/ui'
import { useErrorStore } from '../stores/error'
import { useConfirmStore } from '../stores/confirm'

const dl = useDownloadStore()
const errorStore = useErrorStore()
const confirmStore = useConfirmStore()
const filter = ref<'all' | 'active' | 'paused' | 'done' | 'error'>('all')

// ========== 下载目录设置 ==========
const downloadDirInput = ref<string>('')
const savingDownloadDir = ref(false)

async function applyDownloadDir(): Promise<void> {
  const val = downloadDirInput.value.trim()
  savingDownloadDir.value = true
  try {
    await dl.setDir(val)
    downloadDirInput.value = dl.dir
  } catch (e: any) {
    errorStore.fromError(tr('downloads.saveDirFailed'), e, 'Downloads.applyDownloadDir')
  } finally {
    savingDownloadDir.value = false
  }
}

async function resetDownloadDir(): Promise<void> {
  savingDownloadDir.value = true
  try {
    // 1. 先清空自定义目录
    downloadDirInput.value = ''
    await dl.setDir('')
    // 2. 获取操作系统默认目录
    const defaultDir = await GetDownloadDir()
    downloadDirInput.value = defaultDir || ''
    // 3. 同步 UI 显示
    downloadDirInput.value = dl.dir || downloadDirInput.value
  } catch (e: any) {
    errorStore.fromError(tr('downloads.defaultDirFailed'), e, 'Downloads.resetDownloadDir')
    downloadDirInput.value = ''
  } finally {
    savingDownloadDir.value = false
  }
}

onMounted(async () => {
  await dl.init()
  downloadDirInput.value = dl.dir || ''

  // 从后端载入设置
  try {
    const saved = await GetSetting('download_dir')
    if (saved) downloadDirInput.value = saved
  } catch { /* 忽略 */ }

  // 如果仍然为空，直接从后端获取默认下载目录
  if (!downloadDirInput.value) {
    try {
      const defaultDir = await GetDownloadDir()
      if (defaultDir) downloadDirInput.value = defaultDir
    } catch { /* 忽略 */ }
  }
})

// ========== 分块进度可视化 ==========
function chunkPercent(chunk: ChunkProgress): number {
  const total = chunk.end - chunk.start + 1
  if (total <= 0) return 0
  return Math.min(100, Math.round((chunk.done / total) * 100))
}

function chunkWidth(chunk: ChunkProgress, fileTotal: number): number {
  if (fileTotal <= 0) return 0
  return Math.max(0.5, ((chunk.end - chunk.start + 1) / fileTotal) * 100)
}

function chunkLeft(chunk: ChunkProgress, fileTotal: number): number {
  if (fileTotal <= 0) return 0
  return (chunk.start / fileTotal) * 100
}

function chunkColor(chunk: ChunkProgress): string {
  const pct = chunkPercent(chunk)
  if (pct >= 100) return '#10b981'
  if (pct >= 70) return '#22c55e'
  if (pct >= 30) return '#1890ff'
  return '#60a5fa'
}

function statusLabel(s: string): string {
  switch (s) {
    case 'queued': return tr('downloads.statusQueued')
    case 'downloading': return tr('downloads.statusDownloading')
    case 'paused': return tr('downloads.paused')
    case 'done': return tr('downloads.done')
    case 'error': return tr('downloads.error')
    case 'cancelled': return tr('downloads.statusCancelled')
    default: return s
  }
}
function statusClass(s: string): string {
  switch (s) {
    case 'queued': return 'status-queued'
    case 'downloading': return 'status-downloading'
    case 'paused': return 'status-paused'
    case 'done': return 'status-done'
    case 'error': return 'status-error'
    case 'cancelled': return 'status-cancelled'
    default: return ''
  }
}

// 统计数据
const stats = computed(() => ({
  total: dl.tasks.length,
  active: dl.tasks.filter((t) => t.status === 'downloading' || t.status === 'queued').length,
  paused: dl.tasks.filter((t) => t.status === 'paused').length,
  done: dl.tasks.filter((t) => t.status === 'done').length,
  error: dl.tasks.filter((t) => t.status === 'error' || t.status === 'cancelled').length,
}))

// 批量操作
async function pauseAllActive(): Promise<void> {
  const ids = dl.tasks.filter((t) => t.status === 'downloading' || t.status === 'queued').map((t) => t.task_id)
  for (const id of ids) await dl.pause(id)
}
async function resumeAllPaused(): Promise<void> {
  const ids = dl.tasks.filter((t) => t.status === 'paused').map((t) => t.task_id)
  for (const id of ids) await dl.resume(id)
}
async function cancelAllActive(): Promise<void> {
  const ids = dl.tasks.filter((t) => t.status === 'downloading' || t.status === 'queued' || t.status === 'paused').map((t) => t.task_id)
  const yes = await confirmStore.confirm({
    title: tr('downloads.cancelAllTitle'),
    message: tr('downloads.confirmCancelAll', { count: ids.length }),
    okText: tr('downloads.cancelAll'),
    level: 'warn',
  })
  if (!yes) return
  for (const id of ids) await dl.cancel(id)
}
async function removeAllCompleted(): Promise<void> {
  const ids = dl.tasks.filter((t) => t.status === 'done').map((t) => t.task_id)
  if (ids.length === 0) return
  const yes = await confirmStore.confirm({
    title: tr('downloads.removeCompletedTitle'),
    message: tr('downloads.confirmRemoveCompleted', { count: ids.length }),
    okText: tr('common.remove'),
    level: 'warn',
  })
  if (!yes) return
  for (const id of ids) await dl.remove(id)
}

// 过滤后的任务列表
function isMatchFilter(t: { status: string }): boolean {
  switch (filter.value) {
    case 'all': return true
    case 'active': return t.status === 'downloading' || t.status === 'queued'
    case 'paused': return t.status === 'paused'
    case 'done': return t.status === 'done'
    case 'error': return t.status === 'error' || t.status === 'cancelled'
    default: return true
  }
}
</script>

<template>
  <div class="downloads-page">
    <header class="page-header cczj-flex cczj-flex-col cczj-gap-4">
      <div class="header-top cczj-flex cczj-justify-between cczj-items-start">
        <div>
          <h1 class="cczj-text-xl cczj-font-semibold">{{ tr('downloads.title') }}</h1>
          <p class="subtitle cczj-text-sm cczj-text-muted cczj-mt-1" v-if="dl.dir">{{ tr('downloads.defaultDirLabel') }}{{ dl.dir }}</p>
        </div>
        <!-- 批量操作按钮 -->
        <div v-if="dl.tasks.length > 0" class="bulk-actions cczj-flex cczj-gap-2 cczj-items-center cczj-flex-wrap">
          <button
            v-if="stats.active > 0"
            class="b-btn b-btn-pause cczj-cursor-pointer cczj-rounded cczj-transition cczj-flex cczj-items-center cczj-gap-1"
            @click="pauseAllActive"
          >
            <Icon name="pause" :size="11" /><span>{{ tr('downloads.pauseAll') }} ({{ stats.active }})</span>
          </button>
          <button
            v-if="stats.paused > 0"
            class="b-btn b-btn-resume cczj-cursor-pointer cczj-rounded cczj-transition cczj-flex cczj-items-center cczj-gap-1"
            @click="resumeAllPaused"
          >
            <Icon name="play" :size="11" /><span>{{ tr('downloads.resumeAll') }} ({{ stats.paused }})</span>
          </button>
          <button
            v-if="stats.active + stats.paused > 0"
            class="b-btn b-btn-danger cczj-cursor-pointer cczj-rounded cczj-transition cczj-flex cczj-items-center cczj-gap-1"
            @click="cancelAllActive"
          >
            <Icon name="close" :size="11" /><span>{{ tr('downloads.cancelAll') }}</span>
          </button>
          <button
            v-if="stats.done > 0"
            class="b-btn b-btn-remove cczj-cursor-pointer cczj-rounded cczj-transition cczj-flex cczj-items-center cczj-gap-1"
            @click="removeAllCompleted"
          >
            <Icon name="trash" :size="11" /><span>{{ tr('downloads.removeAllCompleted') }} ({{ stats.done }})</span>
          </button>
        </div>
      </div>

      <div class="filter-row cczj-flex cczj-gap-2 cczj-flex-wrap">
        <Tag :active="filter === 'all'" @click="filter = 'all'">{{ tr('downloads.all') }} ({{ stats.total }})</Tag>
        <Tag :active="filter === 'active'" @click="filter = 'active'">{{ tr('downloads.statusDownloading') }} ({{ stats.active }})</Tag>
        <Tag :active="filter === 'paused'" @click="filter = 'paused'">{{ tr('downloads.paused') }} ({{ stats.paused }})</Tag>
        <Tag :active="filter === 'done'" @click="filter = 'done'">{{ tr('downloads.done') }} ({{ stats.done }})</Tag>
        <Tag :active="filter === 'error'" @click="filter = 'error'">{{ tr('downloads.errorCancelled') }} ({{ stats.error }})</Tag>
      </div>
    </header>

    <!-- ========== 下载设置 ========== -->
    <section class="download-settings-block cczj-rounded cczj-border cczj-p-4 cczj-bg-card">
      <h3 class="cczj-text-lg cczj-font-semibold cczj-mb-3">{{ tr('downloads.directory') }}</h3>
      <div class="setting-row cczj-flex cczj-gap-3 cczj-items-center cczj-flex-wrap">
        <input
          type="text"
          class="setting-input cczj-flex-1 cczj-min-w-48 cczj-rounded cczj-border cczj-bg-secondary cczj-p-2"
          v-model="downloadDirInput"
          :placeholder="tr('downloads.dirPlaceholder')"
          @keyup.enter="applyDownloadDir"
        />
        <Button variant="primary" size="md" :disabled="savingDownloadDir" :loading="savingDownloadDir" @click="applyDownloadDir">
          {{ tr('common.save') }}
        </Button>
        <Button variant="secondary" size="md" :disabled="savingDownloadDir" @click="resetDownloadDir">
          {{ tr('downloads.useDefault') }}
        </Button>
      </div>
      <p class="hint cczj-text-xs cczj-text-muted cczj-mt-2">{{ tr('downloads.currentDirLabel') }}{{ dl.dir || tr('downloads.dirUnset') }}</p>
      <p class="hint cczj-text-xs cczj-text-muted cczj-mt-1">{{ tr('downloads.hint') }}</p>
    </section>

    <div v-if="dl.tasks.length === 0" class="empty cczj-text-center cczj-py-12">
      <div class="empty-icon cczj-text-4xl cczj-mb-3">⬇️</div>
      <div class="empty-title cczj-text-lg cczj-font-semibold cczj-mb-2">{{ tr('downloads.emptyTitle') }}</div>
      <div class="empty-desc cczj-text-sm cczj-text-muted">{{ tr('downloads.emptyDesc') }}</div>
    </div>

    <div v-else class="task-list cczj-flex cczj-flex-col cczj-gap-3">
      <div
        v-for="task in dl.tasks.filter(isMatchFilter)"
        :key="task.task_id"
        class="task-card cczj-rounded cczj-border cczj-p-4 cczj-bg-card"
      >
        <div class="task-head cczj-flex cczj-justify-between cczj-items-start cczj-gap-3">
          <div class="task-title cczj-truncate cczj-flex cczj-items-center cczj-gap-2 cczj-flex-1" :title="task.filename">
            <Icon name="download" :size="14" class="cczj-flex-shrink-0" />
            <span>{{ task.vod_name || task.filename }}</span>
            <span v-if="task.ep_name" class="task-ep cczj-text-muted">- {{ task.ep_name }}</span>
          </div>
          <span :class="['status-chip', statusClass(task.status)]" class="cczj-rounded cczj-px-2 cczj-py-1 cczj-text-xs cczj-font-medium cczj-flex-shrink-0">{{ statusLabel(task.status) }}</span>
        </div>

        <div class="task-meta-row cczj-flex cczj-gap-3 cczj-items-center cczj-text-sm cczj-text-muted cczj-mt-2 cczj-flex-wrap">
          <span class="muted">{{ formatBytes(task.downloaded) }} / {{ task.total > 0 ? formatBytes(task.total) : tr('downloads.unknownSize') }}</span>
          <span v-if="task.status === 'paused'" class="speed status-paused-label">{{ tr('downloads.paused') }}</span>
          <span v-if="task.status === 'downloading'" class="speed">{{ formatSpeed(task.speed_bps) }}</span>
          <span v-if="task.status === 'downloading'" class="eta">{{ tr('downloads.remaining') }} {{ formatEta(task.eta_sec) }}</span>
          <span v-if="task.status === 'queued'" class="muted">{{ tr('downloads.waiting') }}</span>
          <span v-if="task.error" class="err-msg cczj-truncate cczj-max-w-72" :title="task.error">{{ task.error }}</span>
        </div>

        <!-- IDM 风格分块进度可视化 -->
        <div v-if="task.chunks && task.chunks.length > 0 && task.total > 0 && task.status === 'downloading'" class="chunk-progress-container cczj-relative cczj-h-2 cczj-rounded cczj-bg-border cczj-overflow-hidden cczj-mt-2">
          <div
            v-for="chunk in task.chunks"
            :key="chunk.id"
            class="chunk-bar cczj-absolute cczj-top-0 cczj-h-full"
            :style="{
              left: chunkLeft(chunk, task.total) + '%',
              width: chunkWidth(chunk, task.total) + '%',
              background: chunkColor(chunk),
            }"
            :title="tr('downloads.chunkTooltip', { index: chunk.id + 1, percent: chunkPercent(chunk), done: formatBytes(chunk.done), total: formatBytes(chunk.end - chunk.start + 1) })"
          >
            <span class="chunk-label cczj-absolute cczj-inset-0 cczj-flex cczj-items-center cczj-justify-center cczj-text-white cczj-text-xs cczj-font-bold" v-if="chunkWidth(chunk, task.total) > 8">
              {{ chunk.id + 1 }}
            </span>
          </div>
        </div>

        <div class="progress-track cczj-relative cczj-h-2 cczj-rounded cczj-bg-border cczj-overflow-hidden cczj-mt-2">
          <div class="progress-fill cczj-absolute cczj-top-0 cczj-left-0 cczj-h-full cczj-bg-accent cczj-transition-all" :style="{ width: pct(task) + '%' }"></div>
          <span class="progress-text cczj-absolute cczj-inset-0 cczj-flex cczj-items-center cczj-justify-center cczj-text-xs cczj-font-bold cczj-text-white">{{ pct(task) }}%</span>
        </div>

        <div class="task-footer cczj-flex cczj-justify-between cczj-items-center cczj-gap-3 cczj-mt-3">
          <span class="file-path cczj-truncate cczj-flex-1 cczj-text-xs cczj-text-muted" :title="task.save_path">{{ task.save_path }}</span>
          <div class="task-actions cczj-flex cczj-gap-2 cczj-flex-shrink-0">
            <button
              v-if="task.status === 'downloading' || task.status === 'queued'"
              class="t-btn t-btn-pause cczj-cursor-pointer cczj-rounded cczj-transition cczj-flex cczj-items-center cczj-gap-1 cczj-px-2 cczj-py-1 cczj-text-xs"
              @click="dl.pause(task.task_id)"
            >
              <Icon name="pause" :size="12" /><span>{{ tr('downloads.pause') }}</span>
            </button>
            <button
              v-if="task.status === 'paused' || task.status === 'error'"
              class="t-btn t-btn-resume cczj-cursor-pointer cczj-rounded cczj-transition cczj-flex cczj-items-center cczj-gap-1 cczj-px-2 cczj-py-1 cczj-text-xs"
              @click="dl.resume(task.task_id)"
            >
              <Icon name="play" :size="12" /><span>{{ task.status === 'error' ? tr('common.retry') : tr('downloads.resume') }}</span>
            </button>
            <button
              v-if="task.status === 'downloading' || task.status === 'queued' || task.status === 'paused'"
              class="t-btn t-btn-danger cczj-cursor-pointer cczj-rounded cczj-transition cczj-flex cczj-items-center cczj-gap-1 cczj-px-2 cczj-py-1 cczj-text-xs"
              @click="dl.cancel(task.task_id)"
            >
              <Icon name="close" :size="12" /><span>{{ tr('common.cancel') }}</span>
            </button>
            <button
              v-if="task.status === 'done'"
              class="t-btn t-btn-open cczj-cursor-pointer cczj-rounded cczj-transition cczj-flex cczj-items-center cczj-gap-1 cczj-px-2 cczj-py-1 cczj-text-xs"
              @click="dl.openFile(task.save_path)"
            >
              <Icon name="play" :size="12" /><span>{{ tr('downloads.open') }}</span>
            </button>
            <button class="t-btn t-btn-remove cczj-cursor-pointer cczj-rounded cczj-transition cczj-flex cczj-items-center cczj-gap-1 cczj-px-2 cczj-py-1 cczj-text-xs" @click="dl.remove(task.task_id)">
              <Icon name="trash" :size="12" /><span>{{ tr('common.remove') }}</span>
            </button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped src="../styles/views/downloads.css"></style>
