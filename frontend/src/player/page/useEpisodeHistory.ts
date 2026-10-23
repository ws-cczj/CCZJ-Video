import { ref } from 'vue'
import { GetHistoryPosition, SaveWatchHistory } from '../../api/app'
import { epProgressKey, loadEpProgress, saveEpProgress, getEpProgressPct } from '../../utils/episodeProgress'

export function useEpisodeHistory(deps: {
  identity: () => {sourceKey:string;vodId:string;name:string;globalId?:number}
  episodes: () => ReadonlyArray<{ep_num?:number}>
  currentIndex: () => number
}) {
let progressEvents = 0
let progressUIUpdates = 0
let historyWrites = 0
const epProgressMap = ref<Record<number, number>>({})

function epKeyOf(idx: number): string {
  return epProgressKey(deps.identity().globalId, deps.identity().name, deps.episodes()[idx]?.ep_num ?? idx)
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

function syncHistoryToDb(position: number, force = false): void {
  // 刚起播的 0~5 秒不写库：否则每次重看都会把上次记住的位置冲掉。
  if (position <= 5) return
  if (!force) {
    const now = Date.now()
    if (now - lastHistorySyncAt < HISTORY_SYNC_INTERVAL_MS) return
    lastHistorySyncAt = now
  }
  if (!deps.identity().vodId || deps.currentIndex() < 0) return
  const ep = deps.episodes()[deps.currentIndex()]
  if (!ep) return
  historyWrites++
  SaveWatchHistory({
    source_key: deps.identity().sourceKey,
    vod_id: String(deps.identity().vodId),
    vod_name: deps.identity().name || '',
    ep_num: ep.ep_num ?? (deps.currentIndex() + 1),
    position,
  } as any).catch(() => { })
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

// 写入当前集播放时持续写入独立存储（position/duration 会在用户播放过程中不断被写入
function updateCurrentEpProgress(position: number, duration?: number): void {
  if (deps.currentIndex() < 0) return
  progressEvents++
  const k = epKeyOf(deps.currentIndex())
  saveEpProgress(k, position, duration)
  syncHistoryToDb(position)
  // 同时同步 UI
  const store = loadEpProgress()
  const index = deps.currentIndex()
  const percent = getEpProgressPct(store[k])
  if (epProgressMap.value[index] !== percent) {
    epProgressMap.value[index] = percent
    progressUIUpdates++
  } else return // Persistence above retains full precision; UI notifications need only changed percentages.
  // 派发自定义事件，供详情页等其他页面监听以实时同步已观看状态
  try {
    window.dispatchEvent(new CustomEvent('cczj-ep-progress-updated', {
      detail: { key: k, position, duration },
    }))
  } catch { /* ignore */ }
}

function recordHistory(idx: number): void {
  if (!deps.identity().vodId) return
  const ep = deps.episodes()[idx]
  if (!ep) return
  const epNum = ep.ep_num ?? (idx + 1)
  const progKey = epProgressKey(deps.identity().globalId, deps.identity().name, epNum)
  const entry = loadEpProgress()[progKey]
  const position = entry?.position ?? 0
  // 本地没有进度时不写库，否则会把数据库里记住的位置清零。
  if (position <= 0) return
  lastHistorySyncAt = Date.now()
  try {
    historyWrites++
    SaveWatchHistory({
      source_key: deps.identity().sourceKey,
      vod_id: String(deps.identity().vodId),
      vod_name: deps.identity().name || '',
      ep_num: epNum,
      position,
    } as any).catch(() => { })
  } catch { /* ignore */ }
}


return { epKeyOf, refreshEpProgressUI, getEpWatchPct, isWatchedEp,
  syncHistoryToDb, resolveDbResumePosition, updateCurrentEpProgress, recordHistory,
  snapshot: () => ({progress_events:progressEvents,progress_ui_updates:progressUIUpdates,history_writes:historyWrites}) }
}
