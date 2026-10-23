import { defineStore } from 'pinia'
import { ref, computed, onScopeDispose } from 'vue'
import { WriteLog, normalizeApiError } from '../api/app'
import { tr } from '../locales'

export type ErrorLevel = 'info' | 'warn' | 'error'

export interface ErrorItem {
  id: string
  level: ErrorLevel
  title: string
  message: string
  detail?: string
  source?: string
  code?: string
  requestId?: string
  count?: number
  time: number // ms timestamp
  // 在 toast 状态里，用户可关闭
  closed?: boolean
}

const MAX_TOAST = 10 // 最多同时显示 10 个弹窗
// 停留时长按严重程度分档：越严重读起来越慢，不能一律 5 秒把错误和提示一起收走。
const AUTO_DISMISS_MS_INFO = 3000
const AUTO_DISMISS_MS_WARN = 6000
const AUTO_DISMISS_MS_ERROR = 12000

let seq = 0
function nextId(): string {
  seq++
  return `err_${Date.now().toString(36)}_${seq}`
}

export const useErrorStore = defineStore('error', () => {
  const toasts = ref<ErrorItem[]>([])
  const history = ref<ErrorItem[]>([])
  const timers = new Map<string, ReturnType<typeof setTimeout>>()
  onScopeDispose(clearToasts)

  // 直接用 toasts 数组的存在/不存在来驱动 transition-group
  const visibleToasts = computed(() => toasts.value)

  // 暴露给前端的主要方法
  function push(item: Omit<ErrorItem, 'id' | 'time'> & { autoDismiss?: boolean | number; logged?: boolean }): ErrorItem {
    const duplicate = toasts.value.find(e => e.level === item.level && e.source === item.source && e.code === item.code && e.title === item.title && e.message === item.message && Date.now() - e.time < 10000)
    if (duplicate) { duplicate.count = (duplicate.count || 1) + 1; return duplicate }
    const e: ErrorItem = {
      id: nextId(),
      level: item.level || 'error',
      title: item.title || tr('errors.fallbackTitle'),
      message: item.message || '',
      detail: item.detail,
      source: item.source,
      code: item.code,
      requestId: item.requestId,
      count: 1,
      time: Date.now(),
    }
    toasts.value.unshift(e)
    history.value.unshift(e)
    // 限制列表长度
    if (toasts.value.length > MAX_TOAST) {
      // 移除最旧的（数组末尾），保证新的在前
      for (const old of toasts.value.splice(MAX_TOAST)) { clearTimeout(timers.get(old.id)); timers.delete(old.id) }
    }
    if (history.value.length > 500) history.value.length = 500

    // 写入后端日志文件（异步）
    try {
      if (!item.logged) WriteLog({
        level: e.level.toUpperCase(),
        message: `${e.title} — ${e.message}`,
        source: e.source || '',
        detail: e.detail || '',
      }).catch(() => { /* ignore */ })
    } catch { /* ignore */ }

    // 自动关闭：默认按级别分档，调用方可以用 autoDismiss 覆盖
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
      timers.set(e.id, setTimeout(() => dismiss(e.id), ms))
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
    return push({ level: 'error', title, message, detail, source })
  }
  function fromError(title: string, err: unknown, source = ''): ErrorItem {
    // Prefer the public structured message; the adapter also supports old hosts.
    const normalized = normalizeApiError(err)
    if (normalized.code === 'CANCELLED') return { id: nextId(), level: 'info', title, message: normalized.message, source, code: normalized.code, time: Date.now(), closed: true }
    const detail = err instanceof Error && err.stack ? err.stack : ''
    const level: ErrorLevel = ['INTERNAL', 'STORAGE', 'CORRUPT'].includes(normalized.code) ? 'error' : 'warn'
    return push({ level, title, message: normalized.message, detail, source, code: normalized.code, requestId: normalized.requestId, logged: normalized.logged })
  }

  // 从数组中移除 —— 让 transition-group 正确触发 leave 动画
  function dismiss(id: string): void {
    clearTimeout(timers.get(id)); timers.delete(id)
    const idx = toasts.value.findIndex(x => x.id === id)
    if (idx >= 0) toasts.value.splice(idx, 1)
  }
  function clearToasts(): void {
    for (const timer of timers.values()) clearTimeout(timer)
    timers.clear()
    toasts.value = []
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
  }
})
