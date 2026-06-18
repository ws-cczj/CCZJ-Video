import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { GetAllSources, GetSourceStats, GetSetting, SetSetting } from '../../bindings/cczjVideo/app'
import { useErrorStore } from './error'
import type { SourceStat } from '../types'

const DEFAULT_SOURCE_KEY = 'default_source_key'

interface SourceRow {
  id?: number
  source_key?: string
  name: string
  api_url: string
  url_template?: string
  url_prefix?: string
  url_suffix?: string
  collect_limit?: number
  collect_hours?: number
  enabled?: number | boolean
  created_at?: string
}

export const useSourceStore = defineStore('source', () => {
  const sources = ref<SourceRow[]>([])
  const currentSourceKey = ref<string>('')
  const stats = ref<SourceStat[]>([])
  const loading = ref(false)

  const currentSource = computed<SourceRow | undefined>(() =>
    sources.value.find((s) => s.source_key === currentSourceKey.value)
  )

  async function loadSources(): Promise<void> {
    loading.value = true
    try {
      const [s, st] = await Promise.all([GetAllSources(), GetSourceStats()])
      sources.value = s as SourceRow[]
      stats.value = st as SourceStat[]

      if (!currentSourceKey.value && sources.value.length > 0) {
        let savedKey = ''
        try {
          savedKey = (await GetSetting(DEFAULT_SOURCE_KEY)) as string
        } catch { /* ignore */ }
        if (savedKey && sources.value.some(src => src.source_key === savedKey)) {
          currentSourceKey.value = savedKey
        } else {
          currentSourceKey.value = sources.value[0].source_key || ''
        }
      }
    } catch (e) {
      useErrorStore().fromError('加载源站列表失败', e)
    } finally {
      loading.value = false
    }
  }

  async function switchSource(key: string): Promise<void> {
    currentSourceKey.value = key
    try {
      await SetSetting(DEFAULT_SOURCE_KEY, key)
    } catch { /* ignore */ }
  }

  return {
    sources,
    currentSourceKey,
    stats,
    loading,
    currentSource,
    loadSources,
    switchSource,
  }
})
