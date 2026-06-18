/* 每集播放进度独立存储
 * 结构： cczj_ep_prog_v1 -> {
 *   [key: `${sourceKey}-${vodId}-${epNum}`]: {
 *     position: number;     // 已播放秒数
 *     duration?: number;    // 总时长（秒）
 *     updatedAt: number;    // 写入时间戳 (ms)
 *   }
 * }
 * 规则：
 *   - 仅以 sourceKey + vodId + epNum 作为唯一键，同集多次播放覆盖旧记录。
 *   - 读取时自动清理超过 1 个月的条目（updatedAt < now - 30 天）。
 *   - 写入使用内存缓存 + 防抖，避免高频 timeupdate 事件造成读-改-写竞态。
 */

const PROG_KEY = 'cczj_ep_prog_v1'
const TTL_MS = 30 * 24 * 60 * 60 * 1000 // 30 天
const FLUSH_DEBOUNCE_MS = 1000 // 写入 localStorage 的防抖间隔

export interface EpProgressEntry {
  position: number
  duration?: number
  updatedAt: number
}

export type EpProgressStore = Record<string, EpProgressEntry>

// ==================== 内存缓存层 ====================
// 避免每次 timeupdate（~250ms 一次）都做 JSON.parse + JSON.stringify 的读-改-写循环。
// 改为：启动时加载一次到内存 → 写入时更新内存 → 防抖写回 localStorage。
let _cache: EpProgressStore | null = null
let _dirty = false
let _flushTimer: ReturnType<typeof setTimeout> | null = null

function _ensureCache(): EpProgressStore {
  if (_cache) return _cache
  try {
    const raw = localStorage.getItem(PROG_KEY)
    if (!raw) { _cache = {}; return _cache }
    const obj = JSON.parse(raw) as EpProgressStore
    if (!obj || typeof obj !== 'object') { _cache = {}; return _cache }
    _cache = obj
    return _cache
  } catch {
    _cache = {}
    return _cache
  }
}

function _flushToStorage(): void {
  if (!_dirty || !_cache) return
  try {
    // 淘汰过期条目后再写入
    const now = Date.now()
    const keys = Object.keys(_cache)
    for (const k of keys) {
      const v = _cache[k]
      if (!v || typeof v !== 'object') { delete _cache[k]; continue }
      if (now - (v.updatedAt || 0) > TTL_MS) { delete _cache[k]; continue }
    }
    // 始终写入（即使 _cache 为空也同步清理过期的 localStorage 数据）
    localStorage.setItem(PROG_KEY, JSON.stringify(_cache))
    _dirty = false // 仅在写入成功后才清除 dirty 标记，失败时下次 flush 重试
  } catch { /* 写入失败时保留 _dirty，下次 flush 会重试 */ }
}

function _scheduleFlush(): void {
  _dirty = true
  if (_flushTimer != null) return // 已有待执行的 flush
  _flushTimer = setTimeout(() => {
    _flushTimer = null
    _flushToStorage()
  }, FLUSH_DEBOUNCE_MS)
}

/** 立即强制写入（用于页面卸载前等场景） */
export function flushEpProgress(): void {
  if (_flushTimer != null) {
    clearTimeout(_flushTimer)
    _flushTimer = null
  }
  _flushToStorage()
}

/** 组装稳定 key（基于 global_id，跨源统一；fallback 到 vod_name） */
export function epProgressKey(globalId: number | undefined | null, vodName: string | undefined | null, epNum: string | number | undefined | null): string {
  const prefix = globalId ? String(globalId) : String(vodName ?? '')
  return `${prefix}-${String(epNum ?? '')}`
}

/** 读取并清理过期数据。始终返回对象（可能为空）。 */
export function loadEpProgress(): EpProgressStore {
  _ensureCache()
  // 每次读取时顺便清理过期条目（在内存中清理，防抖写回 localStorage）
  const now = Date.now()
  const cache = _cache!
  for (const k of Object.keys(cache)) {
    const v = cache[k]
    if (!v || typeof v !== 'object') { delete cache[k]; _dirty = true; continue }
    if (now - (v.updatedAt || 0) > TTL_MS) { delete cache[k]; _dirty = true; continue }
  }
  // 如果清理了过期数据，调度一次写入确保清理同步到 localStorage
  if (_dirty) _scheduleFlush()
  return cache
}

/** 写入单条进度（内存缓存 + 防抖写回 localStorage）。 */
export function saveEpProgress(
  key: string,
  position: number,
  duration?: number,
): void {
  try {
    const store = _ensureCache()
    const prev = store[key]
    const nextPos = Number(position) || 0
    const nextDur = duration && duration > 0 ? Number(duration) : (prev?.duration && prev.duration > 0 ? prev.duration : undefined)
    store[key] = {
      position: nextPos,
      duration: nextDur,
      updatedAt: Date.now(),
    }
    _scheduleFlush()
  } catch { /* ignore */ }
}

/** 读取单条进度（会做过期校验）。不存在返回 undefined。 */
export function getEpProgress(key: string): EpProgressEntry | undefined {
  const store = loadEpProgress()
  return store[key]
}

/** 计算进度百分比 0-100。position 有值但无 duration 时返回 15%（表示"看过一点"）。 */
export function getEpProgressPct(entry: EpProgressEntry | undefined): number {
  if (!entry) return 0
  const pos = Number(entry.position) || 0
  if (pos <= 0) return 0
  const dur = entry.duration && entry.duration > 0 ? Number(entry.duration) : 0
  if (dur > 0) {
    return Math.min(100, Math.max(0, (pos / dur) * 100))
  }
  return 15
}

/** 清除所有进度数据（内存 + localStorage）。 */
export function clearAllEpProgress(): void {
  _cache = {}
  _dirty = false
  if (_flushTimer != null) {
    clearTimeout(_flushTimer)
    _flushTimer = null
  }
  try { localStorage.removeItem(PROG_KEY) } catch { /* ignore */ }
}

// ==================== 窗口关闭兜底 ====================
// 在 Wails 环境中，webview 销毁时 beforeunload 可能早于 Vue 组件的 onBeforeUnmount，
// 在此注册全局监听确保进度一定被写入 localStorage。
if (typeof window !== 'undefined') {
  window.addEventListener('beforeunload', () => {
    flushEpProgress()
  })
}