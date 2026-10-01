import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
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

  // 直接用 toasts 数组的存在/不存在来驱动 transition-group
  const visibleToasts = computed(() => toasts.value)

  // 暴露给前端的主要方法
  function push(item: Omit<ErrorItem, 'id' | 'time'> & { autoDismiss?: boolean | number }): ErrorItem {
    const e: ErrorItem = {
      id: nextId(),
      level: item.level || 'error',
      title: item.title || tr('errors.fallbackTitle'),
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
    return push({ level: 'error', title, message, detail, source })
  }
  function fromError(title: string, err: unknown, source = ''): ErrorItem {
    // 后端带码错误一律渲染成「CODE: 消息: 原因」（见 app/apperror），而这条是几乎所有
    // 界面提示的唯一出口：码在这里剥掉，界面就只显示人话那半句，不至于每加一个错误码
    // 就多一类把 STORAGE / UNAVAILABLE 甩给用户看的噪声。认不出码的整句照原样显示。
    const { message } = normalizeApiError(err)
    const detail = err instanceof Error && err.stack ? err.stack : ''
    return error(title, message, detail, source)
  }

  // 从数组中移除 —— 让 transition-group 正确触发 leave 动画
  function dismiss(id: string): void {
    const idx = toasts.value.findIndex(x => x.id === id)
    if (idx >= 0) toasts.value.splice(idx, 1)
  }
  function clearToasts(): void {
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
