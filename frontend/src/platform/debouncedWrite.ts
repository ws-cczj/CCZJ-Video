import { writeStorage } from './storage'

export interface DebouncedWriter {
  schedule(): void
  flush(): void
  cancel(): void
}

/**
 * 「整张 map 存进一个 localStorage 键」的缓存共用的落盘时机。
 *
 * localStorage 是同步 API，一次写入要把整张 map 重新 stringify：详情那张装的是多线路
 * payload，海报和图片代理那两张装的是 data URL，几十到几百 KB。以前每读一条、每刷新
 * 一次访问时间都全量重写，首页铺满一屏就是几十次全量序列化，全砸在主线程上。
 * 这里合并成「改脏了、静下来再写一次」，并在窗口卸载前补写，关窗不会丢掉最后一段变更。
 *
 * 只管写入时机，不碰 map 的内容：TTL、条数上限、淘汰顺序仍由各自的缓存决定。
 * 同样的形状在 utils/episodeProgress.ts 里已经有一份，那份贴着播放进度自己的过期清理，
 * 没有并进来。
 */
export function createDebouncedWriter<T>(
  key: string,
  snapshot: () => T,
  debounceMs = 800,
): DebouncedWriter {
  let timer: number | null = null

  function write(): void {
    timer = null
    writeStorage(key, snapshot())
  }

  function schedule(): void {
    if (timer !== null) return
    timer = window.setTimeout(write, debounceMs)
  }

  function flush(): void {
    if (timer === null) return
    window.clearTimeout(timer)
    write()
  }

  // 调用方自己 removeStorage 时要用它：否则待写的定时器会把刚清掉的缓存又整张写回去。
  function cancel(): void {
    if (timer === null) return
    window.clearTimeout(timer)
    timer = null
  }

  // Wails 销毁 webview 时 beforeunload 可能早于组件卸载，这里自己兜住（同 episodeProgress）。
  if (typeof window !== 'undefined') {
    window.addEventListener('beforeunload', flush)
  }

  return { schedule, flush, cancel }
}
