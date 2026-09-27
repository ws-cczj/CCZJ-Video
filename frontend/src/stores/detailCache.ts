import * as AppMod from '../api/app'
import { readStorage, writeStorage } from '../platform/storage'
import type { Episode, Video, VideoDetailResponse } from '../types'

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

// v2: 详情响应新增了 global_video 豆瓣字段回填（vod_douban_id 等），旧缓存缺字段需整体失效
const STORAGE_KEY = 'cczj_detail_cache_v2'
const MAX_ENTRIES = 20
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

function readEntries(): Record<string, StoredDetailEntry> {
  const entries = readStorage<Record<string, StoredDetailEntry>>(STORAGE_KEY, {})
  return entries && typeof entries === 'object' ? entries : {}
}

function saveEntries(entries: Record<string, StoredDetailEntry>): void {
  writeStorage(STORAGE_KEY, entries)
}

function hasLargeM3u8(response: VideoDetailResponse, rawSize: number): boolean {
  const urls = (response.episodes || [])
    .map((ep) => String(ep.ep_url || ep.ep_down_url || ''))
    .filter((url) => /\.m3u8(?:\?|$)/i.test(url) || url.length >= LARGE_M3U8_URL)
  const total = urls.reduce((sum, url) => sum + url.length, 0)
  return rawSize >= LARGE_JSON_SIZE || urls.some((url) => url.length >= LARGE_M3U8_URL) || total >= LARGE_M3U8_TOTAL
}

async function encode(response: VideoDetailResponse): Promise<StoredDetailEntry> {
  const payload = JSON.stringify({
    video: response.video || null,
    episodes: Array.isArray(response.episodes) ? response.episodes : [],
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
    const parsed = JSON.parse(json) as { video?: Video | null; episodes?: Episode[] }
    return {
      video: parsed?.video || null,
      episodes: Array.isArray(parsed?.episodes) ? parsed.episodes : [],
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
  const entries = readEntries()
  const key = makeKey(sourceKey, vodId, globalId)
  const entry = entries[key]
  if (!entry) return null

  const detail = await decode(entry)
  if (!detail?.video) {
    delete entries[key]
    saveEntries(entries)
    return null
  }

  // A hit refreshes recency and is therefore the LRU signal.
  entry.accessedAt = Date.now()
  saveEntries(entries)
  return detail
}

export async function writeDetailCache(
  sourceKey: string,
  vodId: string,
  response: VideoDetailResponse,
  globalId = 0,
): Promise<void> {
  if (!sourceKey || (!vodId && !globalId) || !response.video) return
  const entries = readEntries()
  entries[makeKey(sourceKey, vodId, globalId)] = await encode(response)
  trim(entries)
  saveEntries(entries)
}

