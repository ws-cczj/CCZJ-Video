import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { WriteLog, GetLogList, GetLogContent, ClearLogs, GetLogDir } from '../../bindings/cczjVideo/app'

export type ErrorLevel = 'info' | 'warn' | 'error'

export interface ErrorItem {
  id: string
  level: ErrorLevel
  title: string
  message: string
  detail?: string
  source?: string
  time: number // ms timestamp
  // 在 toast 状态里，用户可关闭
  closed?: boolean
}

const MAX_TOAST = 10 // 最多同时显示 10 个弹窗
const AUTO_DISMISS_MS_DEFAULT = 5000
const AUTO_DISMISS_MS_INFO = 5000
const AUTO_DISMISS_MS_WARN = 5000
const AUTO_DISMISS_MS_ERROR = 5000

let seq = 0
function nextId(): string {
  seq++
  return `err_${Date.now().toString(36)}_${seq}`
}

export const useErrorStore = defineStore('error', () => {
  const toasts = ref<ErrorItem[]>([])
  const history = ref<ErrorItem[]>([])

  // 直接用 toasts 数组的存在/不存在来驱动 transition-group
  const visibleToasts = computed(() => toasts.value)

  // 暴露给前端的主要方法
  function push(item: Omit<ErrorItem, 'id' | 'time'> & { autoDismiss?: boolean | number }): ErrorItem {
    const e: ErrorItem = {
      id: nextId(),
      level: item.level || 'error',
      title: item.title || '错误',
      message: item.message || '',
      detail: item.detail,
      source: item.source,
      time: Date.now(),
    }
    toasts.value.unshift(e)
    history.value.unshift(e)
    // 限制列表长度
    if (toasts.value.length > MAX_TOAST) {
      // 移除最旧的（数组末尾），保证新的在前
      toasts.value.splice(MAX_TOAST, toasts.value.length - MAX_TOAST)
    }
    if (history.value.length > 500) history.value.length = 500

    // 写入后端日志文件（异步）
    try {
      WriteLog({
        level: e.level.toUpperCase(),
        message: `${e.title} — ${e.message}`,
        source: e.source || '',
        detail: e.detail || '',
      }).catch(() => { /* ignore */ })
    } catch { /* ignore */ }

    // 自动关闭：默认按级别分别 5s，调用方可以用 autoDismiss 覆盖
    let ms: number
    if (typeof item.autoDismiss === 'number') {
      ms = item.autoDismiss
    } else if (item.autoDismiss === false) {
      ms = 0
    } else if (e.level === 'info') {
      ms = AUTO_DISMISS_MS_INFO
    } else if (e.level === 'warn') {
      ms = AUTO_DISMISS_MS_WARN
    } else {
      ms = AUTO_DISMISS_MS_ERROR
    }
    if (ms > 0) {
      setTimeout(() => dismiss(e.id), ms)
    }
    return e
  }

  function info(title: string, message = '', detail = '', source = ''): ErrorItem {
    return push({ level: 'info', title, message, detail, source })
  }
  function warn(title: string, message = '', detail = '', source = ''): ErrorItem {
    return push({ level: 'warn', title, message, detail, source })
  }
  function error(title: string, message = '', detail = '', source = ''): ErrorItem {
    // error 也走 5s 自动消失（由 push 内 level 判断）
    return push({ level: 'error', title, message, detail, source })
  }
  function fromError(title: string, err: unknown, source = ''): ErrorItem {
    const msg = err instanceof Error ? err.message : String(err)
    const detail = err instanceof Error && err.stack ? err.stack : ''
    return error(title, msg, detail, source)
  }

  // 从数组中移除 —— 让 transition-group 正确触发 leave 动画
  function dismiss(id: string): void {
    const idx = toasts.value.findIndex(x => x.id === id)
    if (idx >= 0) toasts.value.splice(idx, 1)
  }
  function clearToasts(): void {
    toasts.value = []
  }

  // ======== 日志文件相关 ========
  async function listLogFiles(): Promise<string[]> {
    try {
      return (await GetLogList()) || []
    } catch {
      return []
    }
  }
  async function readLogFile(filename: string): Promise<string> {
    try {
      return (await GetLogContent(filename)) || ''
    } catch {
      return ''
    }
  }
  async function getLogDir(): Promise<string> {
    try { return await GetLogDir() } catch { return '' }
  }
  async function clearAllLogs(): Promise<number> {
    try { return await ClearLogs() } catch { return 0 }
  }

  return {
    toasts,
    history,
    visibleToasts,
    push,
    info,
    warn,
    error,
    fromError,
    dismiss,
    clearToasts,
    listLogFiles,
    readLogFile,
    getLogDir,
    clearAllLogs,
  }
})
