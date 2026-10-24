import { defineStore } from 'pinia'
import { tr } from '../locales'
import { ref, computed } from 'vue'
import { GetAllSources, GetSourceStats, GetSetting, SetSetting, WriteLog, normalizeApiError } from '../api/app'
import { useErrorStore } from './error'
import type { SourceStat } from '../types'
import { useGeneration } from '../composables/useGeneration'
import { list, object, text } from '../api/decode'

const DEFAULT_SOURCE_KEY = 'default_source_key'

interface SourceRow {
  id?: number
  source_key?: string
  name: string
  api_url: string
  collect_limit?: number
  collect_hours?: number
  enabled?: number | boolean
  created_at?: string
  /** 声明式采集策略文档（JSON 文本），由扩展包或手写配置产生，空表示内置 standard_cms */
  strategy_config?: string
}

export const useSourceStore = defineStore('source', () => {
  const sources = ref<SourceRow[]>([])
  const currentSourceKey = ref<string>('')
  const stats = ref<SourceStat[]>([])
  const loading = ref(false)
  const loaded = ref(false)
  let inflight: Promise<void> | null = null

  const currentSource = computed<SourceRow | undefined>(() =>
    sources.value.find((s) => s.source_key === currentSourceKey.value)
  )

  const generation = useGeneration()

  async function fetchSources(force: boolean): Promise<void> {
    const seq = generation.begin()
    loading.value = true
    const pending = GetAllSources()
    function trace(stage: string, extra: Record<string,string> = {}): void {
      void WriteLog({ level: 'INFO', source: 'Sources.list', message: stage, detail: '', fields: {
        ui_request_id: pending.request.requestId, ui_flight_id: pending.request.flightId,
        pipeline: pending.request.pipeline, generation: String(seq), forced: String(force), ...extra,
      } }).catch(() => {})
    }
    trace('requested')
    // Statistics are supplementary: their failure must not hide a committed source.
    void Promise.resolve(GetSourceStats()).then(st => {
      if (generation.isCurrent(seq)) stats.value = st ?? []
    }, e => {
      if (generation.isCurrent(seq)) useErrorStore().fromError(tr('errors.loadSourcesFailed'), e)
    })
    try {
      const rows = list(await pending, value => {
        const row = object(value)
        text(row.source_key); text(row.name); text(row.api_url)
        return row as unknown as SourceRow
      })
      if (!generation.isCurrent(seq)) { trace('discarded_stale_response', { count: String(rows.length) }); return }
      sources.value = rows
      trace('published', { count: String(rows.length), source_keys: rows.map(row => row.source_key).join(',') })
      loaded.value = true
      if (!rows.some(src => src.source_key === currentSourceKey.value)) {
        let savedKey = ''
        try { savedKey = await GetSetting(DEFAULT_SOURCE_KEY) } catch { /* use first source */ }
        if (!generation.isCurrent(seq)) return
        if (rows.some(src => src.source_key === currentSourceKey.value)) return
        currentSourceKey.value = rows.some(src => src.source_key === savedKey)
          ? savedKey : rows[0]?.source_key || ''
      }
    } catch (e) {
      const failure = normalizeApiError(e)
      trace('failed', { backend_request_id: failure.requestId || '', code: failure.code })
      if (!generation.isCurrent(seq)) return
      if (force) throw e
      useErrorStore().fromError(tr('errors.loadSourcesFailed'), e)
    } finally {
      if (generation.isCurrent(seq)) loading.value = false
    }
  }

  // Forced reads start a new generation after writes. Older reads may finish,
  // but may never overwrite the committed list, selection, errors or loading state.
  async function loadSources(force = false): Promise<void> {
    if (!force && inflight) return inflight
    if (!force && loaded.value) return
    const run = fetchSources(force)
    inflight = run
    try { await run } finally { if (inflight === run) inflight = null }
  }

  async function switchSource(key: string): Promise<void> {
    await SetSetting(DEFAULT_SOURCE_KEY, key)
    currentSourceKey.value = key
  }

  return {
    sources,
    currentSourceKey,
    stats,
    loading,
    loaded,
    currentSource,
    loadSources,
    switchSource,
  }
})
