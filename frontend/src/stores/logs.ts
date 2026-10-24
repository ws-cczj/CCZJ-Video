import { computed, ref, shallowRef } from 'vue'
import { defineStore } from 'pinia'
import {
  ClearLogs,
  ExportLogs,
  GetLogLevel,
  GetLogFiles,
  GetLogRecords,
  GetLogStats,
  ReadLogPage,
  SetLogLevel,
  StartLogStream,
  StopLogStream,
} from '../api/app'
import { onBackendEvent } from '../api/events'
import { tr } from '../locales'

export type LogLevel = 'DEBUG' | 'INFO' | 'WARN' | 'ERROR'
export type LogOrigin = 'app' | 'collect' | 'douban' | 'scheduler' | 'update' | 'ui'

export interface LogRecord {
  sessionId?: string
  persisted?: boolean
  /** 列表 key：后端序号，或历史文件的行号 */
  key: string
  seq: number
  ts: number
  level: LogLevel
  caller: string
  message: string
  origin: LogOrigin
  /** 采集来源 key，仅 origin 为 collect 时有值 */
  sourceKey?: string
}

export const ALL_LEVELS: LogLevel[] = ['DEBUG', 'INFO', 'WARN', 'ERROR']
export const ALL_ORIGINS: LogOrigin[] = ['app', 'collect', 'douban', 'scheduler', 'update', 'ui']

const MAX_RECORDS = 5000
const PAGE_SIZE = 200
/** 事件流丢包或暂停期间的兜底轮询间隔 */
const POLL_MS = 2000

const LEVEL_ALIASES: Record<string, LogLevel> = {
  DEBUG: 'DEBUG',
  INFO: 'INFO',
  INFORMATION: 'INFO',
  WARN: 'WARN',
  WARNING: 'WARN',
  ERROR: 'ERROR',
  ERR: 'ERROR',
  FATAL: 'ERROR',
}

function normalizeLevel(level: string | undefined): LogLevel {
  return LEVEL_ALIASES[(level || '').toUpperCase()] || 'INFO'
}

const COLLECT_PREFIX = /^\[collect:([^\]]+)\]\s*/
const UI_PREFIX = /^([\w.\-]{1,64}) :: /

/**
 * 后端各模块用行首 [标记] 自报身份（applog 原样透传，不做归类）。这里按标记还原来源，
 * 顺序即优先级：`[collect:源]` > 显式前缀 > 前端 Toast 的 `xxx :: `，都没命中才算「系统」。
 * 不再用调用文件兜底：caller 只带文件名，app/douban/scheduler.go 和 app/collection/scheduler.go
 * 会撞成同一个 scheduler.go，把豆瓣的定时补全误记成采集调度。
 * 新增模块前缀时只需往这张表里加一行。
 */
const PREFIX_ORIGINS: Array<{ re: RegExp; origin: LogOrigin }> = [
  { re: /^\[scheduler\]\s*/, origin: 'scheduler' },
  { re: /^\[Douban(?:Chart|Comments)?\]\s*/, origin: 'douban' },
  { re: /^\[(?:FieldMapping|FETCH|Strategy|CleanHTML|SearchSource|ImportSourceVideos)\]\s*/, origin: 'collect' },
  { re: /^\[Updater\]\s*/, origin: 'update' },
]

/**
 * 后端把采集、调度、前端 Toast 都写进同一条日志流，这里用行首标记还原来源，
 * 这样时间线只需要消费 app:log 一个事件，天然不会出现重复行。
 */
function classify(raw: { message?: string; caller?: string; level?: string; ts?: number; seq?: number; session_id?: string; persisted?: boolean }): LogRecord {
  let message = raw.message || ''
  let origin: LogOrigin = 'app'
  let sourceKey: string | undefined
  const collect = COLLECT_PREFIX.exec(message)
  const ui = UI_PREFIX.exec(message)
  const prefixed = PREFIX_ORIGINS.find((entry) => entry.re.test(message))
  if (collect) {
    origin = 'collect'
    sourceKey = collect[1]
    message = message.slice(collect[0].length)
  } else if (prefixed) {
    origin = prefixed.origin
    message = message.replace(prefixed.re, '')
  } else if (ui) {
    origin = 'ui'
    message = message.slice(ui[0].length)
  } else if (/source_key="([^"]+)"/.test(message)) {
    origin = 'collect'
    sourceKey = /source_key="([^"]+)"/.exec(message)?.[1]
  }
  return {
    key: `${raw.session_id || ''}:${raw.seq ?? 0}`,
    sessionId: raw.session_id,
    persisted: raw.persisted,
    seq: Number(raw.seq ?? 0),
    ts: Number(raw.ts ?? Date.now()),
    level: normalizeLevel(raw.level),
    caller: raw.caller || '',
    message,
    origin,
    sourceKey,
  }
}

export function formatLogTime(ts: number): string {
  if (!ts) return ''
  const d = new Date(ts)
  const pad = (n: number, width = 2) => String(n).padStart(width, '0')
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}.${pad(d.getMilliseconds(), 3)}`
}

export function logRecordToLine(rec: LogRecord): string {
  const date = new Date(rec.ts)
  const pad = (n: number, width = 2) => String(n).padStart(width, '0')
  const stamp = `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} `
    + `${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}.${pad(date.getMilliseconds(), 3)}`
  const head = rec.caller ? `[${stamp}] [${rec.level}] [${rec.caller}] ` : `[${stamp}] [${rec.level}] `
  return head + (rec.sessionId ? `[session=${rec.sessionId} seq=${rec.seq} persisted=${rec.persisted !== false}] ` : '') + rec.message
}

export const useLogsStore = defineStore('logs', () => {
  const records = shallowRef<LogRecord[]>([])
  const lastSeq = ref(0)
  const level = ref<LogLevel>('INFO')
  const stats = ref<Record<string, any> | null>(null)
  const files = ref<Array<{ name: string; day: string; size_bytes: number; mod_ts: number }>>([])
  const attached = ref(false)
  const paused = ref(false)
  const loading = ref(false)
  const connectionError = ref(false)
  const missingRecords = ref(0)
  let sessionId = ''
  // Events can skip a record. Only ring snapshots may advance this cursor.
  let pulledSeq = 0
  function adoptSession(next: string | undefined): void {
    if (!next || next === sessionId) return
    if (sessionId) { records.value = []; lastSeq.value = 0; pulledSeq = 0; missingRecords.value = 0 }
    sessionId = next
  }

  const query = ref('')
  const levelFilter = ref<LogLevel[]>([])
  const originFilter = ref<LogOrigin[]>([])

  const visible = computed(() => {
    const needle = query.value.trim().toLowerCase()
    const levels = levelFilter.value
    const origins = originFilter.value
    return records.value.filter((rec) => {
      if (levels.length && !levels.includes(rec.level)) return false
      if (origins.length && !origins.includes(rec.origin)) return false
      if (needle && !(`${rec.caller} ${rec.message}`.toLowerCase().includes(needle))) return false
      return true
    })
  })

  const levelCounts = computed(() => {
    const counts: Record<LogLevel, number> = { DEBUG: 0, INFO: 0, WARN: 0, ERROR: 0 }
    for (const rec of records.value) counts[rec.level]++
    return counts
  })

  const originCounts = computed(() => {
    const counts: Partial<Record<LogOrigin, number>> = {}
    for (const rec of records.value) counts[rec.origin] = (counts[rec.origin] ?? 0) + 1
    return counts
  })

  let detachEvents: Array<() => void> = []
  let pollTimer: number | undefined
  let streamGeneration=0
  let pulling=false
  let pullRequest:ReturnType<typeof GetLogRecords>|undefined
  let nativeQueue=Promise.resolve()
  function nativeStream(start:boolean):Promise<void> {
    nativeQueue=nativeQueue.then(async()=>{try{if(start)await StartLogStream();else await StopLogStream()}catch{connectionError.value=true}})
    return nativeQueue
  }

  function append(rec: LogRecord) {
    appendMany([rec])
  }
  function appendMany(items:LogRecord[]):void {
    if (!items.length) return
    adoptSession(items[items.length - 1]?.sessionId)
    const merged = new Map(records.value.map(rec => [rec.seq, rec]))
    for (const rec of items) if (rec.seq > 0) merged.set(rec.seq, rec)
    records.value = Array.from(merged.values()).sort((a,b) => a.seq - b.seq).slice(-MAX_RECORDS)
    lastSeq.value = records.value[records.value.length - 1]?.seq || 0
  }
  function appendSnapshot(items: LogRecord[]): void {
    const mapped = items.sort((a,b) => a.seq - b.seq)
    const first = mapped[0]?.seq || 0
    if (pulledSeq && first > pulledSeq + 1) {
      const retained = records.value.filter(rec => rec.seq > pulledSeq && rec.seq < first).length
      missingRecords.value += Math.max(0, first - pulledSeq - 1 - retained)
    }
    appendMany(mapped)
    pulledSeq = mapped[mapped.length - 1]?.seq || pulledSeq
  }

  /** 从内存环形缓冲补齐增量；暂停时不消费，恢复后一次拉回全部内容 */
  async function pull(limit = MAX_RECORDS): Promise<void> {
    if (paused.value || !attached.value || pulling) return
    pulling=true
    const generation=streamGeneration
    try {
      pullRequest=GetLogRecords(pulledSeq, limit)
      const [rows, snapshot] = await Promise.all([pullRequest, GetLogStats()])
      if(generation!==streamGeneration||!attached.value||paused.value)return
      adoptSession(snapshot?.session_id)
      stats.value = snapshot as Record<string, any>
      level.value = normalizeLevel(String(snapshot?.level || ''))
      if(Array.isArray(rows)) appendSnapshot(rows.map(classify))
      connectionError.value=false
    } catch {
      if(generation===streamGeneration)connectionError.value=true
    } finally {pulling=false;pullRequest=undefined}
  }

  async function refreshStats(): Promise<void> {
    try {
      stats.value = (await GetLogStats()) as Record<string, any>
      level.value = normalizeLevel((stats.value?.level as string) || '')
    } catch { /* 忽略 */ }
    try {
      const result=await GetLogFiles()
      files.value=Array.isArray(result)?result as typeof files.value:[]
    } catch { /* 忽略 */ }
  }

  let bootstrapGeneration = 0
  let bootstrapFlight: Promise<boolean> | undefined
  const bootstrapReads = new Set<{ cancel?: () => void | Promise<void> }>()
  function cancelBootstrap(): void {
    bootstrapGeneration++
    bootstrapFlight = undefined
    loading.value = false
    for (const request of bootstrapReads) {
      try { void Promise.resolve(request.cancel?.()).catch(() => {}) } catch { /* Already settled. */ }
    }
  }
  async function bootstrapRead<T>(request: Promise<T> & { cancel?: () => void | Promise<void> }): Promise<T> {
    bootstrapReads.add(request)
    try { return await request } finally { bootstrapReads.delete(request) }
  }
  function bootstrap(): Promise<boolean> {
    if (bootstrapFlight) return bootstrapFlight
    const token = bootstrapGeneration
    loading.value = true
    bootstrapFlight = (async () => {
      try {
        const [snapshot, filenames, rows] = await Promise.all([
          bootstrapRead(GetLogStats()), bootstrapRead(GetLogFiles()),
          bootstrapRead(GetLogRecords(0, MAX_RECORDS)),
        ])
        if (token !== bootstrapGeneration) return false
        stats.value = snapshot as Record<string, any>
        adoptSession(stats.value?.session_id)
        level.value = normalizeLevel(String(stats.value?.level || ''))
        files.value = Array.isArray(filenames) ? filenames as typeof files.value : []
        // Events can arrive while a manual refresh reads the ring. Merge by
        // sequence without clearing the screen or dropping the newer tail.
        if (Array.isArray(rows)) {
          appendSnapshot(rows.map(classify))
        }
        connectionError.value = false
        return true
      } catch {
        if (token === bootstrapGeneration) connectionError.value = true
        return false
      } finally {
        if (token === bootstrapGeneration) { loading.value = false; bootstrapFlight = undefined }
      }
    })()
    return bootstrapFlight
  }

  async function attach(): Promise<void> {
    if (attached.value) return
    attached.value = true
    const generation=++streamGeneration
    detachEvents.push(onBackendEvent<{ seq?: number; ts?: number; level?: string; caller?: string; message?: string; session_id?: string; persisted?: boolean }>(
      'app:log',
      (payload) => {
        if (paused.value) return
        append(classify(payload || {}))
      },
    ))
    pollTimer = window.setInterval(() => { void pull() }, POLL_MS)
    await nativeStream(true)
    if(generation===streamGeneration&&attached.value)await pull()
  }

  async function detach(): Promise<void> {
    if (!attached.value) return
    attached.value = false
    streamGeneration++
    try{void Promise.resolve(pullRequest?.cancel?.()).catch(()=>{})}catch{/* Completed request. */}
    for (const off of detachEvents) off()
    detachEvents = []
    if (pollTimer !== undefined) {
      window.clearInterval(pollTimer)
      pollTimer = undefined
    }
    await nativeStream(false)
  }

  function setPaused(value: boolean) {
    paused.value = value
    if (!value) void pull()
  }

  async function applyLevel(next: LogLevel): Promise<void> {
    const applied = await SetLogLevel(next)
    level.value = normalizeLevel(String(applied))
    await refreshStats()
  }

  async function clearAll(): Promise<number> {
    cancelBootstrap()
    const removed = await ClearLogs()
    records.value = []
    lastSeq.value = 0
    pulledSeq = 0
    missingRecords.value = 0
    await refreshStats()
    return Number(removed) || 0
  }

  // ===== 历史文件分页 =====
  const fileRecords = ref<LogRecord[]>([])
  const fileInfo = ref<{ filename: string; total_rows: number; match_rows: number; first_row: number; has_earlier: boolean; truncated: boolean } | null>(null)
  const fileLoading = ref(false)
  let fileRequest = 0
  let pendingFileRequest:ReturnType<typeof ReadLogPage>|undefined
  function cancelFileLoad():void {
    fileRequest++
    try{void Promise.resolve(pendingFileRequest?.cancel?.()).catch(()=>{})}catch{/* Completed request. */}
    pendingFileRequest=undefined
    fileLoading.value=false
  }

  async function loadFile(filename: string, startRow: number, replace: boolean): Promise<void> {
    cancelFileLoad()
    if(replace){fileRecords.value=[];fileInfo.value=null}
    const token = ++fileRequest
    fileLoading.value = true
    try {
      pendingFileRequest=ReadLogPage({
        filename,
        start_row: startRow,
        limit: PAGE_SIZE,
        levels: levelFilter.value.slice(),
        query: query.value.trim(),
      })
      const resp = (await pendingFileRequest) as {
        records: Array<{ seq: number; ts: number; level: string; caller: string; message: string }>
        total_rows: number
        match_rows: number
        first_row: number
        has_earlier: boolean
        truncated: boolean
        filename: string
      }
      if (token !== fileRequest) return
      const mapped = (Array.isArray(resp?.records)?resp.records:[]).map((row, index) => {
        // 保留归类结果：整列都写「文件」只是重复标签页已经说明的事，来源列要能看到豆瓣/采集
        const rec = classify({ ...row, seq: 0 })
        return { ...rec, key: `${resp.first_row + index}`, seq: 0 }
      })
      fileRecords.value = replace ? mapped : [...mapped, ...fileRecords.value]
      fileInfo.value = {
        filename: resp.filename,
        total_rows: resp.total_rows,
        match_rows: resp.match_rows,
        first_row: resp.first_row,
        has_earlier: resp.has_earlier,
        truncated: resp.truncated,
      }
      connectionError.value=false
    } catch {
      if(token===fileRequest)connectionError.value=true
    } finally {
      if (token === fileRequest) {fileLoading.value = false;pendingFileRequest=undefined}
    }
  }

  async function loadFileLatest(filename: string): Promise<void> {
    await loadFile(filename, -1, true)
  }

  async function loadFileEarlier(): Promise<void> {
    if (!fileInfo.value || !fileInfo.value.has_earlier) return
    await loadFile(fileInfo.value.filename, fileInfo.value.first_row, false)
  }

  function clearFileView(): void {
    cancelFileLoad()
    fileRecords.value = []
    fileInfo.value = null
  }

  // ===== 导出 =====
  // 导出不再传路径：WebView2 的原生另存为窗口点了没反应也不报错，文件一律由后端落在
  // 数据目录 exports/logs 下，返回实际路径。
  async function exportToFile(options: { filename?: string; format: string; sinceSeq?: number }): Promise<{ path: string; lines: number; bytes: number }> {
    const result = (await ExportLogs({
      path: '',
      filename: options.filename || '',
      format: options.format,
      levels: levelFilter.value.slice(),
      query: query.value.trim(),
      since_seq: options.sinceSeq ?? 0,
    })) as { path: string; lines: number; bytes: number }
    await refreshStats()
    return result
  }

  const bytesLabel = computed(() => {
    const kb = Number(stats.value?.total_kb || 0)
    if (kb < 1024) return `${kb} KB`
    return `${(kb / 1024).toFixed(1)} MB`
  })

  const currentFileLabel = computed(() => {
    const day = new Date()
    const pad = (n: number) => String(n).padStart(2, '0')
    return `cczj-${day.getFullYear()}-${pad(day.getMonth() + 1)}-${pad(day.getDate())}.log`
  })

  function describeOrigin(origin: LogOrigin): string {
    switch (origin) {
      case 'collect': return tr('logs.originCollect')
      case 'douban': return tr('logs.originDouban')
      case 'scheduler': return tr('logs.originScheduler')
      case 'update': return tr('logs.originUpdate')
      case 'ui': return tr('logs.originUi')
      default: return tr('logs.originApp')
    }
  }

  return {
    records,
    visible,
    levelCounts,
    originCounts,
    lastSeq,
    level,
    stats,
    files,
    attached,
    paused,
    loading,
    connectionError,
    missingRecords,
    query,
    levelFilter,
    originFilter,
    fileRecords,
    fileInfo,
    fileLoading,
    bytesLabel,
    currentFileLabel,
    bootstrap,
    cancelBootstrap,
    attach,
    detach,
    pull,
    refreshStats,
    setPaused,
    applyLevel,
    clearAll,
    loadFile,
    loadFileLatest,
    loadFileEarlier,
    cancelFileLoad,
    clearFileView,
    exportToFile,
    describeOrigin,
  }
})
