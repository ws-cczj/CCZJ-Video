/**
 * 更新模块共享状态
 * UpdateModal.vue 和 Settings.vue 等外部组件通过此模块访问/控制更新弹窗
 */
import { ref, reactive, computed } from 'vue'
import { readStorage, removeStorage, writeStorage } from '../platform/storage'
import { tr } from '../locales'
import { DownloadUpdate } from '../api/app'
import { normalizeApiError } from '../api/errors'
import { createRequestScope } from '../api/requestScope'

/** 弹窗是否打开 */
export const updateModalOpen = ref(false)

/** 是否正在检查更新 */
export const updateChecking = ref(false)

// ========== 下载状态（全局共享，支持后台下载） ==========
export const updateDownloading = ref(false)
export const updateDownloaded = ref(false)
export const updateDownloadPath = ref('')
export const updateDownloadError = ref(false)
export const updateDownloadProgress = reactive({
  downloaded: 0,
  total: 0,
  speed_bps: 0,
  percent: 0,
})

const downloadRequests = createRequestScope()
let activeDownload: Promise<string> | null = null

/** The document owns one update download; hiding the modal keeps it running. */
export async function downloadUpdateArtifact(url: string, version: string): Promise<void> {
  if (activeDownload) return
  updateDownloading.value = true
  updateDownloaded.value = false
  updateDownloadError.value = false
  updateDownloadPath.value = ''
  clearDownloadState()
  Object.assign(updateDownloadProgress, { downloaded: 0, total: 0, percent: 0, speed_bps: 0 })
  const request = downloadRequests.run(() => DownloadUpdate(url))
  activeDownload = request
  try {
    const path = await request
    if (activeDownload !== request) return
    updateDownloadPath.value = path
    updateDownloaded.value = true
    if (version) saveDownloadState(path, version, url)
  } catch (error) {
    if (activeDownload === request && normalizeApiError(error).code !== 'CANCELLED') updateDownloadError.value = true
    throw error
  } finally {
    if (activeDownload === request) {
      activeDownload = null
      updateDownloading.value = false
    }
  }
}

/** Release the actual binding and invalidate its late value before another document owner starts. */
export function stopUpdateDownload(): void {
  activeDownload = null
  downloadRequests.cancel()
  updateDownloading.value = false
}

export interface UpdateDownloadProgressPayload {
  downloaded?: number
  total?: number
  speed_bps?: number
}

export function applyUpdateDownloadProgress(data: UpdateDownloadProgressPayload): void {
  if (!data) return
  const nonNegative = (value: unknown) => Number.isFinite(Number(value)) ? Math.max(0, Number(value)) : 0
  const downloaded = nonNegative(data.downloaded)
  const total = nonNegative(data.total)
  const speedBps = nonNegative(data.speed_bps)
  updateDownloadProgress.downloaded = downloaded
  updateDownloadProgress.total = total
  updateDownloadProgress.speed_bps = speedBps
  updateDownloadProgress.percent = total > 0 ? Math.min(100, Math.round((downloaded / total) * 100)) : 0
}

/** 是否正在后台下载 */
export const updateBgDownloading = computed(
  () => updateDownloading.value && !updateModalOpen.value
)

// ========== 下载状态持久化（localStorage） ==========
const STORAGE_KEY = 'cczj_update_download_state'

interface DownloadState {
  path: string
  version: string
  url: string
}

export function saveDownloadState(path: string, version: string, url: string): void {
  writeStorage(STORAGE_KEY, { path, version, url })
}

export function loadDownloadState(): DownloadState | null {
  return readStorage<DownloadState | null>(STORAGE_KEY, null)
}

export function clearDownloadState(): void {
  removeStorage(STORAGE_KEY)
}

/** 格式化下载大小（0 显示 "0 B"，NaN/负数 显示 "未知"） */
export function fmtSize(bytes: number): string {
  if (!bytes || bytes < 0) return tr('common.unknown') // NaN, undefined, negative
  if (bytes < 1024) return bytes.toFixed(0) + ' B'
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB'
  return (bytes / (1024 * 1024)).toFixed(1) + ' MB'
}

/** 格式化下载速度 */
export function fmtSpeed(bps: number): string {
  if (!bps || bps <= 0) return '' // NaN, 0, negative
  if (bps < 1024) return bps.toFixed(0) + ' B/s'
  if (bps < 1024 * 1024) return (bps / 1024).toFixed(1) + ' KB/s'
  return (bps / (1024 * 1024)).toFixed(1) + ' MB/s'
}

/**
 * 更新控制器（由 UpdateModal.vue 在挂载时赋值）
 * Settings.vue 等外部可通过 updateController.checkUpdate() 触发检查
 */
export const updateController = {
  checkUpdate: null as (() => Promise<void>) | null,
  openModal: null as (() => void) | null,
}
