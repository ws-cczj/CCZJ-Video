/* 每集播放进度独立存储
 * 结构： cczj_ep_prog_v1 -> {
 *   [key: `${globalId || vodName}-${epNum}`]: {
 *     position: number;     // 已播放秒数
 *     duration?: number;    // 总时长（秒）
 *     updatedAt: number;    // 写入时间戳 (ms)
 *   }
 * }
 * 规则：
 *   - 已关联视频按 globalId + epNum 共享缓存；旧数据兼容名称键。
 *   - SQLite watch_history 保留持久播放位置；此缓存补充近期百分比与时长。
 *   - 读取时自动清理超过 1 个月的条目（updatedAt < now - 30 天）。
 *   - 写入使用内存缓存 + 防抖，避免高频 timeupdate 事件造成读-改-写竞态。
 */

import { readStorage, removeStorage, writeStorage } from '../platform/storage'

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

function isProgressEntry(value: unknown): value is EpProgressEntry {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return false
  return 'position' in value && typeof value.position === 'number' && Number.isFinite(value.position) && value.position >= 0
    && 'updatedAt' in value && typeof value.updatedAt === 'number' && Number.isFinite(value.updatedAt) && value.updatedAt > 0
    && (!('duration' in value) || value.duration === undefined || (typeof value.duration === 'number' && Number.isFinite(value.duration) && value.duration > 0))
}

function _pruneExpired(): void {
  if (!_cache) return
  const now = Date.now()
  for (const k of Object.keys(_cache)) {
    const v = _cache[k]
    if (!isProgressEntry(v)) { delete _cache[k]; _dirty = true; continue }
    if (now - (v.updatedAt || 0) > TTL_MS) { delete _cache[k]; _dirty = true; continue }
  }
}

function _ensureCache(): EpProgressStore {
  if (_cache) return _cache
  const stored = readStorage<unknown>(PROG_KEY, {})
  const cache: EpProgressStore = Object.create(null)
  if (stored && typeof stored === 'object' && !Array.isArray(stored)) {
    for (const [key, value] of Object.entries(stored)) {
      if (isProgressEntry(value)) cache[key] = { position: value.position, duration: value.duration, updatedAt: value.updatedAt }
      else _dirty = true
    }
  } else _dirty = true
  _cache = cache
  // 整表扫描只在首次载入做一次；之后的过期判定走 getEpProgress 的逐键 O(1) 比较。
  // TTL 是 30 天，一条过期记录多活到下一次写回才消失没有影响。
  _pruneExpired()
  if (_dirty) _scheduleFlush()
  return _cache
}

function _flushToStorage(): void {
  if (!_dirty || !_cache) return
  try {
    _pruneExpired()
    // 始终写入（即使 _cache 为空也同步清理过期的 localStorage 数据）
    if (writeStorage(PROG_KEY, _cache)) {
      _dirty = false // 配额写满时保留待写状态，下一次更新仍可重试。
    }
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

/** 读取全部进度（内存镜像，过期条目已在首次载入时清掉）。始终返回对象（可能为空）。 */
export function loadEpProgress(): EpProgressStore {
  return _ensureCache()
}

/** 写入单条进度（内存缓存 + 防抖写回 localStorage）。 */
export function saveEpProgress(
  key: string,
  position: number,
  duration?: number,
): void {
  if (!key || !Number.isFinite(position) || position < 0) return
  try {
    const store = _ensureCache()
    const prev = store[key]
    const nextPos = position
    const nextDur = duration !== undefined && Number.isFinite(duration) && duration > 0 ? duration : prev?.duration
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
  const entry = loadEpProgress()[key]
  if (!isProgressEntry(entry)) return undefined
  if (Date.now() - (entry.updatedAt || 0) > TTL_MS) return undefined
  return entry
}

/** 计算进度百分比 0-100。条目存在即表示"已开始观看"，position=0 时返回 1%；position>0 但无 duration 时返回 15%。 */
export function getEpProgressPct(entry: EpProgressEntry | undefined): number {
  if (!entry) return 0
  const pos = Number(entry.position) || 0
  // 条目存在但 position=0：用户点开过但还没播放，标记为 1%
  if (pos <= 0) return 1
  const dur = entry.duration && entry.duration > 0 ? Number(entry.duration) : 0
  if (dur > 0) {
    return Math.round(Math.min(100, Math.max(1, (pos / dur) * 100)))
  }
  return 15
}

/** 清除所有进度数据（内存 + localStorage）。 */
export function clearAllEpProgress(): void {
  _cache = Object.create(null)
  _dirty = false
  if (_flushTimer != null) {
    clearTimeout(_flushTimer)
    _flushTimer = null
  }
  removeStorage(PROG_KEY)
}

// ==================== 窗口关闭兜底 ====================
// 在 Wails 环境中，webview 销毁时 beforeunload 可能早于 Vue 组件的 onBeforeUnmount，
// 在此注册全局监听确保进度一定被写入 localStorage。
if (typeof window !== 'undefined') {
  window.addEventListener('beforeunload', () => {
    flushEpProgress()
  })
}
