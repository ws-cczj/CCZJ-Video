import { defineStore } from 'pinia'
import { tr } from '../locales'
import { ref, computed } from 'vue'
import {
  CancelDownload,
  GetDownloadDir,
  GetDownloadProgress,
  GetSetting,
  ListDownloads,
  OpenFileInExplorer,
  PauseDownload,
  RemoveDownload,
  ResumeDownload,
  SetDownloadDir,
  StartVideoDownload,
  downloadErrorCode,
} from '../api/download'
import { useErrorStore } from './error'
import { onBackendEvent } from '../api/events'

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
          const ret = await SetDownloadDir(saved)
          if (typeof ret === 'string' && ret) dir.value = ret
        } catch {
          dir.value = saved
        }
      }
    } catch { /* 忽略 */ }

    // 2) 从后端获取默认目录（兜底）
    if (!dir.value) {
      try {
        const d = await GetDownloadDir()
        if (typeof d === 'string' && d) dir.value = d
      } catch {
        // 忽略
      }
    }

    // 4) 历史任务列表
    try {
      const all = await ListDownloads()
      if (Array.isArray(all)) {
        for (const item of all) upsert(item)
      }
    } catch {
      // 忽略
    }

    try {
      _off = onBackendEvent<any>('download:progress', (data) => {
        upsert(data)
        const taskId = data.task_id ?? data.TaskId ?? ''
        const downloaded = Number(data.downloaded ?? data.Downloaded ?? 0)
        const total = Number(data.total ?? data.Total ?? 0)
      })
    } catch {
      // 忽略运行时尚未就绪的事件桥接
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
      // 1) 推送到 Go 后端（设置进程内的 customDownloadDir，后端会自动持久化）
      try {
        const ret = await SetDownloadDir(newDir)
        if (typeof ret === 'string' && ret) dir.value = ret
      } catch {
        // 忽略
      }
    } catch (e: any) {
      throw new Error(e?.message || tr('downloads.setDirFailed'))
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
      const res = await StartVideoDownload({
        task_id: id,
        url: opts.url,
        filename: opts.filename,
        save_dir: dir.value || '',
        force: opts.force || false,
      })
      if (res) upsert(res)
    } catch (e: any) {
      const msg: string = e?.message || String(e)
      const isDuplicate = downloadErrorCode(e) === 'DOWNLOAD_DUPLICATE'
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
      const res = await GetDownloadProgress(taskId)
      if (res) upsert(res)
    } catch {
      // 忽略
    }
  }

  async function cancel(taskId: string): Promise<boolean> {
    try {
      const ok = await CancelDownload(taskId)
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
      const ok = await PauseDownload(taskId)
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
      useErrorStore().fromError(tr('errors.pauseTaskFailed'), e)
      return false
    }
  }

  async function resume(taskId: string): Promise<boolean> {
    try {
      const ok = await ResumeDownload(taskId)
      if (ok) {
        const t = tasks.value.find((x) => x.task_id === taskId)
        if (t) t.status = 'downloading'
      }
      return !!ok
    } catch (e: any) {
      useErrorStore().fromError(tr('errors.resumeTaskFailed'), e)
      return false
    }
  }

  async function remove(taskId: string): Promise<boolean> {
    try {
      await RemoveDownload(taskId)
    } catch {
      // 忽略
    }
    tasks.value = tasks.value.filter((x) => x.task_id !== taskId)
    return true
  }

  async function openFile(path: string): Promise<boolean> {
    try {
      return !!(await OpenFileInExplorer(path))
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
        if (downloadErrorCode(e) === 'DOWNLOAD_DUPLICATE') {
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
  if (sec < 60) return Math.ceil(sec) + ' ' + tr('common.unitSecond')
  if (sec < 3600) return Math.ceil(sec / 60) + ' ' + tr('common.unitMinute')
  return (sec / 3600).toFixed(1) + ' ' + tr('common.unitHour')
}

export function percent(t: DownloadTask): number {
  if (t.total <= 0) return 0
  return Math.min(100, Math.round((t.downloaded / t.total) * 100))
}
