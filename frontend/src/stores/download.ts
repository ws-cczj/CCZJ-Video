import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { Events } from '@wailsio/runtime'
import { GetSetting, SetSetting } from '../../bindings/cczjVideo/app'

const DIR_STORAGE_KEY = 'cczj_download_dir'

// 运行时动态访问 Go 绑定（避免未生成的 TS 类型声明）
function getApp(): any {
  const w = window as any
  return w?.go?.main?.App || null
}

function safeCall(name: string, args?: any[]): any {
  const app = getApp()
  if (!app || typeof app[name] !== 'function') {
    throw new Error(`Go binding not available: ${name}`)
  }
  return args !== undefined ? app[name](...args) : app[name]()
}

export interface ChunkProgress {
  id: number
  start: number
  end: number
  done: number
}

export interface DownloadTask {
  task_id: string
  url: string
  filename: string
  save_path: string
  total: number
  downloaded: number
  speed_bps: number
  eta_sec: number
  status: 'queued' | 'downloading' | 'paused' | 'done' | 'error' | 'cancelled'
  error?: string
  start_time: number
  end_time?: number
  vod_name?: string
  ep_name?: string
  source_key?: string
  vod_id?: string
  ep_num?: number
  chunks?: ChunkProgress[]
}

export const useDownloadStore = defineStore('download', () => {
  const tasks = ref<DownloadTask[]>([])
  const dir = ref<string>('')
  let _off: (() => void) | null = null
  let _inited = false

  function upsert(raw: any): void {
    if (!raw) return
    const t: DownloadTask = {
      task_id: raw.task_id ?? raw.TaskId ?? '',
      url: raw.url ?? raw.Url ?? '',
      filename: raw.filename ?? raw.Filename ?? '',
      save_path: raw.save_path ?? raw.SavePath ?? '',
      total: Number(raw.total ?? raw.Total ?? 0),
      downloaded: Number(raw.downloaded ?? raw.Downloaded ?? 0),
      speed_bps: Number(raw.speed_bps ?? raw.SpeedBps ?? 0),
      eta_sec: Number(raw.eta_sec ?? raw.EtaSec ?? 0),
      status: (raw.status ?? raw.Status ?? 'queued') as any,
      error: raw.error ?? raw.Error,
      start_time: Number(raw.start_time ?? raw.StartTime ?? 0),
      end_time: raw.end_time ?? raw.EndTime ?? undefined,
      chunks: raw.chunks ?? raw.Chunks ?? undefined,
    }
    // ⭐ O(n) 优化：使用 findIndex + splice 代替 filter 重建数组
    const idx = tasks.value.findIndex((x) => x.task_id === t.task_id)
    if (idx >= 0) {
      t.vod_name = tasks.value[idx].vod_name
      t.ep_name = tasks.value[idx].ep_name
      t.source_key = tasks.value[idx].source_key
      t.vod_id = tasks.value[idx].vod_id
      t.ep_num = tasks.value[idx].ep_num
      tasks.value.splice(idx, 1)
    }
    tasks.value.unshift(t)
  }

  async function init(): Promise<void> {
    if (_inited) return
    _inited = true

    // 1) 先从应用设置载入自定义目录（Go 后端的持久化设置）
    try {
      const saved = await GetSetting('download_dir')
      if (saved && typeof saved === 'string') {
        try {
          const ret = await safeCall('SetDownloadDir', [saved])
          if (typeof ret === 'string' && ret) dir.value = ret
        } catch {
          dir.value = saved
        }
      }
    } catch { /* 忽略 */ }

    // 2) 如未设置，从 localStorage 载入
    if (!dir.value) {
      try {
        const saved = localStorage.getItem(DIR_STORAGE_KEY)
        if (saved) {
          try {
            const ret = await safeCall('SetDownloadDir', [saved])
            if (typeof ret === 'string' && ret) dir.value = ret
          } catch {
            dir.value = saved
          }
        }
      } catch { /* 忽略 */ }
    }

    // 3) 从后端获取默认目录（兜底）
    if (!dir.value) {
      try {
        const d = await safeCall('GetDownloadDir')
        if (typeof d === 'string' && d) dir.value = d
      } catch {
        // 忽略
      }
    }

    // 4) 历史任务列表
    try {
      const all = await safeCall('ListDownloads')
      if (Array.isArray(all)) {
        for (const item of all) upsert(item)
      }
    } catch {
      // 忽略
    }

    if (Events) {
      try {
        _off = Events.On('download:progress', (ev: any) => {
          const data = ev.data
          upsert(data)
        }) as unknown as (() => void) | null
      } catch {
        // 忽略
      }
    }
  }

  function cleanup(): void {
    if (typeof _off === 'function') {
      try {
        _off()
      } catch {
        // 忽略
      }
    }
    _off = null
    _inited = false
  }

  const hasActive = computed(() =>
    tasks.value.some((t) => t.status === 'queued' || t.status === 'downloading' || t.status === 'paused'),
  )
  const activeCount = computed(
    () =>
      tasks.value.filter((t) => t.status === 'queued' || t.status === 'downloading' || t.status === 'paused')
        .length,
  )

  async function setDir(newDir: string): Promise<void> {
    newDir = (newDir || '').trim()
    try {
      // 1) 推送到 Go 后端（设置进程内的 customDownloadDir）
      try {
        const ret = await safeCall('SetDownloadDir', [newDir])
        if (typeof ret === 'string' && ret) dir.value = ret
      } catch {
        // 忽略
      }
      // 2) 持久化到应用设置（下次启动可恢复）
      try {
        await SetSetting('download_dir', newDir)
      } catch {
        // 忽略
      }
      // 3) localStorage 作为兜底
      try {
        if (newDir) localStorage.setItem(DIR_STORAGE_KEY, newDir)
        else localStorage.removeItem(DIR_STORAGE_KEY)
      } catch {
        // 忽略
      }
    } catch (e: any) {
      throw new Error(e?.message || '设置下载目录失败')
    }
  }

  async function startDownload(opts: {
    url: string
    filename: string
    vod_name?: string
    ep_name?: string
    source_key?: string
    vod_id?: string
    ep_num?: number
    force?: boolean
  }): Promise<string> {
    const id = 'dl_' + Date.now() + '_' + Math.random().toString(36).slice(2, 8)
    const initTask: DownloadTask = {
      task_id: id,
      url: opts.url,
      filename: opts.filename,
      save_path: '',
      total: 0,
      downloaded: 0,
      speed_bps: 0,
      eta_sec: 0,
      status: 'queued',
      start_time: Math.floor(Date.now() / 1000),
      vod_name: opts.vod_name,
      ep_name: opts.ep_name,
      source_key: opts.source_key,
      vod_id: opts.vod_id,
      ep_num: opts.ep_num,
    }
    tasks.value = [initTask, ...tasks.value.filter((x) => x.task_id !== id)]
    try {
      const res = await safeCall('StartVideoDownload', [
        {
          task_id: id,
          url: opts.url,
          filename: opts.filename,
          save_dir: dir.value || '',
          force: opts.force || false,
        },
      ])
      if (res) upsert(res)
    } catch (e: any) {
      const msg: string = e?.message || String(e)
      const isDuplicate = msg.startsWith('duplicate_url:')
      // 重复下载：移除临时任务并抛出错误让调用方决定
      tasks.value = tasks.value.filter((x) => x.task_id !== id)
      if (isDuplicate) {
        throw new Error(msg)
      }
      // 其他错误：保留任务记录错误
      tasks.value = [
        { ...initTask, status: 'error', error: msg },
        ...tasks.value.filter((x) => x.task_id !== id),
      ]
      throw e
    }
    return id
  }

  async function refresh(taskId: string): Promise<void> {
    try {
      const res = await safeCall('GetDownloadProgress', [taskId])
      if (res) upsert(res)
    } catch {
      // 忽略
    }
  }

  async function cancel(taskId: string): Promise<boolean> {
    try {
      const ok = await safeCall('CancelDownload', [taskId])
      if (ok) {
        const t = tasks.value.find((x) => x.task_id === taskId)
        if (t) t.status = 'cancelled'
      }
      return !!ok
    } catch {
      return false
    }
  }

  async function pause(taskId: string): Promise<boolean> {
    try {
      const ok = await safeCall('PauseDownload', [taskId])
      if (ok) {
        const t = tasks.value.find((x) => x.task_id === taskId)
        if (t) {
          t.status = 'paused'
          t.speed_bps = 0
          t.eta_sec = 0
        }
      }
      return !!ok
    } catch (e: any) {
      console.error('pause download failed:', e)
      return false
    }
  }

  async function resume(taskId: string): Promise<boolean> {
    try {
      const ok = await safeCall('ResumeDownload', [taskId])
      if (ok) {
        const t = tasks.value.find((x) => x.task_id === taskId)
        if (t) t.status = 'downloading'
      }
      return !!ok
    } catch (e: any) {
      console.error('resume download failed:', e)
      return false
    }
  }

  async function remove(taskId: string): Promise<boolean> {
    try {
      await safeCall('RemoveDownload', [taskId])
    } catch {
      // 忽略
    }
    tasks.value = tasks.value.filter((x) => x.task_id !== taskId)
    return true
  }

  async function openFile(path: string): Promise<boolean> {
    try {
      return !!(await safeCall('OpenFileInExplorer', [path]))
    } catch {
      return false
    }
  }

  /**
   * 批量下载：顺序启动多个下载任务
   * 返回成功启动的任务 ID 列表
   */
  async function startDownloadBatch(
    items: Array<{
      url: string
      filename: string
      vod_name?: string
      ep_name?: string
      source_key?: string
      vod_id?: string
      ep_num?: number
    }>,
    onError?: (item: any, err: Error) => void,
  ): Promise<string[]> {
    const started: string[] = []
    for (const item of items) {
      try {
        const id = await startDownload(item)
        started.push(id)
      } catch (e: any) {
        const msg: string = e?.message || String(e)
        // 重复下载：静默跳过（不报错也不阻塞后续）
        if (msg.startsWith('duplicate_url:')) {
          continue
        }
        if (onError) {
          onError(item, e)
        } else {
          console.warn('batch download item failed:', item.filename, msg)
        }
      }
    }
    return started
  }

  /**
   * 检查指定 URL / vod_id + ep_num 是否已经在下载或已完成
   * 返回 'downloading' | 'done' | 'error' | null
   */
  function findStatusForVideo(
    query: { url?: string; vod_id?: string; ep_num?: number },
  ): DownloadTask | null {
    for (const t of tasks.value) {
      if (query.url && t.url === query.url) return t
      if (
        query.vod_id !== undefined &&
        query.ep_num !== undefined &&
        t.vod_id === query.vod_id &&
        t.ep_num === query.ep_num
      ) {
        return t
      }
    }
    return null
  }

  return {
    tasks,
    dir,
    hasActive,
    activeCount,
    init,
    cleanup,
    setDir,
    startDownload,
    startDownloadBatch,
    refresh,
    cancel,
    pause,
    resume,
    remove,
    openFile,
    findStatusForVideo,
  }
})

export function formatBytes(b: number): string {
  if (!isFinite(b) || b <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let i = 0
  let n = b
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024
    i++
  }
  return n.toFixed(i === 0 ? 0 : 2) + ' ' + units[i]
}

export function formatSpeed(bps: number): string {
  return formatBytes(bps) + '/s'
}

export function formatEta(sec: number): string {
  if (!isFinite(sec) || sec <= 0) return '--'
  if (sec < 60) return Math.ceil(sec) + ' 秒'
  if (sec < 3600) return Math.ceil(sec / 60) + ' 分'
  return (sec / 3600).toFixed(1) + ' 时'
}

export function percent(t: DownloadTask): number {
  if (t.total <= 0) return 0
  return Math.min(100, Math.round((t.downloaded / t.total) * 100))
}
