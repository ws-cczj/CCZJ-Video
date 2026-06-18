/**
 * 后台管理系统 — 公共数据 composable
 * 共享：源列表、统计数据、采集状态映射
 */
import { ref, type Ref } from 'vue'
import { GetAllSources, GetSourceStats, GetCollectStatus } from '../../../../bindings/cczjVideo/app'

// ============ 模块级单例状态 ============
const sources: Ref<any[]> = ref([])
const sourceStats: Ref<any[]> = ref([])
const collectStatusMap: Ref<Record<string, any>> = ref({})
const loading = ref(false)
let loaded = false

// ============ 公共方法 ============

async function loadSourcesAndStats(): Promise<void> {
  if (loading.value) return
  loading.value = true
  try {
    const [s, st] = await Promise.all([GetAllSources(), GetSourceStats()])
    sources.value = (s as any[]) || []
    sourceStats.value = (st as any[]) || []
    // 逐源拉取采集状态
    const statusMap: Record<string, any> = {}
    await Promise.all(
      sources.value.map(async (src: any) => {
        const key = src.source_key || src.key || ''
        if (!key) return
        try {
          statusMap[key] = await GetCollectStatus(key)
        } catch {
          statusMap[key] = { running: false }
        }
      }),
    )
    collectStatusMap.value = statusMap
    loaded = true
  } catch {
    // 静默失败
  } finally {
    loading.value = false
  }
}

async function refreshCollectStatus(sourceKey: string): Promise<void> {
  try {
    collectStatusMap.value[sourceKey] = await GetCollectStatus(sourceKey)
  } catch {
    // 忽略
  }
}

function getSourceName(key: string): string {
  const src = sources.value.find((s: any) => (s.source_key || s.key) === key)
  return src?.name || key
}

function ensureLoaded(): Promise<void> {
  if (loaded) return Promise.resolve()
  return loadSourcesAndStats()
}

// ============ 导出 ============

export function useAdminData() {
  return {
    sources,
    sourceStats,
    collectStatusMap,
    loading,
    loadSourcesAndStats,
    refreshCollectStatus,
    getSourceName,
    ensureLoaded,
  }
}
