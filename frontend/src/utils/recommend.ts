/**
 * 视频推荐算法 — 基于内容的多维度协同过滤
 *
 * 权重比例：演员 > 导演 > 类型 > 年份 > 地区
 * 所有标签字段拆分为规范数组后做交集比对，避免子串误匹配和复合值漏匹配。
 */

import type { Video } from '../types'

// ==================== 类型定义 ====================

export interface RecommendItem {
  vod_id: string
  vod_name: string
  vod_pic?: string
  vod_remarks?: string
  /** 匹配度分数 */
  score: number
  /** 综合匹配理由，例如 "同演员：刘德华、梁朝伟，同类型：动作，同年份：2023" */
  matchKey: string
}

// ==================== 数据清洗 ====================

/** 分隔符：中文逗号 / 英文逗号 / 斜杠 / 顿号 */
const SPLIT_RE = /[,，\/、]+/

/** 提取4位数字年份，无则返回 null */
export function extractYear(raw: string | undefined | null): number | null {
  if (!raw) return null
  const m = raw.match(/\b\d{4}\b/)
  if (!m) return null
  const n = parseInt(m[0], 10)
  return n >= 1900 && n <= 2100 ? n : null
}

/** 将标签字段拆分为去重、去空格、统一小写的数组 */
function splitTags(raw: string | undefined | null): string[] {
  if (!raw) return []
  return raw
    .split(SPLIT_RE)
    .map(s => s.trim().toLowerCase())
    .filter(Boolean)
}

/** 数组交集大小 */
function intersectCount(a: string[], b: string[]): number {
  if (a.length === 0 || b.length === 0) return 0
  const bSet = new Set(b)
  let count = 0
  for (const x of a) {
    if (bSet.has(x)) count++
  }
  return count
}

/** 数组交集值 */
function intersectValues(a: string[], b: string[]): string[] {
  if (a.length === 0 || b.length === 0) return []
  const bSet = new Set(b)
  return a.filter(x => bSet.has(x))
}

// ==================== 预处理缓存 ====================

interface NormalizedVideo {
  raw: Video
  actors: string[]
  directors: string[]
  types: string[]
  areas: string[]
  year: number | null
}

const normalizedCache = new WeakMap<Video, NormalizedVideo>()

/** 一次性清洗并缓存 */
function normalize(v: Video): NormalizedVideo {
  let n = normalizedCache.get(v)
  if (!n) {
    n = {
      raw: v,
      actors: splitTags(v.vod_actor),
      directors: splitTags(v.vod_director),
      types: splitTags(v.type_name),
      areas: splitTags(v.vod_area),
      year: extractYear(v.vod_year),
    }
    normalizedCache.set(v, n)
  }
  return n
}

// ==================== 核心算法 ====================

/**
 * 计算推荐列表
 * @param list      全量视频列表
 * @param currentId 当前视频 vod_id
 * @param currentName 当前视频名称（用于同名同年去重）
 * @param currentYear 当前视频年份（用于同名同年去重）
 */
export function computeRecommendations(
  list: Video[],
  currentId: string,
  currentName: string,
  currentYear: number | null,
): RecommendItem[] {
  // 1. 预处理当前视频
  const currentVideo = list.find(v => String(v.vod_id || '') === currentId)
  if (!currentVideo) return []

  const cur = normalize(currentVideo)

  // 2. 性能优化：大数据量时预筛选（至少有一个类型相同）
  const candidates = list.length > 1000 && cur.types.length > 0
    ? list.filter(v => {
        if (String(v.vod_id || '') === currentId) return false
        const n = normalize(v)
        return intersectCount(cur.types, n.types) > 0
      })
    : list

  const scored: { v: Video; n: NormalizedVideo; score: number; parts: string[] }[] = []

  const curNameLower = (currentName || '').toLowerCase()

  for (const v of candidates) {
    const vid = String(v.vod_id || '')
    if (vid === currentId) continue

    const n = normalize(v)

    // 自身排除：同名同年视为重复
    if (curNameLower && (v.vod_name || '').toLowerCase() === curNameLower) {
      if (currentYear !== null && n.year === currentYear) continue
      // 年份均为 null 也视为同类
      if (currentYear === null && n.year === null) continue
    }

    let score = 0
    const parts: string[] = []

    // --- 演员：+5/人 ---
    const actorMatches = intersectValues(cur.actors, n.actors)
    if (actorMatches.length > 0) {
      const names = actorMatches.slice(0, 3).join('、')
      const suffix = actorMatches.length > 3 ? '等' : ''
      parts.push(`同演员：${names}${suffix}`)
      score += actorMatches.length * 5
    }

    // --- 导演：+4/人 ---
    const dirMatches = intersectValues(cur.directors, n.directors)
    if (dirMatches.length > 0) {
      const names = dirMatches.slice(0, 3).join('、')
      const suffix = dirMatches.length > 3 ? '等' : ''
      parts.push(`同导演：${names}${suffix}`)
      score += dirMatches.length * 4
    }

    // --- 类型：+3/类 ---
    const typeMatches = intersectValues(cur.types, n.types)
    if (typeMatches.length > 0) {
      const names = typeMatches.slice(0, 3).join('、')
      const suffix = typeMatches.length > 3 ? '等' : ''
      parts.push(`同类型：${names}${suffix}`)
      score += typeMatches.length * 3
    }

    // --- 年份：完全相等 +2，±1 年 +1 ---
    if (cur.year !== null && n.year !== null) {
      if (cur.year === n.year) {
        parts.push(`同年份：${cur.year}`)
        score += 2
      } else if (Math.abs(cur.year - n.year) === 1) {
        parts.push(`相近年份：${cur.year} / ${n.year}`)
        score += 1
      }
    }

    // --- 地区：+1/地区 ---
    const areaMatches = intersectValues(cur.areas, n.areas)
    if (areaMatches.length > 0) {
      const names = areaMatches.slice(0, 3).join('、')
      const suffix = areaMatches.length > 3 ? '等' : ''
      parts.push(`同地区：${names}${suffix}`)
      score += areaMatches.length * 1
    }

    if (score > 0) {
      scored.push({ v, n, score, parts })
    }
  }

  // 3. 按得分降序排列
  scored.sort((a, b) => b.score - a.score)

  // 4. 输出结果
  return scored.map(s => ({
    vod_id: String(s.v.vod_id || ''),
    vod_name: s.v.vod_name || '视频',
    vod_pic: s.v.vod_pic,
    vod_remarks: s.v.vod_remarks,
    score: s.score,
    matchKey: s.parts.join('，'),
  }))
}