/**
 * 更新模块共享状态
 * UpdateModal.vue 和 Settings.vue 等外部组件通过此模块访问/控制更新弹窗
 */
import { ref, reactive, computed } from 'vue'
import { readStorage, removeStorage, writeStorage } from '../platform/storage'

/** 弹窗是否打开 */
export const updateModalOpen = ref(false)

/** 是否正在检查更新 */
export const updateChecking = ref(false)

// ========== 下载状态（全局共享，支持后台下载） ==========
export const updateDownloading = ref(false)
export const updateDownloaded = ref(false)
export const updateDownloadPath = ref('')
export const updateDownloadProgress = reactive({
  downloaded: 0,
  total: 0,
  speed_bps: 0,
  percent: 0,
})

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
  if (!bytes || bytes < 0) return '未知' // NaN, undefined, negative
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
