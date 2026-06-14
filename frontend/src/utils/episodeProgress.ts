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
 */

const PROG_KEY = 'cczj_ep_prog_v1'
const TTL_MS = 30 * 24 * 60 * 60 * 1000 // 30 天

export interface EpProgressEntry {
  position: number
  duration?: number
  updatedAt: number
}

export type EpProgressStore = Record<string, EpProgressEntry>

/** 组装稳定 key */
export function epProgressKey(sourceKey: string | number | undefined | null, vodId: string | number | undefined | null, epNum: string | number | undefined | null): string {
  return `${String(sourceKey ?? '')}-${String(vodId ?? '')}-${String(epNum ?? '')}`
}

/** 读取并清理过期数据。始终返回对象（可能为空）。 */
export function loadEpProgress(): EpProgressStore {
  try {
    const raw = localStorage.getItem(PROG_KEY)
    if (!raw) return {}
    const obj = JSON.parse(raw) as EpProgressStore
    if (!obj || typeof obj !== 'object') return {}
    // 淘汰过期
    const now = Date.now()
    let dirty = false
    const out: EpProgressStore = {}
    for (const k of Object.keys(obj)) {
      const v = obj[k]
      if (!v || typeof v !== 'object') { dirty = true; continue }
      if (now - (v.updatedAt || 0) > TTL_MS) { dirty = true; continue }
      out[k] = v
    }
    if (dirty) {
      try { localStorage.setItem(PROG_KEY, JSON.stringify(out)) } catch { /* ignore */ }
    }
    return out
  } catch {
    return {}
  }
}

/** 写入单条进度。 */
export function saveEpProgress(
  key: string,
  position: number,
  duration?: number,
): void {
  try {
    const store = loadEpProgress()
    const prev = store[key]
    const nextPos = Number(position) || 0
    const nextDur = duration && duration > 0 ? Number(duration) : (prev?.duration && prev.duration > 0 ? prev.duration : undefined)
    store[key] = {
      position: nextPos,
      duration: nextDur,
      updatedAt: Date.now(),
    }
    localStorage.setItem(PROG_KEY, JSON.stringify(store))
  } catch { /* ignore */ }
}

/** 读取单条进度（会做过期校验）。不存在返回 undefined。 */
export function getEpProgress(key: string): EpProgressEntry | undefined {
  const store = loadEpProgress()
  return store[key]
}

/** 计算进度百分比 0-100。position 有值但无 duration 时返回 15%（表示“看过一点”）。 */
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

/** 清除所有进度数据。 */
export function clearAllEpProgress(): void {
  try { localStorage.removeItem(PROG_KEY) } catch { /* ignore */ }
}
