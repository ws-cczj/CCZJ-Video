import { defineStore } from 'pinia'
import { tr } from '../locales'
import { ref, computed } from 'vue'
import { GetAllSources, GetSourceStats, GetSetting, SetSetting } from '../api/app'
import { useErrorStore } from './error'
import type { SourceStat } from '../types'

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

  async function fetchSources(): Promise<void> {
    loading.value = true
    try {
      const [s, st] = await Promise.all([GetAllSources(), GetSourceStats()])
      sources.value = s as SourceRow[]
      stats.value = st as SourceStat[]
      loaded.value = true

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
      useErrorStore().fromError(tr('errors.loadSourcesFailed'), e)
    } finally {
      loading.value = false
    }
  }

  /**
   * 源列表只有采集源管理页会改，其他页拿它只为填下拉框和 currentSourceKey，
   * 所以第二次以后直接吃已加载的结果：以前 Home/Detail/Player/Search/Recent/
   * Recommendations 各自 onMounted 都全量重拉，进一次播放页就是 6 个重复 IPC。
   *
   * loaded 只记成功，失败保持 false，下一个挂载点才会重试而不是永久吃空列表。
   * force 不等现有 inflight：管理页刚写完库就要读回真实状态，共享一个在途请求
   * 可能拿到写之前的快照。并发两个 force 只会把同一份结果写两遍，无害。
   */
  async function loadSources(force = false): Promise<void> {
    if (!force && inflight) return inflight
    if (!force && loaded.value) return
    const run = fetchSources()
    inflight = run
    try {
      await run
    } finally {
      if (inflight === run) inflight = null
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
    loaded,
    currentSource,
    loadSources,
    switchSource,
  }
})
