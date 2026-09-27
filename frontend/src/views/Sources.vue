<script setup lang="ts">
defineOptions({ name: 'Sources' })
import { onMounted, ref, computed } from 'vue'
import { useSourceStore } from '../stores/source'
import { useCollectStore } from '../stores/collect'
import { useErrorStore } from '../stores/error'
import type { CollectScheduleConfig, SourceScheduleItem } from '../stores/collect'
import { AddSource, UpdateSource, DeleteSource, GetSourceParamsDoc, ExportSource, ImportSourceFromBase64, OpenFolder, RunSourceAction } from '../api/app'
import Icon from '../components/Icon.vue'
import { Button, Modal, Tag, Spinner as LoadingSpinner, Empty as EmptyState, MotionTransition, Select as SelectDropdown } from '../components/ui'
import { useConfirmStore } from '../stores/confirm'
import { extractDomainKey } from '../utils'
import { tr } from '../locales'

const errorStore = useErrorStore()

interface EditSource {
  source_key: string
  name: string
  api_url: string
  collect_limit?: number
  collect_hours?: number
  enabled?: number
}

interface ParamsDoc {
  base_url: string
  path_params?: { name: string; type: string; desc: string; example: string }[]
  query_ac?: { name: string; type: string; desc: string; example: string }[]
  query_common?: { name: string; type: string; desc: string; example: string }[]
  query_advanced?: { name: string; type: string; desc: string; example: string }[]
}

const sourceStore = useSourceStore()
const collectStore = useCollectStore()
const confirmStore = useConfirmStore()

// === 搜索与过滤 ===
const search = ref('')
const statusFilter = ref<string>('all')

const filteredSources = computed(() => {
  let list = sourceStore.sources
  if (search.value) {
    const q = search.value.toLowerCase()
    list = list.filter(s => {
      return (s.name || '').toLowerCase().includes(q) ||
             (s.source_key || '').toLowerCase().includes(q) ||
             (s.api_url || '').toLowerCase().includes(q)
    })
  }
  if (statusFilter.value !== 'all') {
    list = list.filter(s => {
      const key = sk(s)
      if (statusFilter.value === 'running') return isRunning(key) || isPaused(key)
      if (statusFilter.value === 'idle') return !isRunning(key) && !isPaused(key) && !hasError(key)
      if (statusFilter.value === 'error') return hasError(key)
      return true
    })
  }
  return list
})

// === 表单状态 ===
const showForm = ref(false)
const editing = ref<string | null>(null)
const form = ref({ name: '', api_url: '', collect_limit: 0, collect_hours: 0, enabled: true })
const showAdvanced = ref(false)

// === 采集模式选择（每个源独立）===
const selectedModes = ref<Map<string, string>>(new Map())
const selectedHours = ref<Map<string, number>>(new Map())
function getMode(key: string): string {
  return selectedModes.value.get(key) || 'full'
}
function getHours(key: string): number {
  return selectedHours.value.get(key) || 0
}

// === 展开状态 ===
const expandedKey = ref<string | null>(null)
const paramsDoc = ref<ParamsDoc | null>(null)
const paramsDocLoading = ref(false)

function toggleExpand(key: string): void {
  if (expandedKey.value === key) {
    expandedKey.value = null
    paramsDoc.value = null
    return
  }
  expandedKey.value = key
  void collectStore.syncFromBackend(key)
}

// === 定时配置面板 ===
const scheduleVisible = ref<Set<string>>(new Set())
const scheduleForms = ref<Map<string, { enabled: boolean; mode: string; interval_min: number }>>(new Map())

function openSchedule(key: string): void {
  const set = scheduleVisible.value
  if (set.has(key)) {
    set.delete(key)
    scheduleVisible.value = new Set(set)
    return
  }
  set.add(key)
  scheduleVisible.value = new Set(set)
  const item = collectStore.schedulerStatus?.source_schedules?.find((s: SourceScheduleItem) => s.source_key === key)
  if (item) {
    scheduleForms.value.set(key, { enabled: item.enabled, mode: item.mode || 'incremental', interval_min: item.interval_min || 30 })
  } else {
    scheduleForms.value.set(key, { enabled: false, mode: 'incremental', interval_min: 30 })
  }
}

async function saveSchedule(key: string): Promise<void> {
  const f = scheduleForms.value.get(key)
  if (!f) return
  await collectStore.saveSourceSchedule(key, f.enabled, f.mode, f.interval_min)
  const set = scheduleVisible.value
  set.delete(key)
  scheduleVisible.value = new Set(set)
}

function scheduleStateFor(key: string): SourceScheduleItem | undefined {
  return collectStore.schedulerStatus?.source_schedules?.find((s: SourceScheduleItem) => s.source_key === key)
}

// === 全局调度配置 ===
const globalScheduleVisible = ref(false)
const globalSchedule = ref<CollectScheduleConfig | null>(null)

const globalIntervalMinutes = computed<number>({
  get: () => Math.max(1, Math.round((globalSchedule.value?.background_interval_seconds || 60) / 60)),
  set: (minutes) => {
    if (globalSchedule.value) globalSchedule.value.background_interval_seconds = Math.max(1, minutes) * 60
  },
})

function toggleGlobalSchedule(): void {
  globalScheduleVisible.value = !globalScheduleVisible.value
  if (!globalScheduleVisible.value) return
  const cfg = collectStore.scheduleConfig
  globalSchedule.value = cfg ? { ...cfg } : null
}

async function saveGlobalSchedule(): Promise<void> {
  const cfg = globalSchedule.value
  if (!cfg) return
  try {
    await collectStore.saveSchedule(cfg)
    const saved = collectStore.scheduleConfig
    if (saved) globalSchedule.value = { ...saved }
    errorStore.info(tr('common.saved'), tr('sources.scheduleSaved'), '', 'Sources.saveGlobalSchedule')
  } catch (e) {
    errorStore.fromError(tr('sources.scheduleSaveFailed'), e, 'Sources.saveGlobalSchedule')
  }
}

// === 生命周期 ===
onMounted(async () => {
  try {
    await Promise.all([
      sourceStore.loadSources().catch(e => {
        console.error('[Sources] loadSources failed:', e)
        errorStore.fromError(tr('errors.loadSourcesFailed'), e)
      }),
      collectStore.loadSchedule().catch(e => {
        console.error('[Sources] loadSchedule failed:', e)
      })
    ])
  } catch (e) {
    console.error('[Sources] onMounted init failed:', e)
  }
})

// 工具栏刷新：源列表 + 调度器 + 各源后端状态（后台采集时前端事件会漏）
async function refreshAll(): Promise<void> {
  await Promise.all([
    sourceStore.loadSources().catch(() => { }),
    collectStore.loadSchedule().catch(() => { }),
  ])
  await collectStore.syncAllFromBackend(sourceStore.sources.map(sk))
}

// === 添加/编辑弹窗 ===
const autoKey = computed(() => extractDomainKey(form.value.api_url))

function openAdd(): void {
  console.log('[Sources] openAdd called, showForm before:', showForm.value)
  editing.value = null
  form.value = { name: '', api_url: '', collect_limit: 50, collect_hours: 0, enabled: true }
  showAdvanced.value = false
  showForm.value = true
  console.log('[Sources] openAdd done, showForm after:', showForm.value)
}

function openEdit(s: EditSource): void {
  editing.value = s.source_key
  form.value = {
    name: s.name,
    api_url: s.api_url,
    collect_limit: s.collect_limit ?? 0,
    collect_hours: s.collect_hours ?? 0,
    enabled: (s.enabled ?? 1) === 1,
  }
  showAdvanced.value = !!(s.collect_limit || s.collect_hours)
  showForm.value = true
}

async function save(): Promise<void> {
  const payload = {
    source_key: editing.value || '',
    name: form.value.name,
    api_url: form.value.api_url,
    collect_limit: Number(form.value.collect_limit) || 0,
    collect_hours: Number(form.value.collect_hours) || 0,
    enabled: form.value.enabled ? 1 : 0,
  }
  if (editing.value) {
    await UpdateSource(payload as any)
  } else {
    await AddSource(payload as any)
  }
  showForm.value = false
  await sourceStore.loadSources()
}

// === 删除 ===
async function deleteSourceConfirm(key: string): Promise<void> {
  const yes = await confirmStore.confirm({
    title: tr('sources.deleteTitle'),
    message: tr('sources.deleteConfirmMsg'),
    okText: tr('common.delete'),
    level: 'danger',
  })
  if (!yes) return
  await DeleteSource(key)
  await sourceStore.loadSources()
}

function handleSetDefault(source: any): void {
  const key = sk(source)
  if (key === sourceStore.currentSourceKey) return
  sourceStore.switchSource(key)
  errorStore.info(tr('sources.defaultSwitchedTitle'), tr('sources.defaultSwitched', { name: source.name }))
}

// === 采集操作 ===
function startCollect(key: string, mode?: string, hours?: number): void {
  const m = mode || getMode(key)
  const h = hours ?? getHours(key)
  if (m) selectedModes.value.set(key, m)
  if (h > 0) selectedHours.value.set(key, h)
  collectStore.startCollect(key, m as any, h)
}

// === 导入导出 ===
const exportResult = ref<{ path: string; sourceKey: string } | null>(null)

async function exportSource(key: string): Promise<void> {
  try {
    const fpath = await ExportSource(key) as string
    exportResult.value = { path: fpath, sourceKey: key }
  } catch (e) {
    console.error('export failed', e)
  }
}

async function openExportFolder(): Promise<void> {
  if (!exportResult.value) return
  try {
    await OpenFolder(exportResult.value.path)
  } catch (e) {
    console.error('open folder failed', e)
  }
}

function dismissExport(): void {
  exportResult.value = null
}

const importDialogOpen = ref(false)
const importDragging = ref(false)
const importFile = ref<File | null>(null)
const importLoading = ref(false)
const importFileInput = ref<HTMLInputElement | null>(null)

function openImportDialog(): void {
  importDragging.value = false
  importFile.value = null
  importLoading.value = false
  importDialogOpen.value = true
}

function closeImportDialog(): void {
  importDialogOpen.value = false
  importFile.value = null
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
      await sourceStore.loadSources()
      closeImportDialog()
    } catch (e) {
      console.error('import failed', e)
      importLoading.value = false
    }
  }
  reader.readAsDataURL(file)
}

// === 参数面板 ===
async function toggleParams(key: string, apiUrl: string): Promise<void> {
  if (expandedKey.value === key && paramsDoc.value) {
    paramsDoc.value = null
    return
  }
  expandedKey.value = key
  paramsDoc.value = null
  paramsDocLoading.value = true
  try {
    const doc = await GetSourceParamsDoc(key) as any
    paramsDoc.value = {
      base_url: apiUrl,
      path_params: doc.path_params || [],
      query_ac: doc.query_ac || [],
      query_common: doc.query_common || [],
      query_advanced: doc.query_advanced || [],
    }
  } catch (e) {
    paramsDoc.value = { base_url: apiUrl, path_params: [], query_ac: [], query_common: [], query_advanced: [] }
  } finally {
    paramsDocLoading.value = false
  }
}

// === 辅助 ===
function sk(s: { source_key?: string }): string {
  return s.source_key || ''
}

function isRunning(key: string): boolean {
  const st = collectStore.getState(key)
  return st.running && !st.paused
}

function isPaused(key: string): boolean {
  const st = collectStore.getState(key)
  return st.running && st.paused
}

function hasError(key: string): boolean {
  const st = collectStore.getState(key)
  return !!st.error
}

function statusClass(key: string): string {
  if (isRunning(key)) return 'running'
  if (isPaused(key)) return 'paused'
  if (hasError(key)) return 'error'
  return 'idle'
}

function statusText(key: string): string {
  if (isRunning(key)) return tr('sources.statusRunning')
  if (isPaused(key)) return tr('sources.statusPaused')
  if (hasError(key)) return tr('sources.statusError')
  return tr('sources.statusIdle')
}

function modeLabel(m: string): string {
  switch (m) {
    case 'full': return tr('sources.modeFullShort')
    case 'incremental': return tr('sources.modeIncrShort')
    case 'once': return tr('sources.modeOnceShort')
    default: return m
  }
}

function modeTagVariant(m: string): 'primary' | 'success' | 'warning' {
  switch (m) {
    case 'full': return 'primary'
    case 'incremental': return 'success'
    case 'once': return 'warning'
    default: return 'primary'
  }
}

function progressFor(key: string): number {
  return collectStore.progressFor(key)
}

function formatProgress(key: string): string {
  const st = collectStore.getState(key)
  if (st.total <= 0) return '--'
  return `${st.current}/${st.total}`
}

// === 数据维护（迁移自旧后台面板） ===
const statusSyncing = ref('')
const truncating = ref('')

function sourceName(key: string): string {
  const s = sourceStore.sources.find(x => sk(x) === key)
  return s?.name || key
}

function formatSyncTime(ts: number): string {
  if (!ts) return '--'
  return new Date(ts).toLocaleTimeString()
}

async function refreshStatus(key: string): Promise<void> {
  statusSyncing.value = key
  try {
    await collectStore.syncFromBackend(key)
  } finally {
    statusSyncing.value = ''
  }
}

async function truncateSource(key: string): Promise<void> {
  const ok = await confirmStore.confirm({
    title: tr('sources.truncateTitle'),
    message: tr('sources.truncateConfirm', { name: sourceName(key) }),
    okText: tr('common.clearAll'),
    level: 'danger',
  })
  if (!ok) return
  truncating.value = key
  try {
    await RunSourceAction({ source_key: key, action: 'truncate', vod_id: '' })
    errorStore.info(tr('sources.truncateDone'), tr('sources.truncateDoneMsg', { name: sourceName(key) }), '', 'Sources')
    await sourceStore.loadSources()
    await collectStore.syncFromBackend(key)
  } catch (e: any) {
    errorStore.fromError(tr('sources.truncateFailed'), e, 'Sources.truncateSource')
  } finally {
    truncating.value = ''
  }
}

function copyText(text: string, label = '已复制'): void {
  if (!text) return
  if (navigator.clipboard && navigator.clipboard.writeText) {
    navigator.clipboard.writeText(text).then(() => { console.log(label + ': ' + text) }).catch(() => { fallbackCopy(text) })
  } else {
    fallbackCopy(text)
  }
}
function fallbackCopy(text: string): void {
  const ta = document.createElement('textarea')
  ta.value = text
  ta.style.position = 'fixed'
  ta.style.opacity = '0'
  document.body.appendChild(ta)
  ta.select()
  try { document.execCommand('copy') } catch (e) { console.warn('copy failed', e) }
  document.body.removeChild(ta)
}
</script>

<template>
  <div class="sources-page">
    <!-- 页头 -->
    <div class="page-header">
      <div class="page-title">
        <h1>{{ tr('sources.title') }}</h1>
        <p class="page-desc">{{ tr('sources.pageDesc') }}</p>
      </div>
    </div>

    <!-- 顶部操作栏 -->
    <div class="toolbar">
      <div class="toolbar-left">
        <div class="search-box">
          <Icon name="search" :size="14" />
          <input v-model="search" :placeholder="tr('sources.searchPlaceholder')" />
          <button v-if="search" class="search-clear" @click="search = ''">
            <Icon name="close" :size="10" />
          </button>
        </div>
        <SelectDropdown v-model="statusFilter" :options="[{ value: 'all', label: tr('common.all') }, { value: 'running', label: tr('sources.statusRunning') }, { value: 'idle', label: tr('sources.statusIdle') }, { value: 'error', label: tr('sources.statusError') }]" size="sm" />
      </div>
      <div class="toolbar-right">
        <Button variant="secondary" size="sm" @click="refreshAll" :title="tr('common.refresh')">
          <Icon name="refresh" :size="14" />
          <span>{{ tr('common.refresh') }}</span>
        </Button>
        <Button variant="ghost" size="sm" @click="openImportDialog" :title="tr('common.import')">
          <Icon name="upload" :size="14" />
          <span>{{ tr('common.import') }}</span>
        </Button>
        <Button variant="primary" size="sm" @click="openAdd">
          <Icon name="plus" :size="14" />
          <span>{{ tr('sources.addSource') }}</span>
        </Button>
      </div>
    </div>

    <!-- 全局调度器状态条 -->
    <div v-if="collectStore.schedulerStatus" class="scheduler-bar" :class="{ active: collectStore.schedulerStatus.running }">
      <div class="scheduler-indicator" :class="{ on: collectStore.schedulerStatus.running }"></div>
      <span class="scheduler-label">{{ tr('sources.backendScheduler') }}</span>
      <span class="scheduler-state">{{ collectStore.schedulerStatus.running ? tr('sources.statusRunning') : tr('sources.statusStopped') }}</span>
      <span class="scheduler-note">{{ collectStore.schedulerStatus.note }}</span>
      <div class="scheduler-actions">
        <button v-if="collectStore.schedulerStatus.running" class="mini-btn danger" @click="collectStore.stopBackground()">
          <Icon name="stop" :size="10" /><span>{{ tr('common.stop') }}</span>
        </button>
        <button v-else class="mini-btn accent" @click="collectStore.triggerNow()">
          <Icon name="play" :size="10" /><span>{{ tr('sources.startFull') }}</span>
        </button>
        <button class="mini-btn" :class="{ accent: globalScheduleVisible }" @click="toggleGlobalSchedule">
          <Icon name="settings" :size="10" /><span>{{ tr('sources.scheduleSettings') }}</span>
        </button>
        <button class="mini-btn" @click="collectStore.loadSchedule()">
          <Icon name="refresh" :size="10" /><span>{{ tr('common.refresh') }}</span>
        </button>
      </div>
    </div>

    <!-- 全局调度参数（采集相关的节奏与节流只在这一页改） -->
    <div v-if="globalScheduleVisible && globalSchedule" class="scheduler-config">
      <div class="sc-row">
        <label class="sc-check">
          <input type="checkbox" v-model="globalSchedule.enable_background" />
          <span>{{ tr('sources.enableGlobalSchedule') }}</span>
        </label>
        <div class="sc-field">
          <span class="sc-label">{{ tr('sources.scheduleInterval') }}</span>
          <input type="range" v-model.number="globalIntervalMinutes" min="1" max="180" step="1" />
          <span class="sc-value">{{ tr('sources.minuteCount', { n: globalIntervalMinutes }) }}</span>
        </div>
      </div>
      <div class="sc-row">
        <div class="sc-field">
          <span class="sc-label">{{ tr('sources.scheduleSourceGap') }}</span>
          <input class="sc-input" type="number" min="1" max="600" v-model.number="globalSchedule.source_gap_seconds" />
        </div>
        <div class="sc-field">
          <span class="sc-label">{{ tr('sources.schedulePageGap') }}</span>
          <input class="sc-input" type="number" min="1" max="600" v-model.number="globalSchedule.page_gap_seconds" />
        </div>
        <label class="sc-check">
          <input type="checkbox" v-model="globalSchedule.enable_startup_catchup" />
          <span>{{ tr('sources.scheduleStartupCatchup') }}</span>
        </label>
        <label class="sc-check">
          <input type="checkbox" v-model="globalSchedule.enable_initial_full_collect" />
          <span>{{ tr('sources.scheduleInitialFull') }}</span>
        </label>
      </div>
      <div class="sc-row">
        <Button variant="primary" size="sm" :loading="collectStore.scheduleSaving" @click="saveGlobalSchedule">
          <Icon name="save" :size="12" /><span>{{ tr('common.save') }}</span>
        </Button>
        <small class="sc-hint">{{ tr('sources.scheduleHint') }}</small>
      </div>
    </div>

    <!-- 加载中 -->
    <div v-if="sourceStore.loading" class="content-loader">
      <LoadingSpinner :label="tr('sources.loadingSources')" />
    </div>

    <!-- 空状态 -->
    <div v-else-if="sourceStore.sources.length === 0">
      <EmptyState icon="📡" :title="tr('sources.emptyTitle')" :description="tr('sources.emptyDescHint')">
        <Button variant="primary" size="sm" @click="openAdd">
          <Icon name="plus" :size="14" />
          <span>{{ tr('sources.add') }}</span>
        </Button>
      </EmptyState>
    </div>

    <!-- 源卡片网格 -->
    <div v-else class="card-grid">
      <div v-for="s in filteredSources" :key="sk(s)" class="source-card" :class="{ expanded: expandedKey === sk(s), running: isRunning(sk(s)) || isPaused(sk(s)) }">
        <!-- 卡片头部（始终可见） -->
        <div class="card-header" @click="toggleExpand(sk(s))">
          <div class="card-header-left">
            <span class="status-dot" :class="statusClass(sk(s))"></span>
            <h3 class="card-name">{{ s.name }}</h3>
            <Tag :variant="modeTagVariant(getMode(sk(s)))" size="sm">{{ modeLabel(getMode(sk(s))) }}</Tag>
            <span v-if="scheduleStateFor(sk(s))?.enabled" class="schedule-mini" :title="tr('sources.everyNMinutes', { n: scheduleStateFor(sk(s))?.interval_min })">
              <Icon name="clock" :size="10" />
              {{ scheduleStateFor(sk(s))?.interval_min }} {{ tr('sources.minuteUnit') }}
            </span>
          </div>
          <div class="card-header-right">
            <span v-if="isRunning(sk(s)) || isPaused(sk(s))" class="mini-progress-text">{{ formatProgress(sk(s)) }}</span>
            <span class="expand-arrow">{{ expandedKey === sk(s) ? '▴' : '▾' }}</span>
          </div>
        </div>

        <!-- 折叠时的迷你进度条（仅运行时显示） -->
        <div v-if="(isRunning(sk(s)) || isPaused(sk(s))) && expandedKey !== sk(s)" class="mini-progress">
          <div class="mini-progress-track">
            <div class="mini-progress-fill" :style="{ width: progressFor(sk(s)) + '%' }"></div>
          </div>
          <span class="mini-progress-pct">{{ progressFor(sk(s)) }}%</span>
        </div>

        <!-- 操作按钮行（始终可见） -->
        <div class="card-actions">
          <!-- 模式选择下拉 -->
          <SelectDropdown :model-value="getMode(sk(s))" :options="[{ value: 'full', label: tr('sources.fullCollect') }, { value: 'incremental', label: tr('sources.incrementalCollect') }, { value: 'once', label: tr('sources.onceCollect') }]" @update:model-value="(v: any) => { selectedModes.set(sk(s), String(v)) }" :disabled="isRunning(sk(s)) || isPaused(sk(s))" size="sm" />
          <input
            v-if="getMode(sk(s)) === 'incremental'"
            type="number"
            class="hours-input-small"
            min="1" max="168"
            :value="getHours(sk(s)) || s.collect_hours || 24"
            @input="(e: Event) => selectedHours.set(sk(s), Number((e.target as HTMLInputElement).value))"
            placeholder="h"
            :disabled="isRunning(sk(s)) || isPaused(sk(s))"
            :title="tr('sources.lookbackHours')"
          />
          <span v-if="getMode(sk(s)) === 'incremental'" class="hours-suffix">{{ tr('sources.hourUnit') }}</span>

          <div class="action-spacer"></div>

          <template v-if="isRunning(sk(s)) || isPaused(sk(s))">
            <button class="icon-btn pause-btn" @click="isPaused(sk(s)) ? collectStore.resume(sk(s)) : collectStore.pause(sk(s))" :title="isPaused(sk(s)) ? tr('sources.resume') : tr('sources.pause')">
              <Icon :name="isPaused(sk(s)) ? 'play' : 'pause'" :size="14" />
            </button>
            <button class="icon-btn stop-btn" @click="collectStore.stop(sk(s))" :title="tr('common.stop')">
              <Icon name="stop" :size="14" />
            </button>
          </template>
          <template v-else>
            <button class="icon-btn play-btn" @click="startCollect(sk(s))" :title="tr('sources.startCollect')">
              <Icon name="play" :size="14" />
            </button>
            <button class="icon-btn incr-btn" @click="startCollect(sk(s), 'incremental', getHours(sk(s)) || s.collect_hours || 24)" :title="tr('sources.incrementalCollect')">
              <Icon name="refresh" :size="14" />
            </button>
            <button class="icon-btn export-btn" @click="exportSource(sk(s))" :title="tr('common.export')">
              <Icon name="download" :size="14" />
            </button>
          </template>
          <button class="icon-btn sched-btn" @click="openSchedule(sk(s))" :title="scheduleStateFor(sk(s))?.enabled ? tr('sources.scheduleEnabledTitle') : tr('sources.scheduleConfigTitle')">
              <Icon name="clock" :size="14" />
            </button>
            <button class="icon-btn default-btn" :class="{ active: sk(s) === sourceStore.currentSourceKey }" @click="handleSetDefault(s)" :title="sk(s) === sourceStore.currentSourceKey ? tr('sources.isDefault') : tr('sources.setAsDefault')">
              <Icon name="star" :size="14" />
            </button>
            <button class="icon-btn edit-btn" @click="openEdit({ source_key: sk(s), name: s.name, api_url: s.api_url, collect_limit: s.collect_limit, collect_hours: s.collect_hours })" :title="tr('common.edit')">
              <Icon name="edit" :size="14" />
            </button>
            <button class="icon-btn del-btn" @click="deleteSourceConfirm(sk(s))" :title="tr('common.delete')">
              <Icon name="trash" :size="14" />
            </button>
        </div>

        <!-- 展开内容 -->
        <div v-if="expandedKey === sk(s)" class="card-expanded">

          <!-- 1. 采集进度面板（仅运行时显示） -->
          <div v-if="isRunning(sk(s)) || isPaused(sk(s))" class="collect-progress-panel">
            <div class="cp-progress-wrap">
              <div class="cp-progress-track">
                <div class="cp-progress-fill" :style="{ width: progressFor(sk(s)) + '%' }"></div>
              </div>
              <span class="cp-progress-pct">{{ progressFor(sk(s)) }}%</span>
            </div>
            <div class="cp-stats">
              <div class="cp-stat">
                <span class="cp-stat-label">{{ tr('sources.statPage') }}</span>
                <span class="cp-stat-value">{{ collectStore.getState(sk(s)).page }}/{{ collectStore.getState(sk(s)).total || '?' }}</span>
              </div>
              <div class="cp-stat">
                <span class="cp-stat-label">{{ tr('sources.statVideoCount') }}</span>
                <span class="cp-stat-value">{{ collectStore.getState(sk(s)).videoCount }}</span>
              </div>
              <div class="cp-stat">
                <span class="cp-stat-label">{{ tr('sources.statSpeed') }}</span>
                <span class="cp-stat-value">{{ collectStore.speedStr(sk(s)) }}</span>
              </div>
              <div class="cp-stat">
                <span class="cp-stat-label">{{ tr('sources.statElapsed') }}</span>
                <span class="cp-stat-value">{{ collectStore.elapsedStr(sk(s)) }}</span>
              </div>
              <div class="cp-stat">
                <span class="cp-stat-label">{{ tr('sources.statEta') }}</span>
                <span class="cp-stat-value">{{ collectStore.etaStr(sk(s)) }}</span>
              </div>
            </div>
            <!-- 当前页视频标签 -->
            <div v-if="collectStore.getState(sk(s)).pageNames && collectStore.getState(sk(s)).pageNames.length > 0" class="cp-page-names">
              <span class="cp-page-names-label">{{ tr('sources.pageNamesHeader', { page: collectStore.getState(sk(s)).page, count: collectStore.getState(sk(s)).pageNames.length }) }}</span>
              <div class="cp-name-tags">
                <span v-for="(name, idx) in collectStore.getState(sk(s)).pageNames.slice(0, 15)" :key="idx" class="cp-name-tag">{{ name }}</span>
                <span v-if="collectStore.getState(sk(s)).pageNames.length > 15" class="cp-name-more">+{{ collectStore.getState(sk(s)).pageNames.length - 15 }} {{ tr('common.more') }}</span>
              </div>
            </div>
            <!-- 错误信息 -->
            <div v-if="collectStore.getState(sk(s)).error" class="cp-error">{{ collectStore.getState(sk(s)).error }}</div>
          </div>

          <!-- 2. 采集日志区 -->
          <div class="collect-log-panel">
            <div class="clog-title">{{ tr('sources.collectLogTitle', { count: collectStore.getState(sk(s)).log.length }) }}</div>
            <div v-if="collectStore.getState(sk(s)).log.length > 0" class="clog-list">
              <div v-for="(msg, idx) in collectStore.getState(sk(s)).log.slice(-15)" :key="idx" class="clog-line">{{ msg }}</div>
            </div>
            <div v-else class="clog-empty">{{ tr('sources.noLogs') }}</div>
          </div>

          <!-- 3. 后端状态明细 + 数据维护 -->
          <div class="source-ops-panel">
            <div class="sop-header">
              <span class="sop-title">
                <Icon name="database" :size="12" />
                {{ tr('sources.backendStatus') }}
              </span>
              <span v-if="collectStore.backendStatus[sk(s)]" class="sop-synced">
                {{ tr('sources.statusSyncedAt', { time: formatSyncTime(collectStore.backendStatus[sk(s)].syncedAt) }) }}
              </span>
              <button class="mini-btn" :disabled="statusSyncing === sk(s)" @click="refreshStatus(sk(s))">
                <Icon name="refresh" :size="11" />
                <span>{{ statusSyncing === sk(s) ? tr('common.loading') : tr('sources.refreshStatus') }}</span>
              </button>
            </div>

            <div v-if="collectStore.backendStatus[sk(s)]" class="sop-grid">
              <div class="sop-cell">
                <span class="sop-label">{{ tr('common.status') }}</span>
                <span class="sop-value" :class="{ on: collectStore.backendStatus[sk(s)].running }">
                  {{ collectStore.backendStatus[sk(s)].running
                    ? (collectStore.backendStatus[sk(s)].paused ? tr('sources.statusPaused') : tr('sources.statusRunning'))
                    : tr('sources.statusIdle') }}
                </span>
              </div>
              <div class="sop-cell">
                <span class="sop-label">{{ tr('sources.scheduleMode') }}</span>
                <span class="sop-value">{{ collectStore.backendStatus[sk(s)].mode ? modeLabel(collectStore.backendStatus[sk(s)].mode) : '--' }}</span>
              </div>
              <div class="sop-cell">
                <span class="sop-label">{{ tr('sources.statPage') }}</span>
                <span class="sop-value">{{ collectStore.backendStatus[sk(s)].page || '--' }}</span>
              </div>
              <div class="sop-cell">
                <span class="sop-label">{{ tr('sources.statProcessed') }}</span>
                <span class="sop-value">{{ collectStore.backendStatus[sk(s)].current }} / {{ collectStore.backendStatus[sk(s)].total || '?' }}</span>
              </div>
              <div class="sop-cell sop-cell-wide">
                <span class="sop-label">{{ tr('sources.lastLogLine') }}</span>
                <span class="sop-value sop-mono" :title="collectStore.backendStatus[sk(s)].log">{{ collectStore.backendStatus[sk(s)].log || '--' }}</span>
              </div>
            </div>
            <div v-else class="sop-empty">{{ tr('sources.statusNotLoaded') }}</div>

            <div class="sop-danger">
              <span class="sop-danger-label">
                <Icon name="alert-triangle" :size="12" />
                {{ tr('sources.dangerZone') }}
              </span>
              <button class="mini-btn danger" :disabled="truncating === sk(s)" @click="truncateSource(sk(s))">
                <Icon name="trash" :size="11" />
                <span>{{ truncating === sk(s) ? tr('sources.truncating') : tr('sources.truncateBtn') }}</span>
              </button>
            </div>
          </div>

          <!-- 4. 后台定时采集配置 -->
          <div class="schedule-config-panel">
            <div v-if="scheduleVisible.has(sk(s)) && scheduleForms.get(sk(s))" class="schedule-form-inline">
              <label class="sched-check">
                <input type="checkbox" v-model="scheduleForms.get(sk(s))!.enabled" />
                <span>{{ tr('sources.enableSchedule') }}</span>
              </label>
              <div v-if="scheduleForms.get(sk(s))!.enabled" class="sched-options">
                <div class="sched-row">
                  <label>{{ tr('sources.scheduleMode') }}</label>
                  <SelectDropdown v-model="scheduleForms.get(sk(s))!.mode" :options="[{ value: 'full', label: tr('sources.fullCollect') }, { value: 'incremental', label: tr('sources.incrementalCollect') }]" size="sm" />
                </div>
                <div class="sched-row">
                  <label>{{ tr('sources.intervalMinutes') }}</label>
                  <input type="number" v-model.number="scheduleForms.get(sk(s))!.interval_min" min="5" max="1440" class="sched-input" />
                </div>
              </div>
              <div class="sched-actions">
                <button class="mini-btn" @click="openSchedule(sk(s))">{{ tr('common.cancel') }}</button>
                <button class="mini-btn accent" @click="saveSchedule(sk(s))">{{ tr('sources.saveConfig') }}</button>
              </div>
            </div>
            <div v-else class="schedule-summary">
              <span class="schedule-status-label">
                <Icon name="clock" :size="12" />
                {{ tr('sources.backendScheduleLabel') }}: {{ scheduleStateFor(sk(s))?.enabled ? tr('sources.scheduleOnDetail', { mode: modeLabel(scheduleStateFor(sk(s))?.mode || 'incremental'), n: scheduleStateFor(sk(s))?.interval_min }) : tr('sources.scheduleOff') }}
              </span>
              <button class="mini-btn" @click="openSchedule(sk(s))">{{ tr('sources.configure') }}</button>
            </div>
          </div>

          <!-- 参数指南 -->
          <div class="params-section">
            <button class="params-toggle" @click="toggleParams(sk(s), s.api_url)">
              <Icon name="info" :size="12" />
              <span>{{ paramsDoc && expandedKey === sk(s) ? tr('sources.collapseParamsGuide') : tr('sources.viewParamsGuide') }}</span>
            </button>
            <div v-if="paramsDoc && expandedKey === sk(s)" class="params-content">
              <div v-if="paramsDocLoading" class="params-loading"><LoadingSpinner :label="tr('sources.loadingParams')" /></div>
              <template v-else>
                <div class="param-block">
                  <div class="param-block-header">
                    <span class="param-block-title">{{ tr('sources.url') }}</span>
                    <button class="copy-btn" @click="copyText(paramsDoc.base_url)">
                      <Icon name="copy" :size="11" /><span>{{ tr('common.copy') }}</span>
                    </button>
                  </div>
                  <code class="params-code">{{ paramsDoc.base_url }}</code>
                </div>
                <div v-if="paramsDoc.query_ac && paramsDoc.query_ac.length > 0" class="param-block">
                  <div class="param-block-header"><span class="param-block-title">{{ tr('sources.acTypes') }}</span></div>
                  <div v-for="(item, idx) in paramsDoc.query_ac" :key="'ac'+idx" class="param-row">
                    <code class="param-name">{{ item.name }}</code>
                    <span class="param-desc">{{ item.desc }}</span>
                    <button class="copy-btn-sm" @click="copyText(item.example)">{{ tr('common.copy') }}</button>
                  </div>
                </div>
                <div v-if="paramsDoc.query_common && paramsDoc.query_common.length > 0" class="param-block">
                  <div class="param-block-header"><span class="param-block-title">{{ tr('sources.commonParams') }}</span></div>
                  <div v-for="(item, idx) in paramsDoc.query_common" :key="'qc'+idx" class="param-row">
                    <code class="param-name">{{ item.name }}</code>
                    <span class="param-desc">{{ item.desc }}</span>
                    <button class="copy-btn-sm" @click="copyText(item.example)">{{ tr('common.copy') }}</button>
                  </div>
                </div>
                <div v-if="paramsDoc.query_advanced && paramsDoc.query_advanced.length > 0" class="param-block">
                  <div class="param-block-header"><span class="param-block-title">{{ tr('sources.advancedParams') }}</span></div>
                  <div v-for="(item, idx) in paramsDoc.query_advanced" :key="'qa'+idx" class="param-row">
                    <code class="param-name">{{ item.name }}</code>
                    <span class="param-desc">{{ item.desc }}</span>
                    <button class="copy-btn-sm" @click="copyText(item.example)">{{ tr('common.copy') }}</button>
                  </div>
                </div>
              </template>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- 新建/编辑弹窗 -->
    <Modal
      :model-value="showForm"
      :title="editing ? tr('sources.edit') : tr('sources.add')"
      width="520px"
      :show-footer="true"
      @update:model-value="(v: boolean) => !v && (showForm = false)"
    >
      <p class="modal-desc">{{ tr('sources.formDesc') }}</p>
      <div class="form-group">
        <label>{{ tr('sources.url') }} <span class="required">*</span></label>
        <input v-model="form.api_url" :placeholder="tr('sources.apiUrlPlaceholder')" />
      </div>
      <div v-if="form.api_url" class="auto-info">
        <span class="auto-label">{{ tr('sources.autoDetect') }}:</span>
        <code>{{ autoKey || tr('sources.enterValidUrl') }}</code>
      </div>
      <div class="form-group" style="display:flex;align-items:center;gap:8px">
        <label style="display:flex;align-items:center;gap:6px;cursor:pointer">
          <input type="checkbox" v-model="form.enabled" style="width:auto" />
          <span>{{ tr('sources.enableThisSource') }}</span>
        </label>
      </div>
      <div class="form-group">
        <label>{{ tr('sources.displayName') }} <span class="optional">{{ tr('sources.optional') }}</span></label>
        <input v-model="form.name" :placeholder="autoKey || tr('sources.autoUseSourceKey')" />
      </div>
      <button class="toggle-advanced" @click="showAdvanced = !showAdvanced">
        <span>{{ showAdvanced ? '▾' : '▸' }}</span>
        <span>{{ tr('sources.advancedOptions') }}</span>
      </button>
      <div v-if="showAdvanced" class="form-group">
        <label>{{ tr('sources.perPageLabel') }} <span class="optional">{{ tr('sources.perPageHint') }}</span></label>
        <input type="number" min="0" max="500" v-model.number="form.collect_limit" placeholder="50" />
      </div>
      <div v-if="showAdvanced" class="form-group">
        <label>{{ tr('sources.defaultHoursLabel') }}<span class="optional">{{ tr('sources.defaultHoursHint') }}</span></label>
        <input type="number" min="0" max="8760" v-model.number="form.collect_hours" placeholder="24" />
      </div>
      <template #footer>
        <Button variant="secondary" size="md" @click="showForm = false">{{ tr('common.cancel') }}</Button>
        <Button variant="primary" size="md" :disabled="!form.api_url" @click="save">{{ tr('common.save') }}</Button>
      </template>
    </Modal>

    <!-- 导入弹窗 -->
    <Modal
      :model-value="importDialogOpen"
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
        <input type="file" accept=".json,.json.gz,.json.br" ref="importFileInput" style="display:none" @change="onImportFileSelected" />
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

    <!-- 导出成功提示条 -->
    <MotionTransition preset="toast-bottom">
      <div v-if="exportResult" class="export-toast">
        <Icon name="check" :size="14" />
        <span class="export-toast-text">{{ tr('sources.exported') }} <strong>{{ exportResult.sourceKey }}</strong> {{ tr('sources.exportTo') }}:</span>
        <code class="export-toast-path" :title="exportResult.path">{{ exportResult.path }}</code>
        <button class="mini-btn accent" @click="openExportFolder">
          <Icon name="folder" :size="10" /><span>{{ tr('sources.openFolder') }}</span>
        </button>
        <button class="mini-btn" @click="dismissExport">
          <Icon name="x" :size="10" />
        </button>
      </div>
    </MotionTransition>
  </div>
</template>

<style scoped>

.sources-page {
  max-width: 1280px;
  margin: 0 auto;
  color: var(--text-primary);
  animation: cczj-fade-in-up 0.4s ease;
  padding-bottom: 40px;
}

/* === 页头 === */
.page-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-end;
  margin-bottom: 20px;
  gap: 20px;
}
.page-title h1 {
  font-size: 28px;
  font-weight: 700;
  margin: 0 0 6px;
}
.page-desc {
  font-size: 13px;
  color: var(--text-muted);
  margin: 0;
}
.add-btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 10px 20px;
  border-radius: 10px;
  border: none;
  background: var(--accent);
  color: var(--accent-contrast);
  cursor: pointer;
  font-size: 14px;
  font-weight: 600;
  transition: all 0.15s ease;
  white-space: nowrap;
}
.add-btn:hover {
  background: var(--accent-dim);
  transform: translateY(-1px);
  box-shadow: 0 4px 12px var(--accent-alpha-35);
}

/* === 顶部操作栏 === */
.toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 16px;
  padding: 12px 16px;
  background: rgba(255,255,255,0.03);
  border: 1px solid var(--border);
  border-radius: 12px;
  flex-wrap: wrap;
}
.toolbar-left {
  display: flex;
  align-items: center;
  gap: 10px;
  flex: 1;
  min-width: 0;
}
.toolbar-right {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}
.search-box {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 14px;
  background: rgba(255,255,255,0.05);
  border: 1px solid var(--border);
  border-radius: 10px;
  flex: 1;
  max-width: 360px;
  min-width: 0;
  color: var(--text-muted);
}
.search-box input {
  border: none;
  background: transparent;
  color: var(--text-primary);
  font-size: 13px;
  outline: none;
  flex: 1;
  min-width: 0;
}
.search-box input::placeholder { color: var(--text-muted); }
.search-clear {
  border: none;
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
  padding: 2px;
  display: flex;
}
.search-clear:hover { color: var(--text-primary); }
.filter-select {
  padding: 8px 14px;
  border-radius: 10px;
  border: 1px solid var(--border);
  background: rgba(255,255,255,0.05);
  color: var(--text-primary);
  font-size: 13px;
  cursor: pointer;
  outline: none;
}
.filter-select:focus { border-color: var(--accent); }
.tool-btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 9px 16px;
  border-radius: 10px;
  border: 1px solid var(--border);
  background: rgba(255,255,255,0.05);
  color: var(--text-secondary);
  cursor: pointer;
  font-size: 13px;
  font-weight: 500;
  transition: all 0.15s ease;
  white-space: nowrap;
}
.tool-btn:hover { background: rgba(255,255,255,0.08); color: var(--text-primary); }
.tool-btn.accent { border-color: var(--accent-alpha-35); color: var(--accent); }
.tool-btn.accent:hover { background: var(--accent-alpha-10); }

/* === 全局调度器条 === */
.scheduler-bar {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 16px;
  margin-bottom: 16px;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 10px;
  font-size: 13px;
}
.scheduler-bar.active { border-color: var(--accent-alpha-30); }
.scheduler-indicator {
  width: 8px; height: 8px; border-radius: 50%;
  background: var(--text-muted); flex-shrink: 0;
}
.scheduler-indicator.on {
  background: #4caf50;
  box-shadow: 0 0 6px #4caf50;
  animation: pulse 2s ease-in-out infinite;
}
@keyframes pulse {
  0%, 100% { box-shadow: 0 0 4px #4caf50; }
  50% { box-shadow: 0 0 12px #4caf50; }
}
.scheduler-label { font-weight: 600; color: var(--text-secondary); }
.scheduler-state { font-weight: 600; color: var(--accent); }
.scheduler-note { flex: 1; color: var(--text-muted); font-size: 12px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.scheduler-actions { display: flex; gap: 6px; flex-shrink: 0; }
.scheduler-config {
  display: flex; flex-direction: column; gap: 10px;
  margin: -8px 0 16px; padding: 12px 16px;
  background: var(--bg-card); border: 1px solid var(--accent-alpha-30);
  border-radius: 10px;
}
.sc-row { display: flex; align-items: center; gap: 16px; flex-wrap: wrap; }
.sc-field { display: flex; align-items: center; gap: 8px; font-size: 12px; color: var(--text-secondary); }
.sc-label { color: var(--text-muted); white-space: nowrap; }
.sc-value { min-width: 52px; color: var(--text-primary); font-weight: 600; }
.sc-input {
  width: 68px; padding: 5px 8px; border-radius: 6px;
  border: 1px solid var(--border); background: var(--bg-secondary);
  color: var(--text-primary); font-size: 12px; outline: none;
}
.sc-input:hover, .sc-input:focus { border-color: var(--accent); }
.sc-input:focus { box-shadow: 0 0 0 3px var(--accent-alpha-10); }
.sc-input::-webkit-inner-spin-button,
.sc-input::-webkit-outer-spin-button { -webkit-appearance: none; margin: 0; }
.sc-input[type='number'] { -moz-appearance: textfield; appearance: textfield; }
.sc-field input[type='range'] { accent-color: var(--accent); cursor: pointer; }
.sc-check {
  display: inline-flex; align-items: center; gap: 6px;
  font-size: 12px; color: var(--text-secondary); cursor: pointer;
}
.sc-check input { accent-color: var(--accent); cursor: pointer; }
.sc-hint { color: var(--text-muted); }
.mini-btn {
  display: inline-flex; align-items: center; gap: 4px;
  padding: 4px 10px; border-radius: 6px;
  border: 1px solid var(--border); background: var(--bg-secondary);
  color: var(--text-secondary); cursor: pointer; font-size: 11px; font-weight: 500;
  transition: all 0.15s ease;
}
.mini-btn:hover { background: var(--bg-hover); color: var(--text-primary); }
.mini-btn.accent { border-color: var(--accent-alpha-35); color: var(--accent); }
.mini-btn.accent:hover { background: var(--accent-alpha-10); }
.mini-btn.danger { border-color: var(--danger); color: var(--danger); }
.mini-btn.danger:hover { background: rgba(239, 83, 80, 0.1); }

/* === 加载/空 === */
.content-loader { padding: 60px 20px; }

/* === 卡片网格 === */
.card-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(380px, 1fr));
  gap: 16px;
}

@media (max-width: 480px) {
  .card-grid { grid-template-columns: 1fr; }
  .toolbar { flex-direction: column; align-items: stretch; }
  .toolbar-left { flex-direction: column; }
  .search-box { max-width: none; }
}

/* === 卡片 === */
.source-card {
  background: rgba(255,255,255,0.05);
  border: 1px solid var(--border);
  border-radius: 12px;
  transition: all 0.25s ease;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}
.source-card:hover {
  transform: translateY(-2px);
  box-shadow: 0 8px 24px rgba(0,0,0,0.2);
  border-color: rgba(255,255,255,0.12);
}
.source-card.expanded { border-color: var(--accent); box-shadow: 0 0 0 1px var(--accent-alpha-20), 0 8px 24px rgba(0,0,0,0.25); }
.source-card.running { border-color: var(--accent-alpha-30); }

/* === 卡片头部 === */
.card-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 16px 18px;
  cursor: pointer;
  user-select: none;
  transition: background 0.15s ease;
}
.card-header:hover { background: rgba(255,255,255,0.03); }
.card-header-left {
  display: flex;
  align-items: center;
  gap: 10px;
  flex: 1;
  min-width: 0;
}
.card-header-right {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}
.status-dot {
  width: 10px; height: 10px; border-radius: 50%;
  flex-shrink: 0;
  background: #757575;
}
.status-dot.running {
  background: #4caf50;
  box-shadow: 0 0 8px rgba(76, 175, 80, 0.6);
  animation: dotPulse 1.5s ease-in-out infinite;
}
@keyframes dotPulse {
  0%, 100% { box-shadow: 0 0 4px rgba(76, 175, 80, 0.4); }
  50% { box-shadow: 0 0 14px rgba(76, 175, 80, 0.8); }
}
.status-dot.paused { background: var(--warning); box-shadow: 0 0 6px var(--warning-alpha-10); }
.status-dot.error { background: var(--danger); box-shadow: 0 0 6px var(--danger-alpha-10); }
.status-dot.idle { background: var(--text-muted); }
.card-name {
  font-size: 15px; font-weight: 600; margin: 0;
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}
.mode-badge {
  padding: 3px 10px; border-radius: 12px;
  font-size: 11px; font-weight: 600; flex-shrink: 0;
}
.mode-badge.full { background: var(--accent-alpha-15); color: var(--accent); }
.mode-badge.incremental { background: var(--success-alpha-10); color: var(--success); }
.mode-badge.once { background: var(--warning-alpha-10); color: var(--warning-text); }
.schedule-mini {
  display: inline-flex; align-items: center; gap: 3px;
  font-size: 10px; color: var(--success);
  background: var(--success-alpha-10);
  padding: 2px 8px; border-radius: 10px;
  flex-shrink: 0;
}
.mini-progress-text {
  font-size: 12px; color: var(--accent); font-weight: 600;
  font-family: 'SF Mono', Consolas, monospace;
}
.expand-arrow {
  font-size: 13px; color: var(--text-muted);
  transition: transform 0.2s ease;
  width: 20px; text-align: center;
}

/* === 迷你进度条 === */
.mini-progress {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 0 18px 8px;
}
.mini-progress-track {
  flex: 1; height: 6px;
  background: rgba(255,255,255,0.08);
  border-radius: 3px; overflow: hidden;
}
.mini-progress-fill {
  height: 100%;
  background: linear-gradient(90deg, var(--accent), #818cf8);
  border-radius: 3px;
  transition: width 0.4s ease;
  animation: progressShimmer 2s ease-in-out infinite;
}
@keyframes progressShimmer {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.7; }
}
.mini-progress-pct {
  font-size: 12px; color: var(--accent); font-weight: 600;
  font-family: 'SF Mono', Consolas, monospace;
  min-width: 36px; text-align: right;
}

/* === 操作按钮行 === */
.card-actions {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 10px 18px 14px;
  border-top: 1px solid rgba(255,255,255,0.06);
}
.mode-select {
  padding: 5px 10px;
  border-radius: 6px;
  border: 1px solid var(--border);
  background: rgba(255,255,255,0.05);
  color: var(--text-primary);
  font-size: 12px;
  cursor: pointer;
  outline: none;
  font-weight: 500;
}
.mode-select:focus { border-color: var(--accent); }
.mode-select:disabled { opacity: 0.4; cursor: not-allowed; }
.hours-input-small {
  width: 48px; padding: 5px 8px;
  border-radius: 6px;
  border: 1px solid var(--border);
  background: rgba(255,255,255,0.05);
  color: var(--text-primary);
  font-size: 12px; text-align: center;
  font-family: 'SF Mono', Consolas, monospace;
  -moz-appearance: textfield;
}
.hours-input-small::-webkit-inner-spin-button,
.hours-input-small::-webkit-outer-spin-button {
  -webkit-appearance: none;
  margin: 0;
}
.hours-input-small:focus { border-color: var(--accent); outline: none; box-shadow: 0 0 0 2px var(--accent-alpha-10); }
.hours-input-small:disabled { opacity: 0.4; cursor: not-allowed; }
.hours-suffix { font-size: 11px; color: var(--text-muted); }
.action-spacer { flex: 1; }
.icon-btn {
  width: 34px; height: 34px;
  border-radius: 8px;
  border: 1px solid rgba(255,255,255,0.08);
  background: rgba(255,255,255,0.04);
  color: var(--text-secondary);
  cursor: pointer;
  display: inline-flex; align-items: center; justify-content: center;
  transition: all 0.15s ease;
  flex-shrink: 0;
}
.icon-btn:hover { background: rgba(255,255,255,0.1); color: var(--text-primary); }
.play-btn, .incr-btn, .sched-btn { color: var(--success); border-color: var(--success-alpha-10); }
.play-btn:hover, .incr-btn:hover, .sched-btn:hover { background: var(--success-alpha-10); border-color: var(--success); }
.pause-btn { color: var(--warning-text); border-color: var(--warning-alpha-10); }
.pause-btn:hover { background: var(--warning-alpha-10); border-color: var(--warning); }
.stop-btn, .del-btn { color: var(--danger); border-color: var(--danger-alpha-10); }
.stop-btn:hover, .del-btn:hover { background: var(--danger-alpha-10); border-color: var(--danger); }
.export-btn { color: var(--accent); border-color: var(--accent-alpha-25); }
.export-btn:hover { background: var(--accent-alpha-10); border-color: var(--accent); }
.edit-btn { color: var(--accent); border-color: var(--accent-alpha-25); }
.edit-btn:hover { background: var(--accent-alpha-10); border-color: var(--accent); }

/* === 展开内容 === */
.card-expanded {
  padding: 0 18px 16px;
  display: flex;
  flex-direction: column;
  gap: 14px;
  animation: expandIn 0.25s ease;
}
@keyframes expandIn {
  from { opacity: 0; transform: translateY(-6px); }
  to { opacity: 1; transform: translateY(0); }
}

/* === 1. 采集进度面板 === */
.collect-progress-panel {
  background: rgba(255,255,255,0.03);
  border: 1px solid var(--accent-alpha-15);
  border-radius: 10px;
  padding: 14px 16px;
}
.cp-progress-wrap {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 12px;
}
.cp-progress-track {
  flex: 1; height: 12px;
  background: rgba(255,255,255,0.08);
  border-radius: 6px; overflow: hidden;
}
.cp-progress-fill {
  height: 100%;
  background: linear-gradient(90deg, var(--accent), #818cf8, var(--accent));
  background-size: 200% 100%;
  border-radius: 6px;
  transition: width 0.3s ease;
  animation: gradientMove 2s ease-in-out infinite;
}
@keyframes gradientMove {
  0%, 100% { background-position: 0% 50%; }
  50% { background-position: 100% 50%; }
}
.cp-progress-pct {
  font-size: 16px; font-weight: 700; color: var(--accent);
  font-family: 'SF Mono', Consolas, monospace;
  min-width: 44px; text-align: right;
}
.cp-stats {
  display: flex;
  gap: 16px;
  flex-wrap: wrap;
  margin-bottom: 10px;
}
.cp-stat {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.cp-stat-label {
  font-size: 10px; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.3px;
}
.cp-stat-value {
  font-size: 13px; color: var(--text-primary); font-weight: 600;
  font-family: 'SF Mono', Consolas, monospace;
}
.cp-page-names {
  margin-top: 8px;
}
.cp-page-names-label {
  font-size: 11px; color: var(--text-muted); font-weight: 600; display: block; margin-bottom: 6px;
}
.cp-name-tags {
  display: flex; flex-wrap: wrap; gap: 4px;
}
.cp-name-tag {
  padding: 3px 10px; border-radius: 12px;
  background: rgba(255,255,255,0.05);
  border: 1px solid rgba(255,255,255,0.08);
  font-size: 11px; color: var(--text-secondary);
  max-width: 200px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.cp-name-more {
  padding: 3px 10px; border-radius: 12px;
  background: var(--accent-alpha-10);
  color: var(--accent); font-size: 11px; font-weight: 600;
}
.cp-error {
  margin-top: 10px; padding: 8px 12px;
  background: rgba(239,83,80,0.1);
  border: 1px solid rgba(239,83,80,0.3);
  border-radius: 8px;
  color: #ef5350; font-size: 12px; font-weight: 500;
}

/* === 2. 采集日志 === */
.collect-log-panel {
  background: rgba(255,255,255,0.02);
  border: 1px solid rgba(255,255,255,0.06);
  border-radius: 10px;
  padding: 12px 14px;
}
.clog-title {
  font-size: 12px; font-weight: 600; color: var(--text-secondary);
  margin-bottom: 8px;
}
.clog-list {
  max-height: 180px;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.clog-list::-webkit-scrollbar { width: 4px; }
.clog-list::-webkit-scrollbar-thumb { background: rgba(255,255,255,0.1); border-radius: 2px; }
.clog-line {
  font-size: 11px; color: var(--text-muted);
  font-family: 'SF Mono', Consolas, monospace;
  padding: 2px 0;
  line-height: 1.5;
}
.clog-empty {
  font-size: 12px; color: var(--text-muted); text-align: center;
  padding: 16px 0;
}

/* === 3. 后端状态与数据维护 === */
.source-ops-panel {
  background: rgba(255,255,255,0.02);
  border: 1px solid var(--border);
  border-radius: 10px;
  padding: 10px 12px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.sop-header {
  display: flex;
  align-items: center;
  gap: 8px;
}
.sop-title {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  font-size: 12px;
  font-weight: 600;
  color: var(--text-secondary);
}
.sop-synced {
  font-size: 11px;
  color: var(--text-muted);
  margin-left: auto;
}
.sop-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(120px, 1fr));
  gap: 6px 12px;
}
.sop-cell { display: flex; flex-direction: column; gap: 2px; }
.sop-cell-wide { grid-column: 1 / -1; }
.sop-label { font-size: 11px; color: var(--text-muted); }
.sop-value { font-size: 12px; color: var(--text-primary); }
.sop-value.on { color: var(--accent); }
.sop-mono {
  font-family: 'SF Mono', Consolas, monospace;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.sop-empty { font-size: 12px; color: var(--text-muted); }
.sop-danger {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding-top: 8px;
  border-top: 1px dashed var(--border);
}
.sop-danger-label {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  font-size: 11px;
  color: var(--danger);
}

/* === 4. 定时配置 === */
.schedule-config-panel {
  background: rgba(255,255,255,0.02);
  border: 1px solid rgba(76,175,80,0.2);
  border-radius: 10px;
  padding: 12px 14px;
}
.schedule-summary {
  display: flex; align-items: center; justify-content: space-between;
}
.schedule-status-label {
  display: inline-flex; align-items: center; gap: 6px;
  font-size: 12px; color: var(--text-secondary);
}
.schedule-form-inline {
  display: flex; flex-direction: column; gap: 10px;
}
.sched-check {
  display: inline-flex; align-items: center; gap: 8px;
  font-size: 13px; color: var(--text-primary); font-weight: 500;
  cursor: pointer;
}
.sched-check input[type='checkbox'] {
  -webkit-appearance: none; appearance: none;
  width: 18px; height: 18px;
  border: 1.5px solid var(--border-strong);
  border-radius: 5px;
  background: var(--bg-card);
  cursor: pointer;
  position: relative;
  transition: all 0.15s ease;
  flex-shrink: 0;
}
.sched-check input[type='checkbox']:hover { border-color: var(--accent); }
.sched-check input[type='checkbox']:checked { background: var(--accent); border-color: var(--accent); }
.sched-check input[type='checkbox']:checked::after {
  content: '';
  position: absolute;
  top: 3px; left: 5px;
  width: 4px; height: 8px;
  border: 2px solid var(--accent-contrast);
  border-top: 0; border-left: 0;
  transform: rotate(45deg);
}
.sched-options {
  display: flex; flex-direction: column; gap: 8px;
  padding: 10px 12px;
  background: rgba(255,255,255,0.03);
  border-radius: 8px;
  border: 1px solid var(--border);
}
.sched-row {
  display: flex; align-items: center; gap: 10px;
}
.sched-row label { font-size: 12px; color: var(--text-secondary); min-width: 70px; font-weight: 500; }
.sched-select {
  padding: 5px 32px 5px 12px; border-radius: 6px;
  border: 1.5px solid var(--border-strong); background: var(--bg-card);
  color: var(--text-primary); font-size: 12px; cursor: pointer; outline: none;
  -webkit-appearance: none; appearance: none;
  background-image: url("data:image/svg+xml;charset=UTF-8,%3csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='none' stroke='%23999' stroke-width='2'%3e%3cpolyline points='6 9 12 15 18 9'/%3e%3c/svg%3e");
  background-repeat: no-repeat;
  background-position: right 6px center;
  background-size: 14px;
  transition: all 0.15s ease;
}
.sched-select:hover { border-color: var(--accent); }
.sched-select:focus { border-color: var(--accent); box-shadow: 0 0 0 3px var(--accent-alpha-10); }
.sched-input {
  width: 80px; padding: 5px 12px; border-radius: 6px;
  border: 1.5px solid var(--border-strong); background: var(--bg-card);
  color: var(--text-primary); font-size: 12px; outline: none;
  text-align: center; font-family: 'SF Mono', Consolas, monospace;
  -webkit-appearance: none;
  transition: all 0.15s ease;
}
.sched-input::-webkit-inner-spin-button,
.sched-input::-webkit-outer-spin-button { -webkit-appearance: none; margin: 0; }
.sched-input:hover { border-color: var(--accent); }
.sched-input:focus { border-color: var(--accent); box-shadow: 0 0 0 3px var(--accent-alpha-10); }
.sched-actions {
  display: flex; gap: 8px; justify-content: flex-end; margin-top: 4px;
}

/* === 参数指南 === */
.params-section {
  border-top: 1px solid rgba(255,255,255,0.06);
  padding-top: 10px;
}
.params-toggle {
  display: inline-flex; align-items: center; gap: 6px;
  background: transparent; border: none;
  color: var(--text-muted); cursor: pointer; font-size: 12px;
  padding: 4px 0;
  transition: color 0.15s ease;
}
.params-toggle:hover { color: var(--accent); }
.params-content {
  margin-top: 10px;
  display: flex; flex-direction: column; gap: 10px;
}
.params-loading { padding: 16px; text-align: center; }
.param-block {
  background: rgba(255,255,255,0.03);
  border: 1px solid rgba(255,255,255,0.06);
  border-radius: 8px; padding: 10px 12px;
}
.param-block-header {
  display: flex; align-items: center; justify-content: space-between;
  margin-bottom: 6px;
}
.param-block-title {
  font-size: 11px; font-weight: 600; color: var(--accent);
  text-transform: uppercase; letter-spacing: 0.3px;
}
.params-code {
  display: block; padding: 6px 10px;
  background: rgba(255,255,255,0.05); border-radius: 6px;
  font-family: 'SF Mono', Consolas, monospace; font-size: 11px;
  color: var(--text-primary); word-break: break-all; line-height: 1.6;
  border: 1px solid var(--border);
}
.param-row {
  display: flex; align-items: center; gap: 8px;
  padding: 5px 0;
  border-bottom: 1px dashed rgba(255,255,255,0.06);
}
.param-row:last-child { border-bottom: none; }
.param-name {
  flex-shrink: 0; padding: 2px 8px;
  background: var(--accent-alpha-10); color: var(--accent);
  border-radius: 6px; font-size: 11px; font-weight: 600;
  font-family: 'SF Mono', Consolas, monospace;
  min-width: 70px; text-align: center;
}
.param-desc { flex: 1; font-size: 11px; color: var(--text-secondary); }
.copy-btn {
  display: inline-flex; align-items: center; gap: 4px;
  padding: 4px 10px; border-radius: 6px;
  border: 1px solid var(--accent-alpha-35);
  background: var(--accent-alpha-10); color: var(--accent);
  font-size: 11px; font-weight: 500; cursor: pointer;
  transition: all 0.15s ease; flex-shrink: 0;
}
.copy-btn:hover { background: var(--accent); color: var(--accent-contrast); }
.copy-btn-sm {
  padding: 3px 10px; border-radius: 6px; border: 1px solid var(--border);
  background: transparent; color: var(--text-muted); font-size: 11px;
  cursor: pointer; transition: all 0.15s ease; flex-shrink: 0;
}
.copy-btn-sm:hover { border-color: var(--accent); color: var(--accent); background: var(--accent-alpha-10); }

/* === 弹窗 === */
.modal-overlay {
  position: fixed; inset: 0;
  background: var(--overlay); backdrop-filter: blur(8px);
  display: flex; align-items: center; justify-content: center;
  z-index: 200; padding: 20px;
  animation: cczj-fade-in 0.2s ease;
}
.modal-content {
  background: var(--bg-card); padding: 24px;
  border-radius: 16px; width: 520px; max-width: 90vw;
  border: 1px solid var(--border);
  box-shadow: 0 20px 60px rgba(0,0,0,0.3);
  animation: slideUp 0.3s cubic-bezier(0.4, 0, 0.2, 1);
}
@keyframes slideUp {
  from { opacity: 0; transform: translateY(20px); }
  to { opacity: 1; transform: translateY(0); }
}
.modal-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 6px; }
.modal-header h3 { font-size: 18px; font-weight: 600; margin: 0; }
.close-btn {
  width: 32px; height: 32px; border-radius: 8px;
  border: 1px solid var(--border); background: transparent;
  color: var(--text-secondary); cursor: pointer;
  display: flex; align-items: center; justify-content: center;
  transition: all 0.15s ease;
}
.close-btn:hover { background: var(--bg-hover); color: var(--text-primary); }
.modal-desc { font-size: 13px; color: var(--text-muted); margin: 0 0 18px; }
.form-group { margin-bottom: 14px; }
.form-group label { display: block; font-size: 12px; color: var(--text-secondary); margin-bottom: 6px; font-weight: 500; }
.form-group input {
  width: 100%; padding: 10px 14px; border-radius: 10px;
  border: 1px solid var(--border); background: var(--bg-input);
  color: var(--text-primary); font-size: 13px; outline: none;
  transition: all 0.15s ease; box-sizing: border-box;
}
.form-group input:focus { border-color: var(--accent); box-shadow: 0 0 0 3px var(--accent-alpha-10); }
.form-group input::placeholder { color: var(--text-muted); }
.required { color: var(--danger); }
.optional { color: var(--text-muted); font-weight: normal; }
.auto-info {
  display: flex; align-items: center; gap: 8px;
  font-size: 12px; color: var(--accent);
  padding: 10px 12px; background: var(--accent-alpha-10);
  border-radius: 8px; margin-bottom: 14px;
}
.auto-info code { font-weight: 600; font-family: 'SF Mono', Consolas, monospace; }
.toggle-advanced {
  display: inline-flex; align-items: center; gap: 6px;
  background: transparent; border: none;
  color: var(--text-muted); cursor: pointer;
  font-size: 12px; padding: 6px 0; margin-bottom: 8px;
  transition: color 0.15s ease;
}
.toggle-advanced:hover { color: var(--accent); }
.form-actions { display: flex; gap: 10px; justify-content: flex-end; margin-top: 18px; }
.cancel-btn {
  padding: 10px 20px; border-radius: 8px;
  border: 1px solid var(--border); background: var(--bg-secondary);
  color: var(--text-secondary); cursor: pointer;
  font-size: 13px; transition: all 0.15s ease;
}
.cancel-btn:hover { background: var(--bg-hover); color: var(--text-primary); }
.save-btn {
  padding: 10px 22px; border-radius: 8px; border: none;
  background: var(--accent); color: var(--accent-contrast);
  cursor: pointer; font-size: 13px; font-weight: 600;
  transition: all 0.15s ease;
}
.save-btn:hover:not(:disabled) { background: var(--accent-dim); }
.save-btn:disabled { opacity: 0.4; cursor: not-allowed; }

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

/* === 导出成功提示条 === */
.export-toast {
  position: fixed;
  bottom: 24px;
  left: 50%;
  transform: translateX(-50%);
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 12px 18px;
  background: var(--bg-card);
  border: 1px solid var(--accent-alpha-30);
  border-radius: 12px;
  box-shadow: 0 8px 28px rgba(0, 0, 0, 0.25);
  z-index: 2000;
  color: var(--text-primary);
  font-size: 13px;
  max-width: 640px;
}
.export-toast-text {
  white-space: nowrap;
}
.export-toast-path {
  font-size: 11px;
  color: var(--accent);
  background: var(--accent-alpha-10);
  padding: 2px 8px;
  border-radius: 4px;
  max-width: 240px;
  font-family: 'SF Mono', Consolas, monospace;
}

</style>
