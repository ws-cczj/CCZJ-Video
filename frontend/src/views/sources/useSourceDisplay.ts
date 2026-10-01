import { useCollectStore } from '../../stores/collect'
import type { SourceScheduleItem } from '../../stores/collect'
import { useSourceStore } from '../../stores/source'
import { tr } from '../../locales'

/**
 * 采集源页面的显示层派生。
 *
 * 卡片和它的子面板都从这里拿同一批判定：值全部现读 collect/source store，
 * 不缓存也不发请求，所以拆组件不会多出一次 IPC，也不会改变任何状态时机。
 */

/** store 里的源行类型没导出，页面只用到这几个字段，按用到的形状声明。 */
export interface SourceRow {
  id?: number
  source_key?: string
  name: string
  api_url: string
  collect_limit?: number
  collect_hours?: number
  enabled?: number | boolean
  /** 声明式采集策略文档（JSON 文本），空表示内置 standard_cms */
  strategy_config?: string
}

/** 新建/编辑弹窗的入参：编辑时整行原样带过来，保存链路只读它。 */
export interface EditSource {
  source_key: string
  name: string
  api_url: string
  collect_limit?: number
  collect_hours?: number
  enabled?: number
  strategy_config?: string
}

/** GetSourceParamsDoc 整形后的参数指南。 */
export interface ParamsDoc {
  base_url: string
  path_params?: { name: string; type: string; desc: string; example: string }[]
  query_ac?: { name: string; type: string; desc: string; example: string }[]
  query_common?: { name: string; type: string; desc: string; example: string }[]
  query_advanced?: { name: string; type: string; desc: string; example: string }[]
}

/** 探测点阵的一格：Go 侧原样透传，字段缺失就按「没探」显示。 */
export type ProbeSlot = Record<string, any>

/** 每个源的后台定时采集表单。页面按 source_key 存，面板只改字段。 */
export interface ScheduleForm {
  enabled: boolean
  mode: string
  interval_min: number
}

/** 导出成功后的提示内容。 */
export interface ExportResult {
  path: string
  sourceKey: string
}

export function useSourceDisplay() {
  const collectStore = useCollectStore()
  const sourceStore = useSourceStore()

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
    // 空闲时也要能看出上一次是不是有整页没落地，否则缺数据只藏在日志里。
    const state = collectStore.lastRunOf(key).state
    if (state === 'failed') return 'error'
    if (state === 'partial') return 'warning'
    return 'idle'
  }

  function statusText(key: string): string {
    if (isRunning(key)) return tr('sources.statusRunning')
    if (isPaused(key)) return tr('sources.statusPaused')
    if (hasError(key)) return tr('sources.statusError')
    const state = collectStore.lastRunOf(key).state
    if (state === 'failed') return tr('sources.lastRunFailedShort')
    if (state === 'partial') return tr('sources.lastRunPartialShort')
    return tr('sources.statusIdle')
  }

  function lastRunText(key: string): string {
    const info = collectStore.lastRunOf(key)
    if (info.state === 'none') return '--'
    const secs = Math.round(info.elapsedMs / 1000)
    if (info.state === 'failed') return info.error || tr('sources.lastRunFailedShort')
    if (info.state === 'partial') {
      return tr('sources.lastRunPartial', {
        saved: info.saved,
        fetch: info.fetchFailedPages,
        save: info.saveFailedPages,
        secs,
      })
    }
    if (info.stopped) {
      return tr('sources.lastRunStopped', { saved: info.saved, secs })
    }
    return tr('sources.lastRunOk', { saved: info.saved, secs })
  }

  function lastRunClass(key: string): string {
    const state = collectStore.lastRunOf(key).state
    if (state === 'failed') return 'bad'
    if (state === 'partial') return 'warn'
    return ''
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

  function clockOf(unix: number): string {
    return unix ? new Date(unix * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) : '--:--'
  }

  // 灰点要说清是"没探"而不是"探挂了"：这一页没打开的时段本来就不会有请求。
  function dotTitle(slot: ProbeSlot): string {
    const bucket = clockOf(Number(slot.bucket_unix) || 0)
    if (!slot.probed) return `${tr('sources.probeDotNone')} · ${bucket}`
    const state = slot.ok ? tr('sources.probeDotOk') : tr('sources.probeDotBad')
    const head = tr('sources.probeAt', { time: clockOf(Number(slot.ts_unix) || 0) })
    const detail = slot.ok ? `${Number(slot.latency_ms) || 0}ms` : String(slot.error || '')
    return detail ? `${state} · ${head} · ${detail}` : `${state} · ${head}`
  }

  function dotClass(slot: ProbeSlot): string {
    if (!slot.probed) return 'none'
    return slot.ok ? 'ok' : 'bad'
  }

  function formatSyncTime(ts: number): string {
    if (!ts) return '--'
    return new Date(ts).toLocaleTimeString()
  }

  function sourceName(key: string): string {
    const s = sourceStore.sources.find(x => sk(x) === key)
    return s?.name || key
  }

  /** 后端调度器里这一源的定时配置；没配过就是 undefined，界面按「关」显示。 */
  function scheduleStateFor(key: string): SourceScheduleItem | undefined {
    return collectStore.schedulerStatus?.source_schedules?.find((s: SourceScheduleItem) => s.source_key === key)
  }

  return {
    sk,
    isRunning,
    isPaused,
    hasError,
    statusClass,
    statusText,
    lastRunText,
    lastRunClass,
    modeLabel,
    modeTagVariant,
    progressFor,
    formatProgress,
    dotClass,
    dotTitle,
    formatSyncTime,
    sourceName,
    scheduleStateFor,
  }
}
