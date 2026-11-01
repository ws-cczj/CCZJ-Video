import { CCZJ } from '../constants/cczj'
import { readStorageRecord } from '../platform/storage'
import { createDebouncedWriter } from '../platform/debouncedWrite'

const imageProxyCache = new Map<string, string>()
const imageProxyPending = new Map<string, Promise<string>>()
// A failed proxy attempt should not fall back to the remote URL: doing so lets
// every <img> trigger another CDN request and surface a noisy 418. Keep a
// short failure cooldown so a later mount can recover from transient outages.
const imageProxyFallbackCache = new Map<string, number>()
const IMAGE_PROXY_STORAGE_KEY = CCZJ.Storage.imageProxyCache
// 与海报缓存同寿命：键是原始 URL，同一个 URL 的代理结果不会变，过期只会带来一次重取。
// 而且重取现在不出网：Go 侧还有一层按 URL 记账的磁盘副本（app/proxy/imagestore.go），
// 这里被 LRU 挤掉的那 80 条只是省掉一次 IPC，不再是省掉一次 CDN 请求。
const IMAGE_PROXY_TTL = 30 * 24 * 60 * 60 * 1000
const IMAGE_PROXY_MAX = 80
// 条数上限挡不住体积：一条 data URL 几十到几百 KB，80 条就是好几 MB。整个源的
// localStorage 额度只有 ~10MB（Chromium 按 UTF-16 记账，实际还要翻倍），实测有用户
// 单这一个键就长到 4.97MB，把额度吃满后所有 setItem 一起抛 QuotaExceededError。
// 症状落在毫不相干的功能上：新建收藏夹说成功了，重启后什么都没有。
const IMAGE_PROXY_MAX_BYTES = 1024 * 1024
// 内存层以前不设上限，整场会话看过的 data URL 全留着（单条几十~几百 KB），是实打实的内存泄漏。
// 这里同样封顶：被挤掉的条目还能从持久副本读回，最多回 Go 磁盘缓存重取一次。
const IMAGE_PROXY_MEMORY_MAX = 80
const IMAGE_PROXY_MEMORY_MAX_BYTES = 8 * 1024 * 1024
let imageProxyMemoryBytes = 0
const IMAGE_PROXY_FAILURE_MAX = 256
const IMAGE_PROXY_FAILURE_TTL = 5 * 60 * 1000

interface StoredImageProxy { value: string; accessed: number }

// 这张 map 存的是 data URL，所以必须按字节钉住（见 IMAGE_PROXY_MAX_BYTES）。
// 以前每读一个新 URL 都要把整张 parse 一遍、再为了刷新 accessed 把整张 stringify 写回去
// ——首屏铺几十张海报就是几十轮全量序列化。现在常驻内存一份，落盘合并成防抖一次。
let memStoredProxy: Record<string, StoredImageProxy> | null = null

function proxyEntrySize(entry: StoredImageProxy): number {
  // data URL 是 base64，字符数即字节数量级；键本身也占额度。
  return entry.value.length + 64
}

// 两个上限一起收敛：先按条数，再按字节，都从最久未访问的冷条目开始丢。
function trimStoredProxies(all: Record<string, StoredImageProxy>): boolean {
  const keys = Object.keys(all)
  if (keys.length <= IMAGE_PROXY_MAX) {
    let bytes = 0
    for (const k of keys) bytes += proxyEntrySize(all[k])
    if (bytes <= IMAGE_PROXY_MAX_BYTES) return false
  }
  keys.sort((a, b) => all[a].accessed - all[b].accessed)
  let dropped = false
  while (keys.length > IMAGE_PROXY_MAX) {
    delete all[keys.shift()!]
    dropped = true
  }
  let bytes = 0
  for (const k of keys) bytes += proxyEntrySize(all[k])
  while (bytes > IMAGE_PROXY_MAX_BYTES && keys.length > 0) {
    const k = keys.shift()!
    bytes -= proxyEntrySize(all[k])
    delete all[k]
    dropped = true
  }
  return dropped
}

function storedProxies(): Record<string, StoredImageProxy> {
  if (!memStoredProxy) {
    memStoredProxy = readStorageRecord(IMAGE_PROXY_STORAGE_KEY, (value): value is StoredImageProxy => {
      if (!value || typeof value !== 'object') return false
      const row = value as StoredImageProxy
      return typeof row.value === 'string' && row.value.startsWith('data:') && Number.isFinite(row.accessed)
    })
    // 载入即收敛：已经攒肥的老副本会在下一次防抖写入时缩水，额度随之释放，
    // 被挤掉的其他写入（收藏夹、设置）才能重新落盘。不需要用户手动清缓存。
    if (trimStoredProxies(memStoredProxy)) persistStoredProxy.schedule()
  }
  return memStoredProxy
}

const persistStoredProxy = createDebouncedWriter(IMAGE_PROXY_STORAGE_KEY, storedProxies)

// 诊断页要回答「海报到底有没有走缓存」，所以这里记下三类读数：内存命中、持久命中、真的出网。
// Go 侧的出网账本只看得见最后那一类（命中缓存时根本不发请求），两边合起来才是完整画面。
const imageProxyCounters = { memoryHits: 0, storedHits: 0, fetches: 0, failures: 0 }

export function imageProxyStats(): {
  entries: number; memoryHits: number; storedHits: number; fetches: number; failures: number;
  bytes: number; limitBytes: number; failureEntries: number;
} {
  return {
    entries: imageProxyCache.size,
    // Conservative UTF-16 string/key estimate, excluding decoded pixels/GPU.
    bytes: imageProxyMemoryBytes,
    limitBytes: IMAGE_PROXY_MEMORY_MAX_BYTES,
    failureEntries: imageProxyFallbackCache.size,
    memoryHits: imageProxyCounters.memoryHits,
    storedHits: imageProxyCounters.storedHits,
    fetches: imageProxyCounters.fetches,
    failures: imageProxyCounters.failures,
  }
}

function readStoredImageProxy(url: string): string {
  const all = storedProxies()
  const item = all[url]
  if (!item || Date.now() - item.accessed > IMAGE_PROXY_TTL) return ''
  item.accessed = Date.now()
  persistStoredProxy.schedule()
  return item.value
}

function writeStoredImageProxy(url: string, value: string): void {
  // 单条超出持久预算不落盘；内存层也有独立的字节预算。
  if (value.length > IMAGE_PROXY_MAX_BYTES) return
  const all = storedProxies()
  all[url] = { value, accessed: Date.now() }
  trimStoredProxies(all)
  persistStoredProxy.schedule()
}

// Map 插入序即访问序：新写入落尾部，命中时由调用方 delete+set 挪回尾部，队首永远是最久没用的。
// 条数或字符串字节超预算时从队首挤掉，单张超大图只交给当前消费者。
function rememberProxiedImage(url: string, value: string): void {
  const bytes = (url.length + value.length) * 2
  if (bytes > IMAGE_PROXY_MEMORY_MAX_BYTES) return
  const previous = imageProxyCache.get(url)
  if (previous !== undefined) {
    imageProxyMemoryBytes -= (url.length + previous.length) * 2
    imageProxyCache.delete(url)
  }
  imageProxyCache.set(url, value)
  imageProxyMemoryBytes += bytes
  while (imageProxyCache.size > IMAGE_PROXY_MEMORY_MAX || imageProxyMemoryBytes > IMAGE_PROXY_MEMORY_MAX_BYTES) {
    const oldest = imageProxyCache.keys().next().value
    if (oldest === undefined) break
    imageProxyMemoryBytes -= (oldest.length + imageProxyCache.get(oldest)!.length) * 2
    imageProxyCache.delete(oldest)
  }
}

export async function getProxiedImageUrl(originalUrl: string): Promise<string> {
  if (!originalUrl) return ''
  const cached = imageProxyCache.get(originalUrl)
  if (cached !== undefined) {
    imageProxyCounters.memoryHits++
    // 命中即移到尾部，维持「队首=最久未用」，让上面的容量上限淘汰的是真正的冷条目
    imageProxyCache.delete(originalUrl)
    imageProxyCache.set(originalUrl, cached)
    return cached
  }
  const stored = readStoredImageProxy(originalUrl)
  if (stored) {
    imageProxyCounters.storedHits++
    rememberProxiedImage(originalUrl, stored)
    return stored
  }
  const failedAt = imageProxyFallbackCache.get(originalUrl)
  if (failedAt && Date.now() - failedAt < IMAGE_PROXY_FAILURE_TTL) return ''
  if (failedAt) imageProxyFallbackCache.delete(originalUrl)
  const pending = imageProxyPending.get(originalUrl)
  if (pending) return pending

  const request = (async () => {
    imageProxyCounters.fetches++
    try {
      const { ProxyImage } = await import('../api/app')
      const result = await ProxyImage(originalUrl)
      if (result && result.startsWith('data:')) {
        rememberProxiedImage(originalUrl, result)
        writeStoredImageProxy(originalUrl, result)
        return result
      }
    } catch { }
    imageProxyCounters.failures++
    imageProxyFallbackCache.set(originalUrl, Date.now())
    while (imageProxyFallbackCache.size > IMAGE_PROXY_FAILURE_MAX) {
      imageProxyFallbackCache.delete(imageProxyFallbackCache.keys().next().value!)
    }
    return ''
  })()
  imageProxyPending.set(originalUrl, request)
  try {
    return await request
  } finally {
    imageProxyPending.delete(originalUrl)
  }
}

