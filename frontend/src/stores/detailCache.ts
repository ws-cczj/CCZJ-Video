import * as AppMod from '../api/app'
import { readStorage } from '../platform/storage'
import { createDebouncedWriter } from '../platform/debouncedWrite'
import { normalizePlayLines } from '../utils/playLines'
import type { Episode, PlayLine, Video, VideoDetailResponse } from '../types'

/**
 * The detail cache is intentionally separate from the Pinia video store:
 * navigating between routes must not accidentally turn a stale store value
 * into a network request or overwrite another video's entry.
 */
interface StoredDetailEntry {
  accessedAt: number
  cachedAt: number
  format: 'json' | 'br'
  chunks: string[]
}

// v3: 详情新增了 $$$ 多线路拆分后的 lines，且 v2 条目里的集表是旧解析器把整条多线路
// 串当一集存下来的脏地址，必须整体失效而不是继续复用。
const STORAGE_KEY = 'cczj_detail_cache_v3'
// 一条多线路影片的条目现在装着每线路的完整集表，体积按线路数翻倍；条目数相应下调，
// 否则超出浏览器配额时 writeStorage 只会静默丢缓存。
const MAX_ENTRIES = 12
const CHUNK_SIZE = 16 * 1024
const LARGE_JSON_SIZE = 64 * 1024
const LARGE_M3U8_URL = 2048
const LARGE_M3U8_TOTAL = 8 * 1024

function makeKey(sourceKey: string, vodId: string, globalId = 0): string {
  const id = vodId || (globalId > 0 ? `global:${globalId}` : '')
  return `${sourceKey}:${id}`
}

function splitIntoChunks(value: string): string[] {
  const chunks: string[] = []
  for (let i = 0; i < value.length; i += CHUNK_SIZE) {
    chunks.push(value.slice(i, i + CHUNK_SIZE))
  }
  return chunks.length > 0 ? chunks : ['']
}

async function yieldToUi(): Promise<void> {
  await new Promise<void>((resolve) => setTimeout(resolve, 0))
}

async function joinChunks(chunks: string[]): Promise<string> {
  let value = ''
  for (let i = 0; i < chunks.length; i++) {
    value += chunks[i]
    // Joining in slices prevents a large compressed m3u8 payload from
    // monopolising the WebView event loop while the detail page is mounting.
    if ((i + 1) % 8 === 0) await yieldToUi()
  }
  return value
}

// map 常驻内存，只在第一次用到时解析一次；落盘合并成防抖一次。见 createDebouncedWriter：
// 这张 map 装的是最多 12 条多线路详情 payload，以前每次命中都要整张 parse + stringify，
// 而那正好落在详情页/播放页挂载的主线程上。代价是这些 payload 会占着内存到关窗为止，
// 上限由 MAX_ENTRIES 和 localStorage 配额一起兜住。
let memEntries: Record<string, StoredDetailEntry> | null = null

function detailEntries(): Record<string, StoredDetailEntry> {
  if (!memEntries) {
    const stored = readStorage<Record<string, StoredDetailEntry>>(STORAGE_KEY, {})
    memEntries = stored && typeof stored === 'object' ? stored : {}
  }
  return memEntries
}

const persistDetails = createDebouncedWriter(STORAGE_KEY, detailEntries)

function hasLargeM3u8(response: VideoDetailResponse, rawSize: number): boolean {
  const urls = normalizePlayLines(response)
    .flatMap((line) => line.episodes)
    .map((ep) => String(ep.ep_url || ''))
    .filter((url) => /\.m3u8(?:\?|$)/i.test(url) || url.length >= LARGE_M3U8_URL)
  const total = urls.reduce((sum, url) => sum + url.length, 0)
  return rawSize >= LARGE_JSON_SIZE || urls.some((url) => url.length >= LARGE_M3U8_URL) || total >= LARGE_M3U8_TOTAL
}

async function encode(response: VideoDetailResponse): Promise<StoredDetailEntry> {
  // 只存 lines：episodes 本来就是首条线路的集表，两份都写会让多线路影片的缓存翻倍。
  const payload = JSON.stringify({
    video: response.video || null,
    lines: normalizePlayLines(response),
  })
  const now = Date.now()

  if (hasLargeM3u8(response, payload.length)) {
    try {
      const compress = (AppMod as any).CompressDetailJSONBrotli
      if (typeof compress === 'function') {
        const compressed = await compress(payload)
        if (compressed && String(compressed).length < payload.length) {
          return { accessedAt: now, cachedAt: now, format: 'br', chunks: splitIntoChunks(String(compressed)) }
        }
      }
    } catch {
      // A cache must never make detail loading fail. Store plain JSON below.
    }
  }

  return { accessedAt: now, cachedAt: now, format: 'json', chunks: splitIntoChunks(payload) }
}

async function decode(entry: StoredDetailEntry): Promise<VideoDetailResponse | null> {
  const joined = await joinChunks(entry.chunks || [])
  let json = joined
  if (entry.format === 'br') {
    try {
      const decompress = (AppMod as any).DecompressDetailJSONBrotli
      if (typeof decompress !== 'function') return null
      json = String(await decompress(joined))
    } catch {
      return null
    }
  }

  try {
    const parsed = JSON.parse(json) as { video?: Video | null; lines?: PlayLine[]; episodes?: Episode[] }
    const lines = normalizePlayLines(parsed)
    return {
      video: parsed?.video || null,
      episodes: lines[0]?.episodes ?? [],
      lines,
    }
  } catch {
    return null
  }
}

function trim(entries: Record<string, StoredDetailEntry>): void {
  const keys = Object.keys(entries)
  if (keys.length <= MAX_ENTRIES) return
  keys
    .sort((a, b) => (entries[a].accessedAt || 0) - (entries[b].accessedAt || 0))
    .slice(0, keys.length - MAX_ENTRIES)
    .forEach((key) => delete entries[key])
}

export async function readDetailCache(
  sourceKey: string,
  vodId: string,
  globalId = 0,
): Promise<VideoDetailResponse | null> {
  if (!sourceKey || (!vodId && !globalId)) return null
  const entries = detailEntries()
  const key = makeKey(sourceKey, vodId, globalId)
  const entry = entries[key]
  if (!entry) return null

  const detail = await decode(entry)
  if (!detail?.video) {
    delete entries[key]
    persistDetails.schedule()
    return null
  }

  // A hit refreshes recency and is therefore the LRU signal.
  entry.accessedAt = Date.now()
  persistDetails.schedule()
  return detail
}

export async function writeDetailCache(
  sourceKey: string,
  vodId: string,
  response: VideoDetailResponse,
  globalId = 0,
): Promise<void> {
  if (!sourceKey || (!vodId && !globalId) || !response.video) return
  const entries = detailEntries()
  entries[makeKey(sourceKey, vodId, globalId)] = await encode(response)
  trim(entries)
  persistDetails.schedule()
}

/**
 * 让某个源的详情缓存作废：vodIds 给定时只删这些条目，不给定时删掉该源全部条目。
 *
 * 键是 `source_key:vod_id`（vod_id 缺失时才是 `source_key:global:<id>`），所以按前缀
 * 切能干净地把一个源摘掉。返回值用于日志，调用方不依赖它。
 */
export function dropDetailCache(sourceKey: string, vodIds?: string[]): number {
  if (!sourceKey) return 0
  const entries = detailEntries()
  const keys = vodIds?.length
    ? vodIds.map((vodId) => makeKey(sourceKey, vodId))
    : Object.keys(entries).filter((key) => key.startsWith(`${sourceKey}:`))
  let removed = 0
  for (const key of keys) {
    if (delete entries[key]) removed++
  }
  if (removed > 0) persistDetails.schedule()
  return removed
}

