<script setup lang="ts">
defineOptions({ name: 'Sources' })
import { onMounted, ref, computed } from 'vue'
import { useSourceStore } from '../stores/source'
import { useCollectStore } from '../stores/collect'
import { useErrorStore } from '../stores/error'
import type { SourceScheduleItem } from '../stores/collect'
import { AddSource, UpdateSource, DeleteSource, GetSourceParamsDoc, ExportSource, ImportSourceFromBase64, OpenFolder } from '../../bindings/cczjVideo/app'
import Icon from '../components/Icon.vue'
import { Button, Modal, Tag, Spinner as LoadingSpinner, Empty as EmptyState, Select as SelectDropdown } from '../components/ui'
import { useConfirmStore } from '../stores/confirm'
import { extractDomainKey } from '../utils'

const errorStore = useErrorStore()

interface EditSource {
  source_key: string
  name: string
  api_url: string
  url_template?: string
  url_prefix?: string
  url_suffix?: string
  collect_limit?: number
  collect_hours?: number
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
const form = ref({ name: '', api_url: '', url_template: '', url_prefix: '', url_suffix: '', collect_limit: 0, collect_hours: 0 })
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

// === 生命周期 ===
onMounted(async () => {
  try {
    await Promise.all([
      sourceStore.loadSources().catch(e => {
        console.error('[Sources] loadSources failed:', e)
        errorStore.fromError('加载源站列表失败', e)
      }),
      collectStore.loadSchedule().catch(e => {
        console.error('[Sources] loadSchedule failed:', e)
      })
    ])
  } catch (e) {
    console.error('[Sources] onMounted init failed:', e)
  }
})

// === 添加/编辑弹窗 ===
const autoKey = computed(() => extractDomainKey(form.value.api_url))

function openAdd(): void {
  console.log('[Sources] openAdd called, showForm before:', showForm.value)
  editing.value = null
  form.value = { name: '', api_url: '', url_template: '', url_prefix: '', url_suffix: '', collect_limit: 50, collect_hours: 0 }
  showAdvanced.value = false
  showForm.value = true
  console.log('[Sources] openAdd done, showForm after:', showForm.value)
}

function openEdit(s: EditSource): void {
  editing.value = s.source_key
  form.value = {
    name: s.name,
    api_url: s.api_url,
    url_template: s.url_template || '',
    url_prefix: s.url_prefix || '',
    url_suffix: s.url_suffix || '',
    collect_limit: s.collect_limit ?? 0,
    collect_hours: s.collect_hours ?? 0,
  }
  showAdvanced.value = !!(s.url_template || s.collect_limit || s.collect_hours)
  showForm.value = true
}

async function save(): Promise<void> {
  const payload = {
    source_key: editing.value || '',
    name: form.value.name,
    api_url: form.value.api_url,
    url_template: form.value.url_template,
    url_prefix: form.value.url_prefix,
    url_suffix: form.value.url_suffix,
    collect_limit: Number(form.value.collect_limit) || 0,
    collect_hours: Number(form.value.collect_hours) || 0,
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
    title: '删除数据源',
    message: '确定删除此采集源？视频数据不会删除。',
    okText: '删除',
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
  errorStore.info('默认源已切换', `已将「${source.name}」设为默认数据源`)
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
  if (isRunning(key)) return '运行中'
  if (isPaused(key)) return '已暂停'
  if (hasError(key)) return '错误'
  return '空闲'
}

function modeLabel(m: string): string {
  switch (m) {
    case 'full': return '全量'
    case 'incremental': return '增量'
    case 'once': return '单次'
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
        <h1>采集源管理</h1>
        <p class="page-desc">只需提供 API 地址即可。高级参数和采集模式可针对每个源独立配置。</p>
      </div>
    </div>

    <!-- 顶部操作栏 -->
    <div class="toolbar">
      <div class="toolbar-left">
        <div class="search-box">
          <Icon name="search" :size="14" />
          <input v-model="search" placeholder="搜索名称/关键字..." />
          <button v-if="search" class="search-clear" @click="search = ''">
            <Icon name="close" :size="10" />
          </button>
        </div>
        <SelectDropdown v-model="statusFilter" :options="[{ value: 'all', label: '全部' }, { value: 'running', label: '运行中' }, { value: 'idle', label: '空闲' }, { value: 'error', label: '错误' }]" size="sm" />
      </div>
      <div class="toolbar-right">
        <Button variant="secondary" size="sm" @click="sourceStore.loadSources(); collectStore.loadSchedule()" title="刷新">
          <Icon name="refresh" :size="14" />
          <span>刷新</span>
        </Button>
        <Button variant="ghost" size="sm" @click="openImportDialog" title="导入">
          <Icon name="upload" :size="14" />
          <span>导入</span>
        </Button>
        <Button variant="primary" size="sm" @click="openAdd">
          <Icon name="plus" :size="14" />
          <span>添加源</span>
        </Button>
      </div>
    </div>

    <!-- 全局调度器状态条 -->
    <div v-if="collectStore.schedulerStatus" class="scheduler-bar" :class="{ active: collectStore.schedulerStatus.running }">
      <div class="scheduler-indicator" :class="{ on: collectStore.schedulerStatus.running }"></div>
      <span class="scheduler-label">后台调度</span>
      <span class="scheduler-state">{{ collectStore.schedulerStatus.running ? '运行中' : '已停止' }}</span>
      <span class="scheduler-note">{{ collectStore.schedulerStatus.note }}</span>
      <div class="scheduler-actions">
        <button v-if="collectStore.schedulerStatus.running" class="mini-btn danger" @click="collectStore.stopBackground()">
          <Icon name="stop" :size="10" /><span>停止</span>
        </button>
        <button v-else class="mini-btn accent" @click="collectStore.triggerNow()">
          <Icon name="play" :size="10" /><span>启动全量</span>
        </button>
        <button class="mini-btn" @click="collectStore.loadSchedule()">
          <Icon name="refresh" :size="10" /><span>刷新</span>
        </button>
      </div>
    </div>

    <!-- 加载中 -->
    <div v-if="sourceStore.loading" class="content-loader">
      <LoadingSpinner label="加载采集源..." />
    </div>

    <!-- 空状态 -->
    <div v-else-if="sourceStore.sources.length === 0">
      <EmptyState icon="📡" title="还没有采集源" description="添加第一个采集源，只需提供 API 地址即可开始">
        <Button variant="primary" size="sm" @click="openAdd">
          <Icon name="plus" :size="14" />
          <span>添加采集源</span>
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
            <span v-if="scheduleStateFor(sk(s))?.enabled" class="schedule-mini" :title="'每 ' + scheduleStateFor(sk(s))?.interval_min + ' 分钟定时'">
              <Icon name="clock" :size="10" />
              {{ scheduleStateFor(sk(s))?.interval_min }}分
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
          <SelectDropdown :model-value="getMode(sk(s))" :options="[{ value: 'full', label: '全量采集' }, { value: 'incremental', label: '增量采集' }, { value: 'once', label: '单次采集' }]" @update:model-value="(v: any) => { selectedModes.set(sk(s), String(v)) }" :disabled="isRunning(sk(s)) || isPaused(sk(s))" size="sm" />
          <input
            v-if="getMode(sk(s)) === 'incremental'"
            type="number"
            class="hours-input-small"
            min="1" max="168"
            :value="getHours(sk(s)) || s.collect_hours || 24"
            @input="(e: Event) => selectedHours.set(sk(s), Number((e.target as HTMLInputElement).value))"
            placeholder="h"
            :disabled="isRunning(sk(s)) || isPaused(sk(s))"
            title="回溯小时数"
          />
          <span v-if="getMode(sk(s)) === 'incremental'" class="hours-suffix">时</span>

          <div class="action-spacer"></div>

          <template v-if="isRunning(sk(s)) || isPaused(sk(s))">
            <button class="icon-btn pause-btn" @click="isPaused(sk(s)) ? collectStore.resume(sk(s)) : collectStore.pause(sk(s))" :title="isPaused(sk(s)) ? '恢复' : '暂停'">
              <Icon :name="isPaused(sk(s)) ? 'play' : 'pause'" :size="14" />
            </button>
            <button class="icon-btn stop-btn" @click="collectStore.stop(sk(s))" title="停止">
              <Icon name="stop" :size="14" />
            </button>
          </template>
          <template v-else>
            <button class="icon-btn play-btn" @click="startCollect(sk(s))" title="开始采集">
              <Icon name="play" :size="14" />
            </button>
            <button class="icon-btn incr-btn" @click="startCollect(sk(s), 'incremental', getHours(sk(s)) || s.collect_hours || 24)" title="增量采集">
              <Icon name="refresh" :size="14" />
            </button>
            <button class="icon-btn export-btn" @click="exportSource(sk(s))" title="导出">
              <Icon name="download" :size="14" />
            </button>
          </template>
          <button class="icon-btn sched-btn" @click="openSchedule(sk(s))" :title="scheduleStateFor(sk(s))?.enabled ? '定时已启用' : '定时配置'">
              <Icon name="clock" :size="14" />
            </button>
            <button class="icon-btn default-btn" :class="{ active: sk(s) === sourceStore.currentSourceKey }" @click="handleSetDefault(s)" :title="sk(s) === sourceStore.currentSourceKey ? '已设为默认' : '设为默认'">
              <Icon name="star" :size="14" />
            </button>
            <button class="icon-btn edit-btn" @click="openEdit({ source_key: sk(s), name: s.name, api_url: s.api_url, url_template: s.url_template, url_prefix: s.url_prefix, url_suffix: s.url_suffix, collect_limit: s.collect_limit, collect_hours: s.collect_hours })" title="编辑">
              <Icon name="edit" :size="14" />
            </button>
            <button class="icon-btn del-btn" @click="deleteSourceConfirm(sk(s))" title="删除">
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
                <span class="cp-stat-label">页数</span>
                <span class="cp-stat-value">{{ collectStore.getState(sk(s)).page }}/{{ collectStore.getState(sk(s)).total || '?' }}</span>
              </div>
              <div class="cp-stat">
                <span class="cp-stat-label">视频数</span>
                <span class="cp-stat-value">{{ collectStore.getState(sk(s)).videoCount }}</span>
              </div>
              <div class="cp-stat">
                <span class="cp-stat-label">速度</span>
                <span class="cp-stat-value">{{ collectStore.speedStr(sk(s)) }}</span>
              </div>
              <div class="cp-stat">
                <span class="cp-stat-label">耗时</span>
                <span class="cp-stat-value">{{ collectStore.elapsedStr(sk(s)) }}</span>
              </div>
              <div class="cp-stat">
                <span class="cp-stat-label">预估剩余</span>
                <span class="cp-stat-value">{{ collectStore.etaStr(sk(s)) }}</span>
              </div>
            </div>
            <!-- 当前页视频标签 -->
            <div v-if="collectStore.getState(sk(s)).pageNames && collectStore.getState(sk(s)).pageNames.length > 0" class="cp-page-names">
              <span class="cp-page-names-label">第 {{ collectStore.getState(sk(s)).page }} 页 · {{ collectStore.getState(sk(s)).pageNames.length }} 个</span>
              <div class="cp-name-tags">
                <span v-for="(name, idx) in collectStore.getState(sk(s)).pageNames.slice(0, 15)" :key="idx" class="cp-name-tag">{{ name }}</span>
                <span v-if="collectStore.getState(sk(s)).pageNames.length > 15" class="cp-name-more">+{{ collectStore.getState(sk(s)).pageNames.length - 15 }} 更多</span>
              </div>
            </div>
            <!-- 错误信息 -->
            <div v-if="collectStore.getState(sk(s)).error" class="cp-error">{{ collectStore.getState(sk(s)).error }}</div>
          </div>

          <!-- 2. 采集日志区 -->
          <div class="collect-log-panel">
            <div class="clog-title">采集日志 ({{ collectStore.getState(sk(s)).log.length }}条)</div>
            <div v-if="collectStore.getState(sk(s)).log.length > 0" class="clog-list">
              <div v-for="(msg, idx) in collectStore.getState(sk(s)).log.slice(-15)" :key="idx" class="clog-line">{{ msg }}</div>
            </div>
            <div v-else class="clog-empty">暂无日志</div>
          </div>

          <!-- 3. 后台定时采集配置 -->
          <div class="schedule-config-panel">
            <div v-if="scheduleVisible.has(sk(s)) && scheduleForms.get(sk(s))" class="schedule-form-inline">
              <label class="sched-check">
                <input type="checkbox" v-model="scheduleForms.get(sk(s))!.enabled" />
                <span>启用后台定时采集</span>
              </label>
              <div v-if="scheduleForms.get(sk(s))!.enabled" class="sched-options">
                <div class="sched-row">
                  <label>采集模式</label>
                  <SelectDropdown v-model="scheduleForms.get(sk(s))!.mode" :options="[{ value: 'full', label: '全量采集' }, { value: 'incremental', label: '增量采集' }]" size="sm" />
                </div>
                <div class="sched-row">
                  <label>间隔（分钟）</label>
                  <input type="number" v-model.number="scheduleForms.get(sk(s))!.interval_min" min="5" max="1440" class="sched-input" />
                </div>
              </div>
              <div class="sched-actions">
                <button class="mini-btn" @click="openSchedule(sk(s))">取消</button>
                <button class="mini-btn accent" @click="saveSchedule(sk(s))">保存配置</button>
              </div>
            </div>
            <div v-else class="schedule-summary">
              <span class="schedule-status-label">
                <Icon name="clock" :size="12" />
                后台定时: {{ scheduleStateFor(sk(s))?.enabled ? '已启用 (' + (scheduleStateFor(sk(s))?.mode === 'full' ? '全量' : '增量') + ', 每' + scheduleStateFor(sk(s))?.interval_min + '分)' : '未启用' }}
              </span>
              <button class="mini-btn" @click="openSchedule(sk(s))">配置</button>
            </div>
          </div>

          <!-- 参数指南 -->
          <div class="params-section">
            <button class="params-toggle" @click="toggleParams(sk(s), s.api_url)">
              <Icon name="info" :size="12" />
              <span>{{ paramsDoc && expandedKey === sk(s) ? '收起参数指南' : '查看参数指南' }}</span>
            </button>
            <div v-if="paramsDoc && expandedKey === sk(s)" class="params-content">
              <div v-if="paramsDocLoading" class="params-loading"><LoadingSpinner label="加载参数..." /></div>
              <template v-else>
                <div class="param-block">
                  <div class="param-block-header">
                    <span class="param-block-title">API 地址</span>
                    <button class="copy-btn" @click="copyText(paramsDoc.base_url)">
                      <Icon name="copy" :size="11" /><span>复制</span>
                    </button>
                  </div>
                  <code class="params-code">{{ paramsDoc.base_url }}</code>
                </div>
                <div v-if="paramsDoc.query_ac && paramsDoc.query_ac.length > 0" class="param-block">
                  <div class="param-block-header"><span class="param-block-title">ac 类型</span></div>
                  <div v-for="(item, idx) in paramsDoc.query_ac" :key="'ac'+idx" class="param-row">
                    <code class="param-name">{{ item.name }}</code>
                    <span class="param-desc">{{ item.desc }}</span>
                    <button class="copy-btn-sm" @click="copyText(item.example)">复制</button>
                  </div>
                </div>
                <div v-if="paramsDoc.query_common && paramsDoc.query_common.length > 0" class="param-block">
                  <div class="param-block-header"><span class="param-block-title">常用参数</span></div>
                  <div v-for="(item, idx) in paramsDoc.query_common" :key="'qc'+idx" class="param-row">
                    <code class="param-name">{{ item.name }}</code>
                    <span class="param-desc">{{ item.desc }}</span>
                    <button class="copy-btn-sm" @click="copyText(item.example)">复制</button>
                  </div>
                </div>
                <div v-if="paramsDoc.query_advanced && paramsDoc.query_advanced.length > 0" class="param-block">
                  <div class="param-block-header"><span class="param-block-title">高级参数</span></div>
                  <div v-for="(item, idx) in paramsDoc.query_advanced" :key="'qa'+idx" class="param-row">
                    <code class="param-name">{{ item.name }}</code>
                    <span class="param-desc">{{ item.desc }}</span>
                    <button class="copy-btn-sm" @click="copyText(item.example)">复制</button>
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
      :title="editing ? '编辑采集源' : '添加采集源'"
      width="520px"
      :show-footer="true"
      @update:model-value="(v: boolean) => !v && (showForm = false)"
    >
      <p class="modal-desc">只需提供 API 地址，系统会自动识别来源标识和名称。</p>
      <div class="form-group">
        <label>API 地址 <span class="required">*</span></label>
        <input v-model="form.api_url" placeholder="https://api.yyzy-tv.vip/inc/apijson.php 或 https://api.example.com/api.php/provide/vod/?ac=detail" />
      </div>
      <div v-if="form.api_url" class="auto-info">
        <span class="auto-label">自动识别:</span>
        <code>{{ autoKey || '(请输入有效URL)' }}</code>
      </div>
      <div class="form-group">
        <label>显示名称 <span class="optional">(可选)</span></label>
        <input v-model="form.name" :placeholder="autoKey || '自动使用来源标识'" />
      </div>
      <button class="toggle-advanced" @click="showAdvanced = !showAdvanced">
        <span>{{ showAdvanced ? '▾' : '▸' }}</span>
        <span>高级选项</span>
      </button>
      <div v-if="showAdvanced" class="form-group">
        <label>URL 模板 <span class="optional">(用于压缩 m3u8 地址)</span></label>
        <input v-model="form.url_template" placeholder="https://{host}.example.com/{prefix}/{path}/video/index.m3u8" />
      </div>
      <div v-if="showAdvanced" class="form-group">
        <label>单页条数 <span class="optional">(0=使用接口默认，建议 50-100)</span></label>
        <input type="number" min="0" max="500" v-model.number="form.collect_limit" placeholder="50" />
      </div>
      <div v-if="showAdvanced" class="form-group">
        <label>默认时间窗（小时）<span class="optional">(增量模式默认回溯小时数)</span></label>
        <input type="number" min="0" max="8760" v-model.number="form.collect_hours" placeholder="24" />
      </div>
      <template #footer>
        <Button variant="secondary" size="md" @click="showForm = false">取消</Button>
        <Button variant="primary" size="md" :disabled="!form.api_url" @click="save">保存</Button>
      </template>
    </Modal>

    <!-- 导入弹窗 -->
    <Modal
      :model-value="importDialogOpen"
      title="导入采集源"
      width="560px"
      :show-footer="true"
      @update:model-value="(v: boolean) => !v && closeImportDialog()"
    >
      <p class="modal-desc">支持拖入 .json / .json.br / .json.gz 文件，或点击下方区域选择文件。文件将由后端解析并添加到采集源列表。</p>
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
          <small>{{ (importFile.size / 1024).toFixed(1) }} KB · 点击可重新选择</small>
        </template>
        <template v-else>
          <Icon name="upload" :size="32" />
          <p class="import-hint-main">拖入 .json / .json.br / .json.gz 文件到此处</p>
          <p class="import-hint-sub">或点击此区域选择文件</p>
        </template>
      </div>
      <template #footer>
        <Button variant="secondary" size="md" @click="closeImportDialog">取消</Button>
        <Button variant="primary" size="md" :disabled="!importFile || importLoading" :loading="importLoading" @click="doImportSource">导入</Button>
      </template>
    </Modal>

    <!-- 导出成功提示条 -->
    <transition name="slide-up">
      <div v-if="exportResult" class="export-toast">
        <Icon name="check" :size="14" />
        <span class="export-toast-text">已导出 <strong>{{ exportResult.sourceKey }}</strong> 到:</span>
        <code class="export-toast-path" :title="exportResult.path">{{ exportResult.path }}</code>
        <button class="mini-btn accent" @click="openExportFolder">
          <Icon name="folder" :size="10" /><span>打开文件夹</span>
        </button>
        <button class="mini-btn" @click="dismissExport">
          <Icon name="x" :size="10" />
        </button>
      </div>
    </transition>
  </div>
</template>

<style scoped>
.sources-page {
  max-width: 1280px;
  margin: 0 auto;
  color: var(--text-primary);
  animation: fadeInUp 0.4s ease;
  padding-bottom: 40px;
}

@keyframes fadeInUp {
  from { opacity: 0; transform: translateY(10px); }
  to { opacity: 1; transform: translateY(0); }
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
.status-dot.paused { background: #e69500; box-shadow: 0 0 6px rgba(230, 149, 0, 0.4); }
.status-dot.error { background: #ef5350; box-shadow: 0 0 6px rgba(239, 83, 80, 0.4); }
.status-dot.idle { background: #757575; }
.card-name {
  font-size: 15px; font-weight: 600; margin: 0;
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}
.mode-badge {
  padding: 3px 10px; border-radius: 12px;
  font-size: 11px; font-weight: 600; flex-shrink: 0;
}
.mode-badge.full { background: rgba(99, 102, 241, 0.15); color: #818cf8; }
.mode-badge.incremental { background: rgba(76, 175, 80, 0.15); color: #4caf50; }
.mode-badge.once { background: rgba(255, 165, 0, 0.15); color: #e69500; }
.schedule-mini {
  display: inline-flex; align-items: center; gap: 3px;
  font-size: 10px; color: #4caf50;
  background: rgba(76, 175, 80, 0.1);
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
.play-btn { color: #4caf50; border-color: rgba(76,175,80,0.3); }
.play-btn:hover { background: rgba(76,175,80,0.15); border-color: #4caf50; }
.pause-btn { color: #e69500; border-color: rgba(230,149,0,0.3); }
.pause-btn:hover { background: rgba(230,149,0,0.15); border-color: #e69500; }
.stop-btn { color: #ef5350; border-color: rgba(239,83,80,0.3); }
.stop-btn:hover { background: rgba(239,83,80,0.15); border-color: #ef5350; }
.incr-btn { color: #4caf50; border-color: rgba(76,175,80,0.25); }
.incr-btn:hover { background: rgba(76,175,80,0.12); border-color: #4caf50; }
.export-btn { color: var(--accent); border-color: var(--accent-alpha-25); }
.export-btn:hover { background: var(--accent-alpha-10); border-color: var(--accent); }
.sched-btn { color: #4caf50; border-color: rgba(76,175,80,0.25); }
.sched-btn:hover { background: rgba(76,175,80,0.12); border-color: #4caf50; }
.edit-btn { color: var(--accent); border-color: var(--accent-alpha-25); }
.edit-btn:hover { background: var(--accent-alpha-10); border-color: var(--accent); }
.del-btn { color: #ef5350; border-color: rgba(239,83,80,0.25); }
.del-btn:hover { background: rgba(239,83,80,0.12); border-color: #ef5350; }

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

/* === 3. 定时配置 === */
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
  animation: fadeIn 0.2s ease;
}
@keyframes fadeIn {
  from { opacity: 0; }
  to { opacity: 1; }
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
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: 'SF Mono', Consolas, monospace;
}

.slide-up-enter-active,
.slide-up-leave-active {
  transition: opacity 0.25s ease, transform 0.25s ease;
}
.slide-up-enter-from,
.slide-up-leave-to {
  opacity: 0;
  transform: translateX(-50%) translateY(12px);
}
</style>