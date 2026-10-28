<script setup lang="ts">
import { computed, shallowRef, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button } from './ui'
import { appendResourceSample, type ResourceSample } from '../diagnostics/history'
import { TsCache } from '../utils/tsCache'
import { readAndroidMemoryState, openAndroidMemoryAccessSettings } from '../api/runtime'
import { useErrorStore } from '../stores/error'

const props = defineProps<{ metrics: Record<string, any> | null; paused: boolean }>()
const { t } = useI18n()
const history = shallowRef<ResourceSample[]>([])
const resource = computed(() => props.metrics?.resources || {})
const native = computed(() => resource.value.native_available === true)
const appOnly=computed(()=>resource.value.native_scope==='app_process')
const androidMemory=computed(()=>{void props.metrics;return readAndroidMemoryState()})
const attributed=computed(()=>androidMemory.value?.attributed?.available ? androidMemory.value.attributed : null)
const processRows=computed(()=>attributed.value?.processes?.map(row=>({...row,name:t(row.role==='renderer'?'diagnostics.androidRenderer':'diagnostics.appPss'),proportional_bytes:row.pss_bytes,working_set_bytes:row.rss_bytes}))||resource.value.processes||[])
const errors=useErrorStore()
const permissionCommands=computed(()=>{
  const name=androidMemory.value?.package_name
  return name?`adb shell pm grant ${name} android.permission.DUMP\nadb shell pm grant ${name} android.permission.PACKAGE_USAGE_STATS\nadb shell appops set ${name} GET_USAGE_STATS allow`:''
})
function requestUsageAccess():void {
  try{openAndroidMemoryAccessSettings()}catch(error){errors.fromError(t('diagnostics.androidPermissionFailed'),error,'diagnostics.memory-permission')}
}
async function copyPermissionCommands():Promise<void> {
  try{await navigator.clipboard.writeText(permissionCommands.value);errors.info(t('diagnostics.androidMemoryPermission'),t('logs.copied'))}
  catch(error){errors.fromError(t('logs.copyFailed'),error,'diagnostics.memory-permission')}
}
const memoryLabel=computed(()=>t(attributed.value?'diagnostics.androidWholeMemory':appOnly.value?'diagnostics.appPss':'diagnostics.treeCommit'))
const memoryValue=computed(()=>attributed.value?.pss_bytes??(!native.value?null:appOnly.value?resource.value.proportional_bytes:resource.value.tree_private_bytes))
const memoryNote=computed(()=>attributed.value?t('diagnostics.androidAttributedScope',{count:processRows.value.length}):appOnly.value?t('diagnostics.androidMemoryScope'):t('diagnostics.treeCommitNote',{count:resource.value.process_count||0}))
let historySource=''
watch(() => props.metrics, value => {
  if(!value)return
  const source=attributed.value?'android-attributed':appOnly.value?'android-main':'process-tree-commit'
  // Permission changes must not look like a memory leak or sudden reclamation.
  if(source!==historySource){history.value=[];historySource=source}
  history.value=appendResourceSample(history.value,value,attributed.value?.pss_bytes)
}, { immediate: true })

function bytes(value: number | null | undefined): string {
  if (value === undefined || value === null) return t('diagnostics.unavailable')
  if (value >= 1024**3) return `${(value / 1024**3).toFixed(2)} GB`
  return `${(value / 1024**2).toFixed(1)} MB`
}
function delta(field: 'heap' | 'committed'): string {
  if (history.value.length < 2) return '—'
  const first = history.value[0][field], last = history.value[history.value.length - 1][field]
  if (first === null || last === null) return '—'
  const difference = last - first
  return `${difference >= 0 ? '+' : '−'}${bytes(Math.abs(difference))}`
}
const seconds = computed(() => history.value.length < 2 ? 0 : Math.max(0, Math.round((history.value[history.value.length-1].at-history.value[0].at)/1000)))
const tsStats = computed(() => { void props.metrics; return TsCache.stats() })
const tsBytes = computed(() => tsStats.value.bytes)
const scale = computed(() => Math.max(1, ...history.value.flatMap(item => [item.heap, item.committed || 0])))
function points(field: 'heap' | 'committed'): string {
  const rows = history.value
  return rows.flatMap((row, i) => row[field]===null?[]:[`${(i / Math.max(1, rows.length - 1))*780+10},${110-(Number(row[field])/scale.value)*90}`]).join(' ')
}
function resetBaseline(): void {
  history.value = []
  if (props.metrics) history.value = appendResourceSample([], props.metrics,attributed.value?.pss_bytes)
}
defineExpose({ snapshot: () => {
  const ts = TsCache.stats()
  return { history: history.value, memory_series_source:historySource, segment_cache_bytes: ts.bytes, segment_cache_limit_bytes: ts.memoryLimitBytes,
    playback_cache_owners: ts.activePlayers, playback_cache_trim_pending: ts.idleTrimPending }
} })
</script>

<template>
  <section class="resource-monitor">
    <header class="resource-header">
      <div><span class="resource-kicker">{{ t('diagnostics.resourceEyebrow') }}</span><h3>{{ t('diagnostics.resourceTitle') }}</h3></div>
      <span class="resource-state" :class="{ paused }"><i></i>{{ t(paused ? 'diagnostics.monitorPaused' : 'diagnostics.monitorActive') }}</span>
    </header>
    <div class="resource-cards">
      <article class="resource-card prominent"><span>{{ memoryLabel }}</span><strong>{{ bytes(memoryValue) }}</strong><small>{{ memoryNote }}</small></article>
      <article class="resource-card"><span>{{ t(appOnly?'diagnostics.appRss':'diagnostics.rootWorkingSet') }}</span><strong>{{ bytes(native ? resource.working_set_bytes : null) }}</strong><small>{{ native ? t('diagnostics.physicalRatio', { percent: Number(resource.working_set_percent || 0).toFixed(2) }) : t('diagnostics.unavailable') }}</small></article>
      <article class="resource-card"><span>{{ t('diagnostics.goHeap') }}</span><strong>{{ bytes(resource.heap_alloc_bytes) }}</strong><small>{{ t('diagnostics.heapRetained', { size: bytes(resource.heap_retained_bytes) }) }}</small></article>
      <article class="resource-card"><span>{{ t('diagnostics.machineCPU') }}</span><strong>{{ native && resource.cpu_percent != null ? Number(resource.cpu_percent).toFixed(1) + '%' : '—' }}</strong><small>{{ t(appOnly?'diagnostics.appCpuNote':'diagnostics.machineCPUNote') }}</small></article>
    </div>
    <div v-if="appOnly" class="resource-strip">
      <span>{{ t('diagnostics.androidSystemPss') }} <b>{{ bytes(androidMemory?.available ? androidMemory.pss_bytes : null) }}</b></span>
      <span>{{ t('diagnostics.androidJavaHeap') }} <b>{{ bytes(androidMemory?.available ? androidMemory.java_heap_bytes : null) }}</b></span>
      <span>{{ t('diagnostics.androidNativeHeap') }} <b>{{ bytes(androidMemory?.available ? androidMemory.native_heap_bytes : null) }}</b></span>
      <span>{{ t('diagnostics.androidGraphics') }} <b>{{ bytes(androidMemory?.available ? androidMemory.graphics_bytes : null) }}</b></span>
      <span>{{ t('diagnostics.androidSwap') }} <b>{{ bytes(androidMemory?.available ? androidMemory.total_swap_bytes : null) }}</b></span>
      <span>{{ t('diagnostics.androidWholeMemory') }} <b>{{ bytes(androidMemory?.attributed?.available ? androidMemory.attributed.pss_bytes : null) }}</b></span>
      <span v-if="androidMemory">WebView <b>{{ androidMemory.webview_provider }} {{ androidMemory.webview_version }}</b></span>
    </div>
    <p v-if="appOnly" class="resource-note">{{ t('diagnostics.androidSystemMemoryNote') }}</p>
    <details v-if="appOnly&&androidMemory" class="resource-note">
      <summary>{{ t('diagnostics.androidMemoryPermission') }} · {{ t(androidMemory.dump_permission&&androidMemory.usage_access?'diagnostics.androidMemoryAuthorized':'diagnostics.androidMemoryUnauthorized') }}</summary>
      <p>{{ t('diagnostics.androidMemoryPermissionHint') }}</p>
      <template v-if="!androidMemory.dump_permission||!androidMemory.usage_access">
        <pre class="cczj-overflow-x-auto">{{ permissionCommands }}</pre>
        <Button v-if="permissionCommands" variant="secondary" size="sm" @click="copyPermissionCommands">{{ t('diagnostics.androidCopyPermission') }}</Button>
        <Button v-if="!androidMemory.usage_access" variant="secondary" size="sm" @click="requestUsageAccess">{{ t('diagnostics.androidOpenUsageAccess') }}</Button>
      </template>
      <p v-if="androidMemory.attributed&&!androidMemory.attributed.available">{{ t('diagnostics.androidMemorySamplingFailed') }} · {{ androidMemory.attributed.error_code }}</p>
      <p v-for="process in androidMemory.attributed?.processes||[]" :key="process.pid">{{ t(process.role==='renderer'?'diagnostics.androidRenderer':'diagnostics.appPss') }} · PID {{ process.pid }} · {{ bytes(process.pss_bytes) }}</p>
    </details>
    <div class="resource-strip">
      <span>{{ t('diagnostics.systemRAM') }} <b>{{ resource.system_total_bytes ? Number(resource.system_memory_percent).toFixed(1) + '%' : '—' }}</b></span>
      <span>{{ t('diagnostics.systemFree') }} <b>{{ bytes(resource.system_total_bytes ? resource.system_available_bytes : null) }}</b></span>
      <span v-if="metrics?.network_budget">{{ t('diagnostics.networkActive') }} <b>{{ metrics.network_budget.active }}</b></span>
      <span v-if="metrics?.network_budget">{{ t('diagnostics.networkWaiting') }} <b>{{ metrics.network_budget.waiting }}</b></span>
      <span v-if="metrics?.network_budget">{{ t('diagnostics.networkRejected') }} <b>{{ metrics.network_budget.rejected }}</b></span>
      <span>{{ t('diagnostics.goroutines') }} <b>{{ metrics?.goroutines ?? '—' }}</b></span>
      <span>{{ t(appOnly?'diagnostics.fileDescriptors':'diagnostics.handles') }} <b>{{ native ? (appOnly ? resource.file_descriptors ?? '—' : resource.handles) : '—' }}</b></span>
      <span>{{ t('diagnostics.cacheTs') }} <b>{{ bytes(tsBytes) }} / {{ bytes(tsStats.memoryLimitBytes) }}</b></span>
    </div>
    <div class="resource-chart-header"><div><h4>{{ t('diagnostics.resourceTrend') }}</h4><small>{{ t('diagnostics.observation', { seconds, count: history.length }) }}</small></div><Button variant="secondary" size="sm" @click="resetBaseline">{{ t('diagnostics.resetBaseline') }}</Button></div>
    <svg class="resource-chart" viewBox="0 0 800 120" role="img" :aria-label="t('diagnostics.resourceTrend')">
      <path d="M10 20H790 M10 65H790 M10 110H790" class="chart-grid" />
      <polyline :points="points('committed')" class="chart-commit" />
      <polyline :points="points('heap')" class="chart-heap" />
    </svg>
    <div class="resource-legend"><span class="legend-commit">{{ memoryLabel }} <b>{{ delta('committed') }}</b></span><span class="legend-heap">{{ t('diagnostics.goHeap') }} <b>{{ delta('heap') }}</b></span><small>{{ t('diagnostics.scaleMax', { size: bytes(scale) }) }}</small></div>
    <p class="resource-note">{{ appOnly?memoryNote:t('diagnostics.resourceNote') }}</p>
    <p class="resource-note">{{ t('diagnostics.resourceTrendNote') }}</p>
    <p v-for="note in resource.notes || []" :key="note" class="resource-note unavailable-note">{{ note }}</p>
    <details v-if="native" class="process-details"><summary>{{ t('diagnostics.processDetails', { count: processRows.length }) }}</summary><div class="process-table-scroll"><table><thead><tr><th>{{ t('common.name') }}</th><th>PID</th><th>{{ t(appOnly?'diagnostics.appPss':'diagnostics.privateCommit') }}</th><th>{{ t(appOnly?'diagnostics.appRss':'diagnostics.rootWorkingSet') }}</th><th>{{ t(appOnly?'diagnostics.fileDescriptors':'diagnostics.handles') }}</th></tr></thead><tbody><tr v-for="row in processRows" :key="row.pid"><td>{{ row.name }}</td><td>{{ row.pid }}</td><td>{{ bytes(appOnly?row.proportional_bytes:row.private_bytes) }}</td><td>{{ bytes(row.working_set_bytes) }}</td><td>{{ appOnly?row.file_descriptors ?? '—':row.handles }}</td></tr></tbody></table></div></details>
  </section>
</template>

<style scoped>
.resource-monitor{container-type:inline-size;container-name:resource-monitor;background:var(--bg-card);@apply cczj-border;border-top:3px solid var(--accent);@apply cczj-rounded-lg;padding:22px;@apply cczj-text-primary}
.resource-header,.resource-chart-header{@apply cczj-flex cczj-items-center cczj-justify-between cczj-gap-8}.resource-header h3{margin:5px 0 20px;font-size:1.2rem}.resource-kicker{font-size:.7rem;letter-spacing:.12em;@apply cczj-text-muted}
.resource-state{@apply cczj-flex cczj-gap-3 cczj-items-center;font-size:.76rem;@apply cczj-whitespace-nowrap cczj-text-success}.resource-state i{width:6px;@apply cczj-h-1.5 cczj-rounded-50;background:currentColor}.resource-state.paused{@apply cczj-text-muted}
.resource-cards{@apply cczj-grid;grid-template-columns:repeat(4,minmax(0,1fr));@apply cczj-gap-6}.resource-card{@apply cczj-flex cczj-flex-col;gap:9px;background:var(--bg-secondary);@apply cczj-border cczj-rounded-md cczj-p-8}.resource-card.prominent{@apply cczj-border-primary}.resource-card span{font-size:.76rem;@apply cczj-text-secondary}.resource-card strong{font-size:1.65rem;font-variant-numeric:tabular-nums;letter-spacing:-.04em}.resource-card small{font-size:.72rem;@apply cczj-text-muted cczj-leading-normal}
.resource-strip{@apply cczj-flex cczj-flex-wrap;gap:12px 24px;padding:18px 0;@apply cczj-border-bottom;font-size:.76rem;@apply cczj-text-muted}.resource-strip b{@apply cczj-text-primary cczj-ml-3;font-variant-numeric:tabular-nums}
.resource-chart-header{margin-top:18px}.resource-chart-header h4{margin:0 0 4px;font-size:.9rem}.resource-chart-header small{@apply cczj-text-muted;font-size:.72rem}.resource-chart{@apply cczj-w-full;height:130px;@apply cczj-mt-5}.chart-grid{stroke:var(--border);fill:none;stroke-dasharray:3 5}.chart-commit,.chart-heap{fill:none;stroke-width:2;vector-effect:non-scaling-stroke}.chart-commit{stroke:var(--accent)}.chart-heap{stroke:var(--success)}
.resource-legend{@apply cczj-flex cczj-gap-10 cczj-items-center;font-size:.75rem;@apply cczj-flex-wrap}.legend-commit{@apply cczj-text-accent}.legend-heap{@apply cczj-text-success}.resource-legend b{@apply cczj-ml-4;font-variant-numeric:tabular-nums}.resource-legend small{@apply cczj-ml-auto cczj-text-muted}.resource-note{font-size:.74rem;line-height:1.6;@apply cczj-text-muted;margin:12px 0 0}.unavailable-note{@apply cczj-text-warning}
.process-details{@apply cczj-mt-8;font-size:.76rem}.process-details summary{@apply cczj-cursor-pointer;padding:8px 0}.process-table-scroll{@apply cczj-overflow-x-auto;}table{@apply cczj-w-full;border-collapse:collapse;@apply cczj-text-right cczj-whitespace-nowrap}td,th{padding:9px;@apply cczj-border-bottom}td:first-child,th:first-child{@apply cczj-text-left}th{@apply cczj-text-muted cczj-font-medium}
@container resource-monitor (max-width:700px){.resource-cards{grid-template-columns:repeat(2,minmax(0,1fr))}.resource-monitor{@apply cczj-p-8}.resource-card strong{font-size:1.4rem}}@container resource-monitor (max-width:350px){.resource-cards{grid-template-columns:1fr}.resource-header{@apply cczj-items-start}.resource-state{font-size:.68rem}}
</style>
