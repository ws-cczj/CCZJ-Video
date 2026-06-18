<script setup lang="ts">
defineOptions({ name: 'Downloads' })
import { computed, onMounted, ref } from 'vue'
import { useDownloadStore, formatBytes, formatSpeed, formatEta, percent as pct, type ChunkProgress } from '../stores/download'
import { GetSetting, SetSetting, GetDownloadDir } from '../../bindings/cczjVideo/app'
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
    errorStore.fromError('保存下载目录失败', e, 'Downloads.applyDownloadDir')
  } finally {
    savingDownloadDir.value = false
  }
}

async function resetDownloadDir(): Promise<void> {
  savingDownloadDir.value = true
  try {
    const defaultDir = await GetDownloadDir()
    downloadDirInput.value = defaultDir || ''
    await applyDownloadDir()
  } catch (e: any) {
    errorStore.fromError('获取默认下载目录失败', e, 'Downloads.resetDownloadDir')
    downloadDirInput.value = ''
    await applyDownloadDir()
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
    case 'queued': return '排队中'
    case 'downloading': return '下载中'
    case 'paused': return '已暂停'
    case 'done': return '已完成'
    case 'error': return '失败'
    case 'cancelled': return '已取消'
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
    title: '取消下载',
    message: `确定取消全部 ${ids.length} 个进行中的任务？`,
    okText: '取消全部',
    level: 'warn',
  })
  if (!yes) return
  for (const id of ids) await dl.cancel(id)
}
async function removeAllCompleted(): Promise<void> {
  const ids = dl.tasks.filter((t) => t.status === 'done').map((t) => t.task_id)
  if (ids.length === 0) return
  const yes = await confirmStore.confirm({
    title: '移除已完成',
    message: `确定移除所有 ${ids.length} 个已完成任务？（文件保留）`,
    okText: '移除',
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
    <header class="page-header">
      <div class="header-top">
        <div>
          <h1>下载管理</h1>
          <p class="subtitle" v-if="dl.dir">默认目录：{{ dl.dir }}</p>
        </div>
        <!-- 批量操作按钮 -->
        <div v-if="dl.tasks.length > 0" class="bulk-actions">
          <button
            v-if="stats.active > 0"
            class="b-btn b-btn-pause"
            @click="pauseAllActive"
          >
            <Icon name="pause" :size="11" /><span>全部暂停 ({{ stats.active }})</span>
          </button>
          <button
            v-if="stats.paused > 0"
            class="b-btn b-btn-resume"
            @click="resumeAllPaused"
          >
            <Icon name="play" :size="11" /><span>全部继续 ({{ stats.paused }})</span>
          </button>
          <button
            v-if="stats.active + stats.paused > 0"
            class="b-btn b-btn-danger"
            @click="cancelAllActive"
          >
            <Icon name="close" :size="11" /><span>取消全部</span>
          </button>
          <button
            v-if="stats.done > 0"
            class="b-btn b-btn-remove"
            @click="removeAllCompleted"
          >
            <Icon name="trash" :size="11" /><span>清除已完成 ({{ stats.done }})</span>
          </button>
        </div>
      </div>

      <div class="filter-row">
        <Tag :active="filter === 'all'" @click="filter = 'all'">全部 ({{ stats.total }})</Tag>
        <Tag :active="filter === 'active'" @click="filter = 'active'">下载中 ({{ stats.active }})</Tag>
        <Tag :active="filter === 'paused'" @click="filter = 'paused'">已暂停 ({{ stats.paused }})</Tag>
        <Tag :active="filter === 'done'" @click="filter = 'done'">已完成 ({{ stats.done }})</Tag>
        <Tag :active="filter === 'error'" @click="filter = 'error'">失败/取消 ({{ stats.error }})</Tag>
      </div>
    </header>

    <!-- ========== 下载设置 ========== -->
    <section class="download-settings-block">
      <h3>下载目录</h3>
      <div class="setting-row">
        <input
          type="text"
          class="setting-input"
          v-model="downloadDirInput"
          placeholder="留空则使用默认目录"
          @keyup.enter="applyDownloadDir"
        />
        <Button variant="primary" size="md" :disabled="savingDownloadDir" :loading="savingDownloadDir" @click="applyDownloadDir">
          保存
        </Button>
        <Button variant="secondary" size="md" :disabled="savingDownloadDir" @click="resetDownloadDir">
          使用默认
        </Button>
      </div>
      <p class="hint">当前目录：{{ dl.dir || '未设置（使用默认）' }}</p>
      <p class="hint">提示：m3u8 视频会自动解析并下载所有分片，合并为单个 TS 文件保存。支持多连接并行下载（IDM 风格）。</p>
    </section>

    <div v-if="dl.tasks.length === 0" class="empty">
      <div class="empty-icon">⬇️</div>
      <div class="empty-title">还没有下载任务</div>
      <div class="empty-desc">在详情页点击"下载到本地"即可新建任务</div>
    </div>

    <div v-else class="task-list">
      <div
        v-for="task in dl.tasks.filter(isMatchFilter)"
        :key="task.task_id"
        class="task-card"
      >
        <div class="task-head">
          <div class="task-title" :title="task.filename">
            <Icon name="download" :size="14" />
            <span>{{ task.vod_name || task.filename }}</span>
            <span v-if="task.ep_name" class="task-ep">- {{ task.ep_name }}</span>
          </div>
          <span :class="['status-chip', statusClass(task.status)]">{{ statusLabel(task.status) }}</span>
        </div>

        <div class="task-meta-row">
          <span class="muted">{{ formatBytes(task.downloaded) }} / {{ task.total > 0 ? formatBytes(task.total) : '未知大小' }}</span>
          <span v-if="task.status === 'paused'" class="speed status-paused-label">已暂停</span>
          <span v-if="task.status === 'downloading'" class="speed">{{ formatSpeed(task.speed_bps) }}</span>
          <span v-if="task.status === 'downloading'" class="eta">剩余 {{ formatEta(task.eta_sec) }}</span>
          <span v-if="task.status === 'queued'" class="muted">等待下载...</span>
          <span v-if="task.error" class="err-msg" :title="task.error">{{ task.error }}</span>
        </div>

        <!-- IDM 风格分块进度可视化 -->
        <div v-if="task.chunks && task.chunks.length > 0 && task.total > 0 && task.status === 'downloading'" class="chunk-progress-container">
          <div
            v-for="chunk in task.chunks"
            :key="chunk.id"
            class="chunk-bar"
            :style="{
              left: chunkLeft(chunk, task.total) + '%',
              width: chunkWidth(chunk, task.total) + '%',
              background: chunkColor(chunk),
            }"
            :title="`分块 ${chunk.id + 1}: ${chunkPercent(chunk)}% (${formatBytes(chunk.done)} / ${formatBytes(chunk.end - chunk.start + 1)})`"
          >
            <span class="chunk-label" v-if="chunkWidth(chunk, task.total) > 8">
              {{ chunk.id + 1 }}
            </span>
          </div>
        </div>

        <div class="progress-track">
          <div class="progress-fill" :style="{ width: pct(task) + '%' }"></div>
          <span class="progress-text">{{ pct(task) }}%</span>
        </div>

        <div class="task-footer">
          <span class="file-path" :title="task.save_path">{{ task.save_path }}</span>
          <div class="task-actions">
            <button
              v-if="task.status === 'downloading' || task.status === 'queued'"
              class="t-btn t-btn-pause"
              @click="dl.pause(task.task_id)"
            >
              <Icon name="pause" :size="12" /><span>暂停</span>
            </button>
            <button
              v-if="task.status === 'paused'"
              class="t-btn t-btn-resume"
              @click="dl.resume(task.task_id)"
            >
              <Icon name="play" :size="12" /><span>继续</span>
            </button>
            <button
              v-if="task.status === 'downloading' || task.status === 'queued' || task.status === 'paused'"
              class="t-btn t-btn-danger"
              @click="dl.cancel(task.task_id)"
            >
              <Icon name="close" :size="12" /><span>取消</span>
            </button>
            <button
              v-if="task.status === 'done'"
              class="t-btn t-btn-open"
              @click="dl.openFile(task.save_path)"
            >
              <Icon name="play" :size="12" /><span>打开</span>
            </button>
            <button class="t-btn t-btn-remove" @click="dl.remove(task.task_id)">
              <Icon name="trash" :size="12" /><span>移除</span>
            </button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.downloads-page {
  max-width: 1100px;
  margin: 0 auto;
  color: var(--text-primary);
}
.page-header {
  margin-bottom: 20px;
}
.header-top {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 16px;
  margin-bottom: 12px;
  flex-wrap: wrap;
}
.page-header h1 {
  font-size: 22px;
  font-weight: 600;
  margin: 0 0 4px 0;
}
.subtitle {
  font-size: 12px;
  color: var(--text-muted);
  margin: 0;
}
.filter-row {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

/* 批量操作按钮区 */
.bulk-actions {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
  align-items: center;
}
.b-btn {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 4px 10px;
  border-radius: 6px;
  border: 1px solid var(--border);
  background: var(--bg-card);
  font-size: 11px;
  cursor: pointer;
  transition: all 0.15s;
  font-family: inherit;
}
.b-btn-pause { background: rgba(245, 158, 11, 0.1); border-color: rgba(245, 158, 11, 0.4); color: #f59e0b; }
.b-btn-pause:hover { background: rgba(245, 158, 11, 0.2); border-color: #f59e0b; }
.b-btn-resume { background: rgba(34, 197, 94, 0.1); border-color: rgba(34, 197, 94, 0.4); color: #22c55e; }
.b-btn-resume:hover { background: rgba(34, 197, 94, 0.2); border-color: #22c55e; }
.b-btn-danger { background: rgba(239, 68, 68, 0.1); border-color: rgba(239, 68, 68, 0.4); color: #ef4444; }
.b-btn-danger:hover { background: rgba(239, 68, 68, 0.2); border-color: #ef4444; }
.b-btn-remove { background: rgba(107, 114, 128, 0.1); border-color: rgba(107, 114, 128, 0.4); color: #6b7280; }
.b-btn-remove:hover { background: rgba(107, 114, 128, 0.2); border-color: #6b7280; color: #6b7280; }

/* ========== 下载设置 ========== */
.download-settings-block {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 12px;
  padding: 18px 20px;
  margin-bottom: 20px;
}
.download-settings-block h3 {
  font-size: 14px;
  font-weight: 700;
  margin: 0 0 14px;
}
.setting-row {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}
.setting-input {
  flex: 1;
  min-width: 200px;
  padding: 8px 12px;
  border-radius: 8px;
  border: 1px solid var(--border);
  background: var(--bg-secondary);
  color: var(--text-primary);
  font-size: 13px;
  font-family: inherit;
  outline: none;
  transition: border-color 0.15s;
}
.setting-input:focus {
  border-color: var(--accent);
}
.hint {
  margin-top: 8px;
  font-size: 12px;
  color: var(--text-muted);
  line-height: 1.5;
}

.f-btn {
  padding: 6px 14px;
  border-radius: 18px;
  border: 1px solid var(--border);
  background: var(--bg-card);
  color: var(--text-secondary);
  font-size: 12px;
  cursor: pointer;
  transition: all 0.15s;
  font-family: inherit;
}
.f-btn:hover {
  border-color: var(--accent);
  color: var(--accent);
}
.f-btn.active {
  background: var(--accent);
  border-color: var(--accent);
  color: var(--accent-contrast);
}

.empty {
  text-align: center;
  padding: 60px 20px;
  background: var(--bg-card);
  border-radius: 12px;
  border: 1px dashed var(--border);
}
.empty-icon { font-size: 44px; margin-bottom: 12px; }
.empty-title { font-size: 16px; font-weight: 600; margin-bottom: 6px; }
.empty-desc { font-size: 13px; color: var(--text-muted); }

.task-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.task-card {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 12px;
  padding: 16px 18px;
  transition: all 0.15s;
}
.task-card:hover {
  border-color: var(--accent-alpha-20);
  box-shadow: 0 4px 14px var(--accent-alpha-10);
}
.task-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 10px;
  gap: 10px;
}
.task-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 14px;
  font-weight: 600;
  color: var(--text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.task-ep {
  color: var(--text-secondary);
  font-weight: 500;
  margin-left: 4px;
}
.status-chip {
  flex-shrink: 0;
  padding: 3px 10px;
  font-size: 11px;
  border-radius: 12px;
  font-weight: 500;
}
.status-queued { background: rgba(120, 120, 120, 0.15); color: #8a8a8a; }
.status-downloading { background: rgba(24, 144, 255, 0.15); color: #1890ff; }
.status-paused { background: rgba(245, 158, 11, 0.15); color: #f59e0b; }
.status-done { background: rgba(22, 163, 74, 0.15); color: #10b981; }
.status-error, .status-cancelled { background: rgba(239, 68, 68, 0.15); color: #ef4444; }

.task-meta-row {
  display: flex;
  gap: 14px;
  font-size: 12px;
  color: var(--text-muted);
  margin-bottom: 8px;
  align-items: center;
  flex-wrap: wrap;
}
.task-meta-row .speed { color: #1890ff; font-variant-numeric: tabular-nums; }
.task-meta-row .eta { color: var(--text-secondary); font-variant-numeric: tabular-nums; }
.task-meta-row .err-msg { color: #ef4444; max-width: 300px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }

/* ========== IDM 风格分块进度条 ========== */
.chunk-progress-container {
  position: relative;
  width: 100%;
  height: 8px;
  background: var(--bg-secondary);
  border: 1px solid var(--border);
  border-radius: 4px;
  overflow: hidden;
  margin-bottom: 8px;
}
.chunk-bar {
  position: absolute;
  top: 0;
  height: 100%;
  border-radius: 2px;
  transition: width 0.2s ease, background 0.3s ease;
  display: flex;
  align-items: center;
  justify-content: center;
  min-width: 2px;
}
.chunk-bar:not(:last-child) {
  border-right: 1px solid rgba(0, 0, 0, 0.15);
}
.chunk-label {
  font-size: 7px;
  font-weight: 700;
  color: rgba(255, 255, 255, 0.85);
  text-shadow: 0 1px 2px rgba(0, 0, 0, 0.4);
  pointer-events: none;
}

.progress-track {
  position: relative;
  width: 100%;
  height: 18px;
  background: var(--bg-secondary);
  border: 1px solid var(--border);
  border-radius: 9px;
  overflow: hidden;
  margin-bottom: 10px;
}
.progress-fill {
  position: absolute;
  inset: 0 auto 0 0;
  height: 100%;
  background: linear-gradient(90deg, var(--accent) 0%, #36a3ff 100%);
  transition: width 0.3s ease;
}
.progress-text {
  position: relative;
  display: flex;
  align-items: center;
  justify-content: center;
  height: 100%;
  font-size: 11px;
  font-weight: 600;
  color: var(--text-primary);
  text-shadow: 0 1px 2px rgba(255, 255, 255, 0.4);
}

.task-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}
.file-path {
  font-size: 11px;
  color: var(--text-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  flex: 1;
  min-width: 0;
}
.task-actions {
  display: flex;
  gap: 6px;
  flex-shrink: 0;
}
.t-btn {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 5px 10px;
  border-radius: 6px;
  border: 1px solid var(--border);
  background: var(--bg-secondary);
  color: var(--text-secondary);
  font-size: 11px;
  cursor: pointer;
  transition: all 0.15s;
  font-family: inherit;
}
.t-btn:hover {
  border-color: var(--accent);
  color: var(--accent);
}
.t-btn.primary {
  background: var(--accent);
  border-color: var(--accent);
  color: var(--accent-contrast);
}
.t-btn.primary:hover {
  background: var(--accent-dim);
  border-color: var(--accent-dim);
  color: var(--accent-contrast);
}

/* 按钮类型样式 */
.t-btn-pause {
  background: rgba(245, 158, 11, 0.1);
  border-color: rgba(245, 158, 11, 0.4);
  color: #f59e0b;
}
.t-btn-pause:hover {
  background: rgba(245, 158, 11, 0.2);
  border-color: #f59e0b;
  color: #f59e0b;
}

.t-btn-resume {
  background: rgba(34, 197, 94, 0.1);
  border-color: rgba(34, 197, 94, 0.4);
  color: #22c55e;
}
.t-btn-resume:hover {
  background: rgba(34, 197, 94, 0.2);
  border-color: #22c55e;
  color: #22c55e;
}

.t-btn-danger {
  background: rgba(239, 68, 68, 0.1);
  border-color: rgba(239, 68, 68, 0.4);
  color: #ef4444;
}
.t-btn-danger:hover {
  background: rgba(239, 68, 68, 0.2);
  border-color: #ef4444;
  color: #ef4444;
}

.t-btn-open {
  background: rgba(24, 144, 255, 0.15);
  border-color: rgba(24, 144, 255, 0.5);
  color: #1890ff;
}
.t-btn-open:hover {
  background: rgba(24, 144, 255, 0.25);
  border-color: #1890ff;
  color: #1890ff;
}

.t-btn-remove {
  background: rgba(107, 114, 128, 0.1);
  border-color: rgba(107, 114, 128, 0.4);
  color: #6b7280;
}
.t-btn-remove:hover {
  background: rgba(107, 114, 128, 0.2);
  border-color: #ef4444;
  color: #ef4444;
}

.status-paused-label {
  color: #f59e0b !important;
  font-weight: 500;
}
</style>