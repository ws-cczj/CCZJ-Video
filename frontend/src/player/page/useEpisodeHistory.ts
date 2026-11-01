import { ref } from 'vue'
import { GetHistoryPosition, SaveWatchHistory } from '../../api/app'
import { epProgressKey, loadEpProgress, saveEpProgress, getEpProgressPct } from '../../utils/episodeProgress'

export function useEpisodeHistory(deps: {
  identity: () => {sourceKey:string;vodId:string;name:string;globalId?:number}
  episodes: () => ReadonlyArray<{ep_num?:number}>
  currentIndex: () => number
  onPersistenceError?: (error: unknown) => void
}) {
let progressEvents = 0
let progressUIUpdates = 0
let historyWrites = 0
const epProgressMap = ref<Record<number, number>>({})

function epKeyOf(idx: number): string {
  return epProgressKey(deps.identity().globalId, deps.identity().name, deps.episodes()[idx]?.ep_num ?? (idx+1))
}

// 从独立存储刷新 UI 可见的进度百分比表
function refreshEpProgressUI(): void {
  try {
    const store = loadEpProgress()
    const out: Record<number, number> = {}
    for (let i = 0; i < deps.episodes().length; i++) {
      const k = epKeyOf(i)
      if (store[k]) out[i] = getEpProgressPct(store[k])
    }
    epProgressMap.value = out
  } catch { /* ignore */ }
}

// 播放开始时先执行一次（此时 episodes 可能为空，后面 loadData 会再刷一次）
refreshEpProgressUI()

function getEpWatchPct(idx: number): number {
  return epProgressMap.value[idx] ?? 0
}
function isWatchedEp(idx: number): boolean { return getEpWatchPct(idx) > 0 }

let lastHistorySyncAt = 0
const HISTORY_SYNC_INTERVAL_MS = 10000

function syncHistoryToDb(position: number, force = false, duration?: number, allowInitial = false): Promise<boolean> | undefined {
  // Explicit source handoffs may record the opening seconds; ordinary reopens retain saved progress.
  if (!Number.isFinite(position) || position < 0 || (!allowInitial && position <= 5)) return
  const now = Date.now()
  if (!force && now - lastHistorySyncAt < HISTORY_SYNC_INTERVAL_MS) return
  const identity = deps.identity()
  const index = deps.currentIndex()
  const ep = deps.episodes()[index]
  if (!identity.sourceKey || !identity.vodId || index < 0 || !ep) return
  lastHistorySyncAt = now
  historyWrites++
  return SaveWatchHistory({
    source_key: identity.sourceKey,
    vod_id: String(identity.vodId),
    vod_name: identity.name || '',
    ep_num: ep.ep_num ?? (index + 1),
    position, duration: Number.isFinite(duration) ? duration : undefined,
  } as any).then(() => true).catch(error => { deps.onPersistenceError?.(error); return false })
}

/**
 * 播放器恢复进度兜底：localStorage 按 origin 分区，独立 exe 与 dev 端口互不可见，
 * 本地没有记录时以数据库 watch_history 为准。
 */
async function resolveDbResumePosition(): Promise<number> {
  const idx = deps.currentIndex()
  const ep = deps.episodes()[idx]
  if (!deps.identity().sourceKey || !deps.identity().vodId || !ep) return 0
  try {
    return await GetHistoryPosition({
      source_key: deps.identity().sourceKey,
      vod_id: String(deps.identity().vodId),
      ep_num: ep.ep_num ?? (idx + 1),
    } as any) || 0
  } catch { return 0 }
}

// Persist precise progress while notifying views only when the visible percentage changes.
function updateCurrentEpProgress(position: number, duration?: number, force=false, allowInitial=false): Promise<boolean> | undefined {
  if (deps.currentIndex() < 0 || !Number.isFinite(position) || position < 0) return
  progressEvents++
  const k = epKeyOf(deps.currentIndex())
  saveEpProgress(k, position, duration)
  const saved = syncHistoryToDb(position, force, duration, allowInitial)
  // 同时同步 UI
  const store = loadEpProgress()
  const index = deps.currentIndex()
  const percent = getEpProgressPct(store[k])
  if (epProgressMap.value[index] !== percent) {
    epProgressMap.value[index] = percent
    progressUIUpdates++
  } else return saved
  // 派发自定义事件，供详情页等其他页面监听以实时同步已观看状态
  try {
    window.dispatchEvent(new CustomEvent('cczj-ep-progress-updated', {
      detail: { key: k, position, duration },
    }))
  } catch { /* ignore */ }
  return saved
}


return { epKeyOf, refreshEpProgressUI, getEpWatchPct, isWatchedEp,
  syncHistoryToDb, resolveDbResumePosition, updateCurrentEpProgress,
  snapshot: () => ({progress_events:progressEvents,progress_ui_updates:progressUIUpdates,history_writes:historyWrites}) }
}
