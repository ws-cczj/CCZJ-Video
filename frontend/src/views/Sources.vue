<script setup lang="ts">
defineOptions({ name: 'Sources' })
import { onMounted, onActivated, onDeactivated, onBeforeUnmount, ref, computed } from 'vue'
import { useSourceStore } from '../stores/source'
import { useCollectStore } from '../stores/collect'
import { useErrorStore } from '../stores/error'
import { usePluginStore } from '../stores/plugins'
import type { SourceScheduleItem } from '../stores/collect'
import { DeleteSource, ExportSource, GetSourceParamsDoc, ProbeSource, ProbeSources, SourceProbeTimeline } from '../api/app'
import Icon from '../components/Icon.vue'
import { Button, MotionTransition, Spinner as LoadingSpinner, Empty as EmptyState, Select as SelectDropdown } from '../components/ui'
import { useConfirmStore } from '../stores/confirm'
import { tr } from '../locales'
import SourceCard from './sources/SourceCard.vue'
import SourceFormModal from './sources/SourceFormModal.vue'
import SourceImportModal from './sources/SourceImportModal.vue'
import SourceExportToast from './sources/SourceExportToast.vue'
import SourceGlobalSchedule from './sources/SourceGlobalSchedule.vue'
import { useSourceDisplay, type EditSource, type ExportResult, type ParamsDoc, type ScheduleForm } from './sources/useSourceDisplay'

/**
 * 采集源页面。
 *
 * 页面这一层只留"决定权"：过滤、每张卡的展开态、探测的触发与定时器、
 * 以及弹窗开合。卡片本身（sources/SourceCard.vue）和它的面板只读共享派生
 * （sources/useSourceDisplay.ts），要做事就往上抛事件——
 * 特别是探测：请求只从这一页发，Go 侧每源 5 分钟的闸门才不会被打穿，
 * 所以子组件不会在挂载时自己探一次。
 */
const errorStore = useErrorStore()
const sourceStore = useSourceStore()
const collectStore = useCollectStore()
const confirmStore = useConfirmStore()
const pluginStore = usePluginStore()
const { sk, isRunning, isPaused, hasError, sourceName } = useSourceDisplay()

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
// 按 source_key 记，不按序号：一次只展开一张卡，卡片重排也不会串位。
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
const scheduleForms = ref<Map<string, ScheduleForm>>(new Map())

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

// === 生命周期 ===
// 这一页是源列表唯一的写入方，每次 loadSources 都带 force：刚改完库要读回真实状态，
// 吃 store 的会话缓存会把"新增了一个源"显示成没生效。其他页不带 force，只借 currentSourceKey。
onMounted(async () => {
  try {
    await Promise.all([
      sourceStore.loadSources(true).catch(e => {
        console.error('[Sources] loadSources failed:', e)
        errorStore.fromError(tr('errors.loadSourcesFailed'), e)
      }),
      collectStore.loadSchedule().catch(e => {
        console.error('[Sources] loadSchedule failed:', e)
      }),
      // 采集适配下拉要等扩展包注册表才有内容；读不到就只显示内置适配，不打扰用户。
      pluginStore.ensureLoaded().catch(() => { })
    ])
  } catch (e) {
    console.error('[Sources] onMounted init failed:', e)
  }
})

// 工具栏刷新：源列表 + 调度器 + 各源后端状态（后台采集时前端事件会漏）
async function refreshAll(): Promise<void> {
  await Promise.all([
    sourceStore.loadSources(true).catch(() => { }),
    collectStore.loadSchedule().catch(() => { }),
  ])
  await collectStore.syncAllFromBackend(sourceStore.sources.map(sk))
}

// === 添加/编辑弹窗 ===
// 表单与保存链路在 sources/SourceFormModal.vue，这里只决定"给谁开窗"。
const showForm = ref(false)
const editingSource = ref<EditSource | null>(null)

function openAdd(): void {
  console.log('[Sources] openAdd called, showForm before:', showForm.value)
  editingSource.value = null
  showForm.value = true
  console.log('[Sources] openAdd done, showForm after:', showForm.value)
}

function openEdit(s: EditSource): void {
  editingSource.value = s
  showForm.value = true
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
  await sourceStore.loadSources(true)
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
const exportResult = ref<ExportResult | null>(null)

async function exportSource(key: string): Promise<void> {
  try {
    const fpath = await ExportSource(key) as string
    exportResult.value = { path: fpath, sourceKey: key }
  } catch (e) {
    console.error('export failed', e)
  }
}

function dismissExport(): void {
  exportResult.value = null
}

const importDialogOpen = ref(false)

function openImportDialog(): void {
  importDialogOpen.value = true
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

// === 采集源连通性探测 ===
// 探测戳的就是这一页在管理的源接口，所以入口和流量都只挂在这一页上：
// 进入时探一轮，之后每分钟补一轮，离开页面立刻停表。闸门在 Go 侧，
// 每个源最多 5 分钟一次，界面上连点不会变成对源站的压力。
const probingKey = ref('')
const probingAll = ref(false)
const autoProbing = ref(false)
const probes = ref<Record<string, Record<string, any>>>({})
const timelines = ref<Record<string, Record<string, any>[]>>({})
let probeTimer: ReturnType<typeof setInterval> | null = null

// 60 秒一跳：点阵要按分钟走格，而真正发不发请求由 Go 侧的 5 分钟闸门决定。
const PROBE_TICK_MS = 60 * 1000

function probeOf(key: string): Record<string, any> | null {
  return probes.value[key] || null
}

function rememberProbes(list: Record<string, any>[]): void {
  const next = { ...probes.value }
  for (const probe of list) {
    if (probe && probe.source_key) next[probe.source_key] = probe
  }
  probes.value = next
}

function slotsOf(key: string): Record<string, any>[] {
  return timelines.value[key] || []
}

// 冷却中的源额外挂一个文字提示：点阵上看不出"刚刚想探但被闸门挡了"，
// 只留一个灰点会被读成这一页没打开过。
function coolingText(key: string): string {
  const probe = probeOf(key)
  if (!probe || !probe.skipped) return ''
  return tr('sources.probeSkipped', { sec: Number(probe.retry_after_sec) || 0 })
}

async function loadTimeline(): Promise<void> {
  try {
    const list = ((await SourceProbeTimeline()) as unknown as Record<string, any>[]) || []
    const next: Record<string, Record<string, any>[]> = {}
    for (const timeline of list) {
      if (timeline && timeline.source_key) next[timeline.source_key] = timeline.slots || []
    }
    timelines.value = next
  } catch (e: any) {
    console.error('[Sources] 探测历史读取失败', e)
  }
}

async function autoProbe(): Promise<void> {
  if (autoProbing.value || probingAll.value) return
  autoProbing.value = true
  try {
    const list = ((await ProbeSources()) as unknown as Record<string, any>[]) || []
    rememberProbes(list)
  } catch (e: any) {
    console.error('[Sources] 自动探测失败', e)
  } finally {
    autoProbing.value = false
    await loadTimeline()
  }
}

function startProbeLoop(): void {
  stopProbeLoop()
  void autoProbe()
  probeTimer = setInterval(() => void autoProbe(), PROBE_TICK_MS)
}

function stopProbeLoop(): void {
  if (probeTimer) {
    clearInterval(probeTimer)
    probeTimer = null
  }
}

async function probeOne(key: string): Promise<void> {
  if (probingKey.value || probingAll.value) return
  probingKey.value = key
  try {
    const probe = (await ProbeSource(key)) as unknown as Record<string, any>
    if (probe) rememberProbes([probe])
    if (probe?.skipped) {
      errorStore.info(
        tr('sources.probeCooling', { name: sourceName(key) }),
        tr('sources.probeCoolingMsg', { sec: Number(probe.retry_after_sec) || 0 }),
        '',
        'Sources.probeOne',
      )
    }
    await loadTimeline()
  } catch (e: any) {
    errorStore.fromError(tr('sources.probeFailed'), e, 'Sources.probeOne')
  } finally {
    probingKey.value = ''
  }
}

async function probeAll(): Promise<void> {
  if (probingAll.value || autoProbing.value) return
  probingAll.value = true
  try {
    const list = ((await ProbeSources()) as unknown as Record<string, any>[]) || []
    rememberProbes(list)
  } catch (e: any) {
    errorStore.fromError(tr('sources.probeFailed'), e, 'Sources.probeAll')
  } finally {
    probingAll.value = false
    await loadTimeline()
  }
}

onActivated(startProbeLoop)
onDeactivated(stopProbeLoop)
onBeforeUnmount(stopProbeLoop)
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
        <Button variant="secondary" size="sm" :loading="probingAll" @click="probeAll" :title="tr('sources.probeNote')">
          <Icon name="globe" :size="14" />
          <span>{{ tr('sources.probeAll') }}</span>
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

    <!-- 全局调度器状态条 + 全局调度参数 -->
    <SourceGlobalSchedule />

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
      <SourceCard
        v-for="s in filteredSources"
        :key="sk(s)"
        :source="s"
        :source-key="sk(s)"
        :expanded="expandedKey === sk(s)"
        :mode="getMode(sk(s))"
        :hours="getHours(sk(s))"
        :slots="slotsOf(sk(s))"
        :cooling="coolingText(sk(s))"
        :probe-busy="probingKey === sk(s) || probingAll"
        :is-default="sk(s) === sourceStore.currentSourceKey"
        :schedule-visible="scheduleVisible.has(sk(s))"
        :schedule-form="scheduleForms.get(sk(s)) || null"
        :doc="paramsDoc"
        :doc-loading="paramsDocLoading"
        @toggle="toggleExpand(sk(s))"
        @update-mode="(m: string) => { selectedModes.set(sk(s), m) }"
        @update-hours="(h: number) => { selectedHours.set(sk(s), h) }"
        @collect="startCollect(sk(s))"
        @collect-incremental="startCollect(sk(s), 'incremental', getHours(sk(s)) || s.collect_hours || 24)"
        @probe="probeOne(sk(s))"
        @export="exportSource(sk(s))"
        @set-default="handleSetDefault(s)"
        @edit="openEdit({ source_key: sk(s), name: s.name, api_url: s.api_url, collect_limit: s.collect_limit, collect_hours: s.collect_hours, strategy_config: s.strategy_config })"
        @delete="deleteSourceConfirm(sk(s))"
        @schedule-toggle="openSchedule(sk(s))"
        @schedule-save="saveSchedule(sk(s))"
        @params-toggle="toggleParams(sk(s), s.api_url)"
      />
    </div>

    <!-- 新建/编辑弹窗 -->
    <SourceFormModal v-model="showForm" :source="editingSource" />

    <!-- 导入弹窗 -->
    <SourceImportModal v-model="importDialogOpen" />

    <!-- 导出成功提示条 -->
    <MotionTransition preset="toast-bottom">
      <SourceExportToast v-if="exportResult" :result="exportResult" @dismiss="dismissExport" />
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

/* === 小按钮 === */
/* 卡片里的两块面板、全局调度条和导出提示条共用这一套小按钮：规则只留在页面这一份，
   用 :deep 下发给子组件（子组件根节点带着页面的 scope id，选得到），免得各抄一份。 */
:deep(.mini-btn) {
  display: inline-flex; align-items: center; gap: 4px;
  padding: 4px 10px; border-radius: 6px;
  border: 1px solid var(--border); background: var(--bg-secondary);
  color: var(--text-secondary); cursor: pointer; font-size: 11px; font-weight: 500;
  transition: all 0.15s ease;
}
:deep(.mini-btn:hover) { background: var(--bg-hover); color: var(--text-primary); }
:deep(.mini-btn.accent) { border-color: var(--accent-alpha-35); color: var(--accent); }
:deep(.mini-btn.accent:hover) { background: var(--accent-alpha-10); }
:deep(.mini-btn.danger) { border-color: var(--danger); color: var(--danger); }
:deep(.mini-btn.danger:hover) { background: rgba(239, 83, 80, 0.1); }

/* === 加载/空 === */
.content-loader { padding: 60px 20px; }

/* === 卡片网格 === */
.card-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(380px, 1fr));
  gap: 16px;
  align-items: start;
}

@media (max-width: 480px) {
  .card-grid { grid-template-columns: 1fr; }
  .toolbar { flex-direction: column; align-items: stretch; }
  .toolbar-left { flex-direction: column; }
  .search-box { max-width: none; }
}

/* === 弹窗外壳（拆分前就已经是没人用的历史规则） === */
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

</style>
