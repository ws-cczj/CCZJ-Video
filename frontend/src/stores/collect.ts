import { defineStore } from 'pinia'
import { tr } from '../locales'
import { ref, computed } from 'vue'
import { StartCollect, PauseCollect, ResumeCollect, StopCollect, GetCollectSchedule, SetCollectSchedule, TriggerCollectNow, StopBackgroundCollect, SetSourceSchedule, GetCollectStatus } from '../api/app'
import { useErrorStore } from './error'
import { appEvent } from '../event'
import { onBackendEvent } from '../api/events'

export interface CollectPageEvent {
  source_key: string
  page: number
  names: string[]
}

export interface CollectScheduleConfig {
  enable_background: boolean
  background_interval_seconds: number
  background_interval_minutes?: number
  enable_startup_catchup: boolean
  enable_initial_full_collect: boolean
  source_gap_seconds: number
  page_gap_seconds: number
}

export interface SchedulerStatus {
  running: boolean
  background: boolean
  background_every_minutes: number
  background_every_seconds: number
  source_gap_seconds: number
  page_gap_seconds: number
  startup_catchup: boolean
  initial_full_collect: boolean
  last_exit_unix: number
  last_run_unix: number
  now_unix: number
  note: string
  source_schedules: SourceScheduleItem[]
}

export interface SourceScheduleItem {
  source_key: string
  name: string
  enabled: boolean
  mode: string
  interval_min: number
  running: boolean
}

// 单个源的采集状态快照
export interface SourceCollectState {
  sourceKey: string
  running: boolean
  paused: boolean
  mode: string
  current: number
  total: number
  page: number
  pageNames: string[]
  log: string[]
  done: boolean
  error: string
  startTime: number
  // 速度/ETA 相关
  videoCount: number       // 已处理视频总数
  videoTotal: number       // 预估视频总数
  speed: number            // 速度: 页/秒
  etaSeconds: number       // 预估剩余秒数
}

// 后端采集状态快照（GetCollectStatus 的原始返回 + 拉取时刻）
export interface BackendCollectStatus {
  source_key: string
  running: boolean
  paused: boolean
  mode: string
  current: number
  total: number
  page: number
  names: string[]
  log: string
  syncedAt: number
}

type CollectMode = 'full' | 'incremental' | 'once'

function toGoCfg(cfg: CollectScheduleConfig): any {
  const seconds = Math.max(30, Math.floor(cfg.background_interval_seconds || 60))
  // ⭐ 字段名必须与 Go struct 的 json tag 一致（snake_case），否则 Wails 反序列化时字段为零值
  return {
    enable_background: !!cfg.enable_background,
    background_interval_seconds: seconds,
    background_interval_minutes: Math.floor(seconds / 60),
    enable_startup_catchup: !!cfg.enable_startup_catchup,
    enable_initial_full_collect: !!cfg.enable_initial_full_collect,
    source_gap_seconds: Math.max(1, Math.floor(cfg.source_gap_seconds || 10)),
    page_gap_seconds: Math.max(1, Math.floor(cfg.page_gap_seconds || 30)),
  }
}

function fromGoCfg(v: any): CollectScheduleConfig {
  if (!v) {
    return {
      enable_background: true,
      background_interval_seconds: 60,
      background_interval_minutes: 1,
      enable_startup_catchup: true,
      enable_initial_full_collect: false,
      source_gap_seconds: 10,
      page_gap_seconds: 30,
    }
  }
  const secs = Number(v.background_interval_seconds) || (Number(v.background_interval_minutes) || 1) * 60
  return {
    enable_background: !!v.enable_background,
    background_interval_seconds: Math.max(30, secs),
    background_interval_minutes: Math.floor(secs / 60),
    enable_startup_catchup: !!v.enable_startup_catchup,
    enable_initial_full_collect: !!v.enable_initial_full_collect,
    source_gap_seconds: Number(v.source_gap_seconds) || 10,
    page_gap_seconds: Number(v.page_gap_seconds) || 30,
  }
}

function makeState(sourceKey: string): SourceCollectState {
  return {
    sourceKey,
    running: false,
    paused: false,
    mode: 'full',
    current: 0,
    total: 0,
    page: 0,
    pageNames: [],
    log: [],
    done: false,
    error: '',
    startTime: 0,
    videoCount: 0,
    videoTotal: 0,
    speed: 0,
    etaSeconds: 0,
  }
}

export const useCollectStore = defineStore('collect', () => {
  // === 全局（兼容旧代码）===
  const running = ref(false)
  const paused = ref(false)
  const sourceKey = ref('')
  const current = ref(0)
  const total = ref(0)
  const log = ref<string[]>([])
  const done = ref(false)
  const error = ref('')
  const startTime = ref(0)
  const page = ref(0)
  const pageNames = ref<string[]>([])
  const mode = ref<CollectMode>('full')
  const lastHours = ref(0)

  // === 调度器级别 ===
  const schedulerStatus = ref<SchedulerStatus | null>(null)
  const scheduleConfig = ref<CollectScheduleConfig | null>(null)
  const scheduleSaving = ref(false)

  // === 每个源的独立状态 ===
  const sourceStates = ref<Map<string, SourceCollectState>>(new Map())

  function getState(key: string): SourceCollectState {
    if (!sourceStates.value.has(key)) {
      sourceStates.value.set(key, makeState(key))
    }
    return sourceStates.value.get(key)!
  }

  function syncGlobalFromState(st: SourceCollectState) {
    sourceKey.value = st.sourceKey
    running.value = st.running
    paused.value = st.paused
    mode.value = st.mode as CollectMode
    current.value = st.current
    total.value = st.total
    page.value = st.page
    pageNames.value = st.pageNames
    // Copy rather than alias: `log.value = st.log` made the global log and the
    // per-source log the same array, so every line the handlers below append twice
    // showed up twice, and scheduler messages leaked into the source's own log.
    log.value = st.log.slice()
    error.value = st.error
    done.value = st.done
    startTime.value = st.startTime
  }

  const progress = computed<number>(() => {
    if (total.value <= 0) return 0
    return Math.round((current.value / total.value) * 100)
  })

  function progressFor(key: string): number {
    const st = sourceStates.value.get(key)
    if (!st || st.total <= 0) return 0
    return Math.round((st.current / st.total) * 100)
  }

  // === 后端权威状态 ===
  // 事件流是"尽力而为"的：页面重挂载、后端单独被调度器驱动时前端会漏掉进度。
  // 这里直连 GetCollectStatus，既用于展示明细，也用于把前端状态校正回真实值。
  const backendStatus = ref<Record<string, BackendCollectStatus>>({})

  async function syncFromBackend(key: string): Promise<BackendCollectStatus | null> {
    if (!key) return null
    let raw: any
    try {
      raw = await GetCollectStatus(key)
    } catch {
      return null
    }
    if (!raw) return null
    const snapshot: BackendCollectStatus = {
      source_key: raw.source_key || key,
      running: !!raw.running,
      paused: !!raw.paused,
      mode: raw.mode || '',
      current: Number(raw.current) || 0,
      total: Number(raw.total) || 0,
      page: Number(raw.page) || 0,
      names: Array.isArray(raw.names) ? raw.names : [],
      log: raw.log || '',
      syncedAt: Date.now(),
    }
    backendStatus.value = { ...backendStatus.value, [key]: snapshot }

    const st = getState(key)
    st.running = snapshot.running
    st.paused = snapshot.paused
    st.current = snapshot.current
    st.total = snapshot.total
    st.page = snapshot.page
    st.pageNames = snapshot.names
    if (snapshot.mode) st.mode = snapshot.mode
    if (snapshot.running && !st.startTime) st.startTime = Date.now()
    if (!snapshot.running) st.done = false
    syncGlobalFromState(st)
    return snapshot
  }

  // 批量校正：事件流只覆盖"页面在线时"的推送，重进页面需拉一次权威快照
  async function syncAllFromBackend(keys: string[]): Promise<void> {
    await Promise.all(keys.map(k => syncFromBackend(k)))
  }

  // 记录上一次 progress 事件的时间和页码，用于计算速度
  const _lastProgressAt = new Map<string, { ts: number; page: number }>()

  // === 事件监听（Wails 后端事件 → appEvent 桥接） ===
  onBackendEvent<{ source_key: string; current: number; total: number }>('collect:progress', (data) => {
    const st = getState(data.source_key)
    st.current = data.current
    st.total = data.total

    // 计算速度/ETA
    const last = _lastProgressAt.get(data.source_key)
    if (last && data.current > last.page) {
      const dt = (Date.now() - last.ts) / 1000
      if (dt > 0) {
        st.speed = Math.round(((data.current - last.page) / dt) * 10) / 10
        const remaining = data.total - data.current
        if (st.speed > 0 && remaining > 0) {
          st.etaSeconds = Math.round(remaining / st.speed)
        }
      }
    }
    _lastProgressAt.set(data.source_key, { ts: Date.now(), page: data.current })
    // 估计视频数（每页约5条）
    st.videoCount = Math.max(st.videoCount, data.current * 5)
    st.videoTotal = Math.max(st.videoTotal, data.total * 5)

    if (data.source_key === sourceKey.value) {
      current.value = data.current
      total.value = data.total
    }

    // 桥接到前端事件总线
    appEvent.emit('collect:progress', data.source_key, data.current, data.total)
  })

  onBackendEvent<{ source_key: string; message: string }>('collect:log', (data) => {
    if (data.source_key === '__scheduler__') {
      log.value.push(data.message)
      if (log.value.length > 200) log.value.shift()
      return
    }
    const st = getState(data.source_key)
    st.log.push(data.message)
    if (st.log.length > 200) st.log.shift()
    if (data.source_key === sourceKey.value) {
      log.value.push(data.message)
      if (log.value.length > 200) log.value.shift()
    }

    // 桥接到前端事件总线
    appEvent.emit('collect:log', data.source_key, data.message)
  })

  onBackendEvent<CollectPageEvent>('collect:page', (data) => {
    const st = getState(data.source_key)
    st.page = data.page
    st.pageNames = data.names || []
    st.videoCount += (data.names || []).length
    if (data.source_key === sourceKey.value) {
      page.value = data.page
      pageNames.value = data.names || []
    }
  })

  onBackendEvent<{ source_key: string; error?: string; mode?: string }>('collect:done', (data) => {
    const st = getState(data.source_key)
    st.running = false
    st.paused = false
    st.done = true
    st.error = data.error || ''
    if (!st.error) {
      st.log.push(tr('sources.collectDone'))
    } else {
      st.log.push(tr('sources.collectError', { error: st.error }))
    }
    if (data.source_key === sourceKey.value) {
      running.value = false
      paused.value = false
      done.value = true
      error.value = data.error || ''
      if (!error.value) {
        log.value.push(tr('sources.collectDone'))
      } else {
        log.value.push(tr('sources.collectError', { error: error.value }))
      }
    }

    // 桥接到前端事件总线
    appEvent.emit('collect:done', data.source_key, data.error)

    setTimeout(() => {
      st.done = false
      st.error = ''
      if (data.source_key === sourceKey.value) {
        done.value = false
        error.value = ''
      }
    }, 8000)
  })

  // === 采集操作（支持模式参数）===
  async function startCollect(key: string, collectMode: CollectMode = 'full', hours: number = 0): Promise<void> {
    const st = getState(key)
    st.running = true
    st.paused = false
    st.mode = collectMode
    st.current = 0
    st.total = 0
    st.page = 0
    st.pageNames = []
    st.log = [tr('sources.startCollecting', { mode: modeLabel(collectMode) })]
    st.error = ''
    st.startTime = Date.now()

    syncGlobalFromState(st)

    try {
      await StartCollect({ source_key: key, mode: collectMode, hours })
      appEvent.emit('collect:start', key, collectMode)
    } catch (e) {
      st.running = false
      st.paused = false
      st.error = (e as Error)?.toString() || tr('sources.unknownError')
      st.log.push(tr('sources.startFailed', { error: st.error }))
      syncGlobalFromState(st)
    }
  }

  async function pauseCollect(key?: string): Promise<boolean> {
    const k = key || sourceKey.value
    if (!k) return false
    try {
      const ok = await PauseCollect({ source_key: k, mode: '', hours: 0 })
      if (ok) {
        const st = getState(k)
        st.paused = true
        st.log.push(tr('downloads.paused'))
        if (k === sourceKey.value) paused.value = true
      }
      return ok
    } catch (e) {
      getState(k).log.push(tr('sources.pauseFailed', { error: (e as Error).message }))
      return false
    }
  }

  async function resume(key?: string): Promise<boolean> {
    const k = key || sourceKey.value
    if (!k) return false
    try {
      const ok = await ResumeCollect({ source_key: k, mode: '', hours: 0 })
      if (ok) {
        const st = getState(k)
        st.paused = false
        st.log.push(tr('sources.resumed'))
        if (k === sourceKey.value) paused.value = false
      }
      return ok
    } catch (e) {
      getState(k).log.push(tr('sources.resumeFailed', { error: (e as Error).message }))
      return false
    }
  }

  async function stop(key?: string): Promise<boolean> {
    const k = key || sourceKey.value
    if (!k) return false
    try {
      const ok = await StopCollect({ source_key: k, mode: '', hours: 0 })
      if (ok) {
        const st = getState(k)
        st.running = false
        st.paused = false
        st.log.push(tr('sources.stopped'))
        if (k === sourceKey.value) {
          running.value = false
          paused.value = false
        }
      }
      return ok
    } catch (e) {
      getState(k).log.push(tr('sources.stopFailed', { error: (e as Error).message }))
      return false
    }
  }

  // === 调度器操作 ===
  async function loadSchedule(): Promise<void> {
    try {
      const status: any = await GetCollectSchedule()
      schedulerStatus.value = status as SchedulerStatus
      const everySec = Number(status.background_every_seconds) || (Number(status.background_every_minutes) || 1) * 60
      scheduleConfig.value = {
        enable_background: !!status.background,
        background_interval_seconds: Math.max(30, everySec),
        background_interval_minutes: Math.floor(everySec / 60),
        enable_startup_catchup: !!status.startup_catchup,
        enable_initial_full_collect: !!status.initial_full_collect,
        source_gap_seconds: Number(status.source_gap_seconds) || 10,
        page_gap_seconds: Number(status.page_gap_seconds) || 30,
      }
    } catch (e) {
      useErrorStore().fromError(tr('errors.loadSchedulerFailed'), e)
    }
  }

  async function saveSchedule(cfg: CollectScheduleConfig): Promise<void> {
    scheduleSaving.value = true
    try {
      const updated: any = await SetCollectSchedule(toGoCfg(cfg))
      scheduleConfig.value = fromGoCfg(updated)
      await loadSchedule()
    } finally {
      scheduleSaving.value = false
    }
  }

  async function triggerNow(sourceKey?: string, collectMode: CollectMode = 'full', hours: number = 0): Promise<void> {
    try {
      await TriggerCollectNow({ source_key: sourceKey || '', mode: collectMode, hours })
      if (sourceKey) {
        log.value.push(tr('sources.triggered', { key: sourceKey, mode: modeLabel(collectMode) }))
      } else {
        log.value.push(tr('sources.triggeredFull'))
      }
      await loadSchedule()
    } catch (e) {
      log.value.push(tr('sources.triggerFailed', { error: (e as Error).message }))
    }
  }

  async function stopBackground(): Promise<void> {
    try {
      await StopBackgroundCollect()
      log.value.push(tr('sources.stoppedBackground'))
      await loadSchedule()
    } catch (e) {
      log.value.push(tr('sources.stopBackgroundFailed', { error: (e as Error).message }))
    }
  }

  // === 源级别调度配置 ===
  async function saveSourceSchedule(sourceKey: string, enabled: boolean, mode: string, intervalMin: number): Promise<void> {
    try {
      await SetSourceSchedule({ source_key: sourceKey, enabled, mode, interval_min: intervalMin })
      log.value.push(tr('sources.sourceScheduleUpdated', { key: sourceKey }))
      await loadSchedule()
    } catch (e) {
      log.value.push(tr('sources.saveConfigFailed', { error: (e as Error).message }))
    }
  }

  // === 速度/ETA 辅助 ===
  function elapsed(key: string): number {
    const st = sourceStates.value.get(key)
    if (!st || !st.startTime) return 0
    return Math.max(0, Math.floor((Date.now() - st.startTime) / 1000))
  }

  function elapsedStr(key: string): string {
    const s = elapsed(key)
    if (s < 60) return s + tr('common.unitSecond')
    const m = Math.floor(s / 60)
    const rs = s % 60
    if (m < 60) return m + tr('common.unitMinute') + rs + tr('common.unitSecond')
    const h = Math.floor(m / 60)
    return h + tr('common.unitHour') + (m % 60) + tr('common.unitMinute')
  }

  function speedStr(key: string): string {
    const st = sourceStates.value.get(key)
    if (!st || st.speed <= 0) return '--'
    return st.speed + ' ' + tr('common.pagesPerSecond')
  }

  function etaStr(key: string): string {
    const st = sourceStates.value.get(key)
    if (!st || st.etaSeconds <= 0) return '--'
    const s = st.etaSeconds
    if (s < 60) return s + tr('common.unitSecond')
    if (s < 3600) return Math.floor(s / 60) + tr('common.unitMinute') + (s % 60) + tr('common.unitSecond')
    return Math.floor(s / 3600) + tr('common.unitHour') + Math.floor((s % 3600) / 60) + tr('common.unitMinute')
  }

  return {
    // 全局
    running,
    paused,
    sourceKey,
    current,
    total,
    log,
    done,
    error,
    progress,
    page,
    pageNames,
    mode,
    lastHours,
    // 调度器
    schedulerStatus,
    scheduleConfig,
    scheduleSaving,
    // 每源状态
    sourceStates,
    backendStatus,
    // 方法
    startCollect,
    getState,
    progressFor,
    syncFromBackend,
    syncAllFromBackend,
    pause: pauseCollect,
    resume,
    stop,
    loadSchedule,
    saveSchedule,
    triggerNow,
    stopBackground,
    saveSourceSchedule,
    // 速度/ETA 辅助
    elapsed,
    elapsedStr,
    speedStr,
    etaStr,
    modeLabel,
  }
})

function modeLabel(m: string): string {
  switch (m) {
    case 'full': return tr('sources.modeFull')
    case 'incremental': return tr('sources.modeIncremental')
    case 'once': return tr('sources.modeOnce')
    default: return m
  }
}
