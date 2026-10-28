<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from './Icon.vue'
import { Button, Modal, Segment as ChoiceSegment } from './ui'
import LogRecordDetails from './LogRecordDetails.vue'
import { GetLogContent, OpenFileInExplorer } from '../api/app'
import { revealDiagnosticExport, revealLogDirectory } from '../platform/diagnosticFiles'
import { presentationProfile } from '../platform/presentation'
import { useViewActivity } from '../composables/useViewActivity'
import { useConfirmStore } from '../stores/confirm'
import { useErrorStore } from '../stores/error'
import {
  ALL_LEVELS,
  ALL_ORIGINS,
  formatLogTime,
  logRecordToLine,
  useLogsStore,
  type LogOrigin,
  type LogRecord,
} from '../stores/logs'

const { t } = useI18n()
const props=withDefaults(defineProps<{active?:boolean;rowHeight?:number;localFileActions?:boolean;exportActionLabel?:string;detailMode?:'inline'|'sheet'}>(),{
  active:true,rowHeight:26,localFileActions:true,exportActionLabel:'logs.revealExport',detailMode:'inline',
})
const viewActive=useViewActivity(()=>props.active,'committed')
let disposed=false
let activation=0
let initialized=false
const logs = useLogsStore()
const errorStore = useErrorStore()
const confirmStore = useConfirmStore()

type PanelTab = 'live' | 'file' | 'raw'
const tab = ref<PanelTab>('live')
const autoScroll = ref(true)
const exportFormat = ref<'log' | 'csv' | 'json'>('log')
const lastExportPath = ref('')
const exporting = ref(false)
const mobileExport = presentationProfile().ui === 'mobile'
const selected = ref<LogRecord | null>(null)
const overviewOpen = ref(!mobileExport)
const filtersOpen = ref(!mobileExport)
const formatOptions = ['log', 'csv', 'json'].map(value => ({ value, label: value.toUpperCase() }))
function setExportFormat(value: string | number): void {
  if (value === 'log' || value === 'csv' || value === 'json') exportFormat.value = value
}

// ---------- 实时时间线：定高窗口化渲染 ----------
const ROW_H = computed(()=>props.rowHeight)
const OVERSCAN = 8
const scroller = ref<HTMLElement | null>(null)
const scrollTop = ref(0)
const viewportH = ref(420)

const liveRows = computed(() => logs.visible)
const first = computed(() => Math.max(0, Math.floor(scrollTop.value / ROW_H.value) - OVERSCAN))
const rowCapacity = computed(() => Math.ceil(viewportH.value / ROW_H.value) + OVERSCAN * 2)
const last = computed(() => Math.min(liveRows.value.length, first.value + rowCapacity.value))
const rendered = computed(() => liveRows.value.slice(first.value, last.value))
const padTop = computed(() => first.value * ROW_H.value)
const padBottom = computed(() => Math.max(0, (liveRows.value.length - last.value) * ROW_H.value))

function onScroll(evt: Event): void {
  const el = evt.target as HTMLElement
  scrollTop.value = el.scrollTop
  autoScroll.value = el.scrollHeight - el.scrollTop - el.clientHeight < ROW_H.value * 2
}

let resizeObserver: ResizeObserver | undefined

async function scrollToBottom(force = false): Promise<void> {
  if (!force && (!autoScroll.value || tab.value !== 'live')) return
  await nextTick()
  const el = scroller.value
  if (el) el.scrollTop = el.scrollHeight
}

watch(() => liveRows.value[liveRows.value.length-1]?.key, () => { void scrollToBottom() })
watch([() => logs.query, () => logs.levelFilter.slice(), () => logs.originFilter.slice()],()=>{scrollTop.value=0;if(scroller.value)scroller.value.scrollTop=0})

// ---------- 关键词高亮（纯文本分段，不用 v-html） ----------
interface Segment { text: string; hit: boolean }
function segments(text: string): Segment[] {
  const needle = logs.query.trim()
  if (!needle) return [{ text, hit: false }]
  const lower = text.toLowerCase()
  const target = needle.toLowerCase()
  const out: Segment[] = []
  let cursor = 0
  while (cursor < text.length) {
    const at = lower.indexOf(target, cursor)
    if (at < 0) {
      out.push({ text: text.slice(cursor), hit: false })
      break
    }
    if (at > cursor) out.push({ text: text.slice(cursor, at), hit: false })
    out.push({ text: text.slice(at, at + target.length), hit: true })
    cursor = at + target.length
  }
  return out
}

function toggleInList<T>(list: T[], item: T): void {
  const idx = list.indexOf(item)
  if (idx >= 0) list.splice(idx, 1)
  else list.push(item)
}

// ---------- 历史文件 ----------
const fileOptions = computed(() => logs.files.map(f => ({
  name: f.name,
  label: `${f.day} · ${f.size_bytes > 1048576 ? (f.size_bytes / 1048576).toFixed(1) + ' MB' : Math.round(f.size_bytes / 1024) + ' KB'}`,
})))
const currentFile = ref('')

async function openFile(name: string): Promise<void> {
  if (!name || disposed || !viewActive.value || tab.value !== 'file') return
  currentFile.value = name
  selected.value = null
  try {await logs.loadFileLatest(name)}
  catch(error){if(!disposed&&viewActive.value)errorStore.fromError(t('logs.loadFailed'),error,'LogPanel.file')}
}

function pickFile(evt: Event): void {
  void openFile((evt.target as HTMLSelectElement).value)
}

let fileFilterTimer: number | undefined
function cancelFileFilter(): void {
  window.clearTimeout(fileFilterTimer)
  fileFilterTimer = undefined
}
watch([() => logs.query, () => logs.levelFilter.slice(), () => logs.originFilter.slice()], () => {
  cancelFileFilter()
  if (!viewActive.value || tab.value !== 'file' || !currentFile.value) return
  fileFilterTimer = window.setTimeout(() => { fileFilterTimer = undefined; void openFile(currentFile.value) }, 300)
})

// ---------- 原始文本 ----------
const rawText = ref('')
const rawFile = ref('')
const rawLoading = ref(false)
const RAW_LIMIT_BYTES = 2 * 1024 * 1024
let rawGeneration=0
let rawRequest:ReturnType<typeof GetLogContent>|undefined
function cancelRaw():void {
  rawGeneration++
  try{void Promise.resolve(rawRequest?.cancel?.()).catch(()=>{})}catch{/* Already completed. */}
  rawRequest=undefined
  rawLoading.value=false
}

async function loadRaw(): Promise<void> {
  if(disposed||!viewActive.value)return
  cancelRaw()
  const generation=rawGeneration
  const name = currentFile.value || logs.currentFileLabel
  const size = logs.files.find(f => f.name === name)?.size_bytes ?? 0
  if (size > RAW_LIMIT_BYTES) {
    errorStore.warn(t('logs.export'), t('logs.rawTooLarge', { name }))
    return
  }
  rawLoading.value = true
  rawFile.value = name
  try {
    rawRequest=GetLogContent(name)
    const text=await rawRequest
    if(generation!==rawGeneration||disposed)return
    rawText.value = text || ''
  } catch (e: any) {
    if(generation!==rawGeneration||disposed)return
    errorStore.fromError(t('logs.loadFailed'), e, 'LogPanel')
    rawText.value = ''
  } finally {
    if(generation===rawGeneration){rawRequest=undefined;rawLoading.value = false}
  }
}

watch(tab, (next) => {
  cancelFileFilter()
  if (next !== 'raw') cancelRaw()
  if (next !== 'file') logs.cancelFileLoad()
  if (!viewActive.value) return
  if (next === 'file' && !logs.fileRecords.length && currentFile.value) void openFile(currentFile.value)
  if (next === 'raw' && !rawText.value) void loadRaw()
})

// ---------- 操作 ----------
async function refresh(): Promise<void> {
  const token = activation
  await logs.bootstrap()
  if (disposed || !viewActive.value || token !== activation) return
  if (tab.value === 'file' && currentFile.value) await openFile(currentFile.value)
  else if (tab.value === 'raw') await loadRaw()
  else await scrollToBottom(true)
}

async function openLogDir(): Promise<void> {
  const dir = String(logs.stats?.dir || '')
  if (!dir) return
  try { await revealLogDirectory(dir) } catch (e: any) { errorStore.fromError(t('logs.openDirFailed'), e, 'LogPanel') }
}

async function revealLogFile(): Promise<void> {
  const dir = String(logs.stats?.dir || '')
  const name = currentFile.value || logs.currentFileLabel
  if (!dir || !name) return
  try { await OpenFileInExplorer(`${dir}/${name}`) } catch (e: any) { errorStore.fromError(t('logs.openDirFailed'), e, 'LogPanel') }
}

async function doExport(): Promise<void> {
  if (exporting.value) return
  exporting.value = true
  try {
    const result = await logs.exportToFile({
      filename: tab.value === 'live' ? '' : (currentFile.value || logs.currentFileLabel),
      format: exportFormat.value,
    })
    lastExportPath.value = result.path
    if (mobileExport && !disposed && viewActive.value) await revealDiagnosticExport(result.path)
    errorStore.info(t('logs.exportOkTitle'), t(mobileExport ? 'logs.mobileExportReady' : 'logs.exportOk', { lines: result.lines, path: result.path }))
  } catch (e: any) {
    errorStore.fromError(t('logs.exportFailed'), e, 'LogPanel')
  } finally {
    exporting.value = false
  }
}

async function revealExport(): Promise<void> {
  if (!lastExportPath.value) return
  try { await revealDiagnosticExport(lastExportPath.value) } catch (e: any) { errorStore.fromError(t('logs.openDirFailed'), e, 'LogPanel') }
}

async function doClear(): Promise<void> {
  const ok = await confirmStore.confirm({
    title: t('logs.clearTitle'),
    message: t('logs.clearMessage'),
    level: 'danger',
  })
  if (!ok) return
  try {
    const removed = await logs.clearAll()
    clearFileView()
    errorStore.info(t('logs.clearTitle'), t('logs.clearDone', { count: removed }))
  } catch (e: any) {
    errorStore.fromError(t('logs.clearTitle'), e, 'LogPanel')
  }
}

function clearFileView(): void {
  cancelRaw()
  logs.clearFileView()
  rawText.value = ''
  rawFile.value = ''
}

async function copyText(text: string, label?: string): Promise<void> {
  try {
    await navigator.clipboard.writeText(text)
    errorStore.info(label || t('logs.copyTitle'), t('logs.copied'))
  } catch {
    errorStore.warn(label || t('logs.copyTitle'), t('logs.copyFailed'))
  }
}

function copyRecord(rec: LogRecord): void {
  void copyText(logRecordToLine(rec), t('logs.copyLine'))
}

function copyVisible(): void {
  const source = tab.value === 'file' ? logs.fileRecords : logs.visible
  void copyText(source.map(logRecordToLine).join('\n'), t('logs.copyAll'))
}

function togglePause(): void {
  logs.setPaused(!logs.paused)
}

async function switchLevel(next: string): Promise<void> {
  try {
    await logs.applyLevel(next as any)
  } catch (e: any) {
    errorStore.fromError(t('logs.levelTitle'), e, 'LogPanel')
  }
}

const levelChips = computed(() => ALL_LEVELS.map(level => ({
  level,
  count: logs.levelCounts[level],
  active: logs.levelFilter.includes(level),
})))

const originChips = computed(() => ALL_ORIGINS.map(origin => ({
  origin,
  label: logs.describeOrigin(origin as LogOrigin),
  count: logs.originCounts[origin] ?? 0,
  active: logs.originFilter.includes(origin),
})))

const stats = computed<Record<string, any>>(() => logs.stats || {})
const bufferedLabel = computed(() => `${stats.value.buffered ?? 0} / ${stats.value.capacity ?? 0}`)

watch(viewActive,async active=>{
  const token=++activation
  if(!active){cancelFileFilter();cancelRaw();logs.cancelBootstrap();logs.cancelFileLoad();void logs.detach();return}
  if(!initialized){const loaded=await logs.bootstrap();if(disposed||token!==activation)return;initialized=loaded;currentFile.value=logs.files[0]?.name||logs.currentFileLabel}
  if(tab.value==='live')await logs.attach()
  else if(tab.value==='file'&&currentFile.value)await openFile(currentFile.value)
  else if(tab.value==='raw')await loadRaw()
  if(!disposed&&token===activation)await scrollToBottom(true)
},{immediate:true})
watch(tab,next=>{if(!viewActive.value)return;if(next==='live')void logs.attach();else void logs.detach()})
watch(scroller,el=>{
  resizeObserver?.disconnect()
  if(!el)return
  viewportH.value=el.clientHeight
  resizeObserver=new ResizeObserver(()=>{viewportH.value=el.clientHeight})
  resizeObserver.observe(el)
},{flush:'post'})

onBeforeUnmount(() => {
  disposed=true
  activation++
  cancelRaw()
  logs.cancelBootstrap()
  logs.cancelFileLoad()
  resizeObserver?.disconnect()
  cancelFileFilter()
  void logs.detach()
})
</script>

<template>
  <div class="log-panel cczj-flex cczj-flex-col cczj-gap-2">
    <p v-if="logs.connectionError" class="log-hint" role="status">{{t('logs.connectionRetry')}}</p>
    <p v-if="logs.stats?.last_write_error || logs.stats?.unpersisted" class="log-hint" role="alert">{{ t('logs.diskFailure', { count: logs.stats?.unpersisted || 0 }) }} {{ logs.stats?.last_write_error || t('logs.writerRecovered') }}</p>
    <p v-if="logs.missingRecords" class="log-hint" role="status">{{ t('logs.missingRecords', { count: logs.missingRecords }) }}</p>
    <p v-if="logs.stats?.level === 'WARN' || logs.stats?.level === 'ERROR'" class="log-hint" role="status">{{ t('logs.detailSuppressed') }}</p>
    <p v-if="!mobileExport" class="log-hint">{{ t('logs.exportScope') }}</p>
    <!-- 概览卡 -->
    <section class="block">
      <div class="block-hd cczj-flex cczj-items-center cczj-justify-between">
        <h3>{{ t('logs.overview') }}<span v-if="mobileExport" class="log-overview-summary">{{ logs.level }} · {{ stats.buffered ?? 0 }}</span></h3>
        <div class="cczj-flex cczj-items-center cczj-gap-4">
          <span class="log-live" :class="{ on: logs.attached && !logs.paused }">
            <Icon :name="logs.paused ? 'pause' : 'play'" :size="11" />
            {{ logs.paused ? t('logs.paused') : t('logs.live') }}
          </span>
          <Button variant="secondary" size="sm" @click="refresh">
            <Icon name="refresh" :size="12" /> {{ t('common.refresh') }}
          </Button>
          <Button v-if="mobileExport" variant="ghost" size="sm" :aria-expanded="overviewOpen" :aria-label="t('logs.overview')" @click="overviewOpen = !overviewOpen">
            <Icon :name="overviewOpen ? 'chevron-up' : 'chevron-down'" :size="16" />
          </Button>
        </div>
      </div>

      <div v-if="overviewOpen" class="log-grid cczj-grid">
        <div class="log-card">
          <div class="log-card-icon lvl-info"><Icon name="sliders" :size="16" /></div>
          <div class="log-card-body">
            <div class="log-card-label">{{ t('logs.levelTitle') }}</div>
            <div class="log-card-value">{{ logs.level }}</div>
            <div class="log-card-note">{{ logs.level === 'DEBUG' ? t('logs.debugOn') : t('logs.debugOffNote') }}</div>
          </div>
        </div>
        <div class="log-card">
          <div class="log-card-icon lvl-accent"><Icon name="layers" :size="16" /></div>
          <div class="log-card-body">
            <div class="log-card-label">{{ t('logs.buffered') }}</div>
            <div class="log-card-value">{{ bufferedLabel }}</div>
            <div class="log-card-note">{{ t('logs.dropped', { count: stats.dropped ?? 0 }) }}</div>
          </div>
        </div>
        <div class="log-card">
          <div class="log-card-icon lvl-warn"><Icon name="database" :size="16" /></div>
          <div class="log-card-body">
            <div class="log-card-label">{{ t('logs.onDisk') }}</div>
            <div class="log-card-value">{{ stats.files ?? 0 }} · {{ logs.bytesLabel }}</div>
            <div class="log-card-note">{{ t('logs.keepDays', { days: stats.keep_days ?? 30 }) }}</div>
          </div>
        </div>
        <div class="log-card">
          <div class="log-card-icon lvl-pink"><Icon name="folder" :size="16" /></div>
          <div class="log-card-body cczj-min-w-0">
            <div class="log-card-label">{{ t('logs.dir') }}</div>
            <button class="log-card-path cczj-truncate" :title="t('logs.copyTitle')" @click="copyText(stats.dir || '', t('logs.copyTitle'))">{{ stats.dir || '—' }}</button>
            <div class="log-card-note">{{ stats.goos }}/{{ stats.goarch }}</div>
          </div>
        </div>
      </div>

      <div v-if="overviewOpen" class="log-levels cczj-flex cczj-flex-wrap cczj-items-center cczj-gap-4">
        <span class="log-levels-label">{{ t('logs.switchLevel') }}</span>
        <ChoiceSegment :model-value="logs.level" :options="['DEBUG', 'INFO', 'WARN', 'ERROR'].map(value => ({ value, label: value }))" size="sm" @update:model-value="switchLevel(String($event))" />
        <span class="log-levels-hint">{{ t('logs.levelImmediate') }}</span>
      </div>
    </section>

    <!-- 筛选与操作 -->
    <section class="block log-controls">
      <div class="cczj-flex cczj-flex-wrap cczj-items-center cczj-gap-4">
        <div class="log-search cczj-flex cczj-items-center">
          <Icon name="search" :size="13" />
          <input v-model="logs.query" type="search" :aria-label="t('logs.searchPlaceholder')" :placeholder="t('logs.searchPlaceholder')" />
        </div>
        <Button v-if="mobileExport" variant="secondary" size="sm" :aria-expanded="filtersOpen" @click="filtersOpen = !filtersOpen">
          <Icon name="sliders" :size="14" /> {{ t('logs.filters') }}
        </Button>
      </div>
      <div v-if="filtersOpen" class="log-filter-groups cczj-flex cczj-flex-wrap cczj-items-center cczj-gap-4">
        <button
          v-for="chip in levelChips"
          :key="chip.level"
          class="log-chip"
          :class="{ active: chip.active }"
          :aria-pressed="chip.active"
          @click="toggleInList(logs.levelFilter, chip.level)"
        >
          <span class="log-dot" :class="`lvl-${chip.level.toLowerCase()}`"></span>
          {{ chip.level }}
          <em>{{ chip.count }}</em>
        </button>
        <span class="log-divider"></span>
        <span class="log-levels-label">{{ t('logs.originLabel') }}</span>
        <button
          v-for="chip in originChips"
          :key="chip.origin"
          class="log-chip log-chip-origin"
          :class="{ active: chip.active }"
          :aria-pressed="chip.active"
          :disabled="!chip.count"
          @click="toggleInList(logs.originFilter, chip.origin)"
        >{{ chip.label }} <em>{{ chip.count }}</em></button>
      </div>

      <div v-if="!mobileExport || filtersOpen" class="log-toolbar cczj-flex cczj-flex-wrap cczj-items-center cczj-gap-4">
        <Button variant="secondary" size="sm" @click="togglePause">
          <Icon :name="logs.paused ? 'play' : 'pause'" :size="12" />
          {{ logs.paused ? t('logs.resume') : t('logs.pause') }}
        </Button>
        <Button variant="secondary" size="sm" :aria-pressed="autoScroll" :class="{ 'log-toggle-on': autoScroll }" @click="autoScroll = !autoScroll">
          <Icon name="arrow-up" :size="12" /> {{ t('logs.autoScroll') }}
        </Button>
        <Button variant="secondary" size="sm" @click="copyVisible">
          <Icon name="copy" :size="12" /> {{ t('logs.copyAll') }}
        </Button>
        <Button v-if="localFileActions" variant="secondary" size="sm" @click="openLogDir">
          <Icon name="folder" :size="12" /> {{ t('logs.openDir') }}
        </Button>
        <Button v-if="localFileActions" variant="secondary" size="sm" @click="revealLogFile">
          <Icon name="file-text" :size="12" /> {{ t('logs.reveal') }}
        </Button>
      </div>
      <div class="log-export-toolbar cczj-flex cczj-flex-wrap cczj-items-center cczj-gap-4">
        <div class="log-export-actions cczj-flex cczj-items-center cczj-gap-3">
          <ChoiceSegment :model-value="exportFormat" :options="formatOptions" size="sm" @update:model-value="setExportFormat" />
          <Button variant="primary" size="sm" :loading="exporting" @click="doExport">
            <Icon name="download" :size="12" /> {{ t('logs.export') }}
          </Button>
          <Button v-if="lastExportPath" variant="secondary" size="sm" @click="revealExport">
            <Icon name="folder" :size="12" /> {{ t(mobileExport ? 'logs.shareExport' : exportActionLabel) }}
          </Button>
        </div>
        <details v-if="mobileExport && filtersOpen" class="log-export-scope log-hint">
          <summary>{{ t('logs.exportScopeTitle') }}</summary>
          <p>{{ t('logs.exportScope') }}</p>
        </details>
        <Button v-if="!mobileExport || filtersOpen" variant="danger" size="sm" @click="doClear">
          <Icon name="trash" :size="12" /> {{ t('logs.clear') }}
        </Button>
      </div>
    </section>

    <!-- 视图 -->
    <section class="block log-view">
      <div class="log-tabs">
        <button
          v-for="item in ([
            { id: 'live', label: t('logs.tabLive'), icon: 'terminal' },
            { id: 'file', label: t('logs.tabFile'), icon: 'clock' },
            { id: 'raw', label: t('logs.tabRaw'), icon: 'file-text' },
          ] as const)"
          :key="item.id"
          class="log-tab"
          :class="{ active: tab === item.id }"
          :aria-pressed="tab === item.id"
          @click="tab = item.id"
        >
          <Icon :name="item.icon" :size="13" />
          <span>{{ item.label }}</span>
        </button>
        <div v-if="tab === 'file'" class="cczj-flex cczj-items-center cczj-gap-4 log-file-picker">
          <select class="log-select" :aria-label="t('logs.tabFile')" :value="currentFile" @change="pickFile">
            <option v-for="option in fileOptions" :key="option.name" :value="option.name">{{ option.label }}</option>
          </select>
          <span v-if="logs.fileInfo" class="log-hint">
            {{ t('logs.fileRows', { shown: logs.fileRecords.length, total: logs.fileInfo.total_rows, matched: logs.fileInfo.match_rows }) }}
          </span>
          <span v-if="logs.fileInfo?.truncated" class="log-hint" role="status">{{ t('logs.historyTruncated') }}</span>
        </div>
      </div>

      <!-- 实时时间线 -->
      <div
        v-if="tab === 'live'"
        ref="scroller"
        class="log-list"
        @scroll.passive="onScroll"
      >
        <div v-if="!logs.visible.length" class="log-empty">
          <Icon name="info" :size="16" /> {{ t('logs.noRows') }}
        </div>
        <div v-else :style="{ height: padTop + 'px' }"></div>
        <div
          v-for="rec in rendered"
          :key="rec.key"
          class="log-row cczj-flex cczj-items-center"
          :class="[`row-${rec.level.toLowerCase()}`, { picked: selected?.key === rec.key && selected?.origin === rec.origin }]"
          :style="{ height: ROW_H + 'px' }"
          role="button" tabindex="0"
          @keydown.enter="selected = rec" @keydown.space.prevent="selected = rec"
          @click="selected = rec"
        >
          <span class="log-ts">{{ formatLogTime(rec.ts) }}</span>
          <span class="log-lvl" :class="`lvl-${rec.level.toLowerCase()}`">{{ rec.level }}</span>
          <span class="log-origin">{{ logs.describeOrigin(rec.origin) }}</span>
          <span class="log-msg cczj-flex-1 cczj-min-w-0">
            <span v-for="(seg, index) in segments(rec.message)" :key="index" :class="{ hit: seg.hit }">{{ seg.text }}</span>
          </span>
          <span v-if="rec.caller" class="log-caller cczj-truncate" :title="rec.caller">{{ rec.caller }}</span>
        </div>
        <div v-if="padBottom" :style="{ height: padBottom + 'px' }"></div>
      </div>

      <!-- 历史文件 -->
      <div v-else-if="tab === 'file'" class="log-list">
        <div v-if="logs.fileLoading" class="log-empty"><Icon name="refresh" :size="16" /> {{ t('logs.loading') }}</div>
        <div v-else-if="!logs.fileRecords.length" class="log-empty">
          <Icon name="info" :size="16" /> {{ t('logs.noRows') }}
        </div>
        <template v-else>
          <button v-if="logs.fileInfo?.has_earlier" class="log-more" :disabled="logs.fileLoading" @click="logs.loadFileEarlier()">
            <Icon name="chevron-up" :size="12" /> {{ t('logs.loadEarlier') }}
          </button>
          <div
            v-for="rec in logs.fileRecords"
            :key="`${rec.origin}-${rec.key}`"
            class="log-row cczj-flex cczj-items-center"
            :class="[`row-${rec.level.toLowerCase()}`, { picked: selected?.key === rec.key && selected?.origin === rec.origin }]"
            role="button" tabindex="0"
            @keydown.enter="selected = rec" @keydown.space.prevent="selected = rec"
            @click="selected = rec"
          >
            <span class="log-ts">{{ formatLogTime(rec.ts) }}</span>
            <span class="log-lvl" :class="`lvl-${rec.level.toLowerCase()}`">{{ rec.level }}</span>
            <span class="log-msg cczj-flex-1 cczj-min-w-0">
              <span v-for="(seg, index) in segments(rec.message)" :key="index" :class="{ hit: seg.hit }">{{ seg.text }}</span>
            </span>
            <span v-if="rec.caller" class="log-caller cczj-truncate" :title="rec.caller">{{ rec.caller }}</span>
          </div>
        </template>
      </div>

      <!-- 原始文本 -->
      <div v-else class="log-raw-wrap">
        <div class="log-raw-hd cczj-flex cczj-items-center cczj-justify-between">
          <span class="log-hint">{{ rawFile || logs.currentFileLabel }}</span>
          <div class="cczj-flex cczj-items-center cczj-gap-3">
            <Button variant="secondary" size="sm" @click="loadRaw"><Icon name="refresh" :size="12" /> {{ t('common.refresh') }}</Button>
            <Button variant="secondary" size="sm" @click="copyText(rawText, t('logs.copyFile'))"><Icon name="copy" :size="12" /> {{ t('logs.copyFile') }}</Button>
          </div>
        </div>
        <pre class="log-raw">{{ rawLoading ? t('logs.loading') : (rawText || t('logs.noRows')) }}</pre>
      </div>

      <!-- 选中详情 -->
      <div v-if="selected && detailMode === 'inline'" class="log-detail cczj-flex cczj-items-start cczj-gap-7">
        <LogRecordDetails class="cczj-flex-1 cczj-min-w-0" :record="selected" :origin-label="logs.describeOrigin(selected.origin)" @copy="copyRecord(selected)" />
        <Button variant="ghost" size="sm" :aria-label="t('common.close')" @click="selected = null"><Icon name="close" :size="12" /></Button>
      </div>
    </section>
    <Modal :model-value="!!selected && detailMode === 'sheet' && viewActive" :title="t('logs.recordDetail')" width="640px" @update:model-value="selected = null">
      <LogRecordDetails v-if="selected" :record="selected" :origin-label="logs.describeOrigin(selected.origin)" @copy="copyRecord(selected)" />
    </Modal>
  </div>
</template>

<style scoped src="../styles/components/log-panel.css"></style>
