<script setup lang="ts">
defineOptions({ name: 'UpdateModal' })
import { ref, onMounted, onUnmounted } from 'vue'
import {
  CheckUpdate, DownloadUpdate, InstallUpdate, FileExists,
  IgnoreVersion, GetPendingUpdateInfo, ClearPendingUpdateInfo,
} from '../api/app'
import { useErrorStore } from '../stores/error'
import {
  updateModalOpen, updateChecking,
  updateDownloading, updateDownloaded, updateDownloadPath, updateDownloadProgress,
  updateController, fmtSize, fmtSpeed,
  saveDownloadState, loadDownloadState, clearDownloadState,
} from '../stores/updateState'
import Icon from './Icon.vue'
import { Button, Modal } from './ui'
import { onBackendEvent } from '../api/events'

const errorStore = useErrorStore()

// 本地状态（不共享）
const installing = ref(false)
const checkFailed = ref(false)
let stopDownloadProgress: (() => void) | null = null
let stopUpdateAvailable: (() => void) | null = null

interface UpdateInfoData {
  has_update: boolean
  current_version: string
  latest_version: string
  release_name: string
  release_notes: string
  download_url: string
  asset_name: string
  asset_size: number
  published_at: string
  history?: { version: string; desc: string }[]
  already_downloaded_path?: string
  already_downloaded_version?: string
}
const updateInfo = ref<UpdateInfoData | null>(null)

// ---------- 检查更新 ----------
async function doCheckUpdate(): Promise<void> {
  updateChecking.value = true
  checkFailed.value = false
  try {
    const info = await CheckUpdate()
    if (!info) return
    updateInfo.value = info
    if (info.has_update) {
      // 检查是否有已下载的安装包
      await checkAlreadyDownloaded(info)
      updateModalOpen.value = true
    } else {
      errorStore.info('检查更新', `当前已是最新版本 ${info.current_version}`)
    }
  } catch (e: any) {
    checkFailed.value = true
    updateModalOpen.value = true
  } finally {
    updateChecking.value = false
  }
}

// ---------- 下载更新 ----------
async function doDownload(): Promise<void> {
  if (!updateInfo.value?.download_url || updateDownloading.value) return
  updateDownloading.value = true
  updateDownloaded.value = false
  updateDownloadPath.value = ''
  updateDownloadProgress.downloaded = 0
  updateDownloadProgress.total = 0
  updateDownloadProgress.percent = 0
  updateDownloadProgress.speed_bps = 0
  try {
    const path = await DownloadUpdate(updateInfo.value.download_url)
    updateDownloadPath.value = path
    updateDownloaded.value = true
    // 持久化下载状态，下次启动时可检查
    if (updateInfo.value?.latest_version) {
      saveDownloadState(path, updateInfo.value.latest_version, updateInfo.value.download_url)
    }
  } catch (e: any) {
    errorStore.fromError('下载更新失败', e, 'UpdateModal.downloadUpdate')
  } finally {
    updateDownloading.value = false
  }
}

// ---------- 安装更新 ----------
async function doInstall(): Promise<void> {
  if (!updateDownloadPath.value) return
  installing.value = true
  try {
    clearDownloadState()
    // 注意：不要在这里调用 ClearPendingUpdateInfo()！
    // Go 的 InstallUpdate 会先读取 pendingUpdateInfo 记录版本号，再清理
    await InstallUpdate(updateDownloadPath.value)
  } catch (e: any) {
    errorStore.fromError('安装更新失败', e, 'UpdateModal.installUpdate')
    installing.value = false
  }
}

// ---------- 忽略版本 ----------
async function doIgnore(): Promise<void> {
  if (!updateInfo.value?.latest_version) return
  try {
    await IgnoreVersion(updateInfo.value.latest_version)
    await ClearPendingUpdateInfo()
    // 清理已下载的安装包状态
    const state = loadDownloadState()
    if (state && state.version === updateInfo.value.latest_version) {
      clearDownloadState()
      updateDownloaded.value = false
      updateDownloadPath.value = ''
    }
    updateModalOpen.value = false
  } catch { /* ignore */ }
}

// ---------- 关闭弹窗（支持后台下载） ----------
function onClose(): void {
  if (updateDownloading.value) {
    // 正在下载：最小化到后台，不重置状态
    updateModalOpen.value = false
    return
  }
  updateModalOpen.value = false
  checkFailed.value = false
  // 只有下载完成且未安装时才清除
  if (updateDownloaded.value) {
    // 保留状态，允许下次打开继续安装
  } else {
    ClearPendingUpdateInfo()
  }
}

// ---------- 事件监听 ----------
function onDownloadProgress(ev: any): void {
  const data = ev.data
  if (!data) return
  const downloaded = Number(data.downloaded ?? 0)
  const total = Number(data.total ?? 0)
  const speedBps = Number(data.speed_bps ?? 0)
  updateDownloadProgress.downloaded = downloaded
  updateDownloadProgress.total = total
  updateDownloadProgress.speed_bps = speedBps
  if (total > 0) {
    updateDownloadProgress.percent = Math.round((downloaded / total) * 100)
  }
}

function onUpdateAvailable(ev: any): void {
  const data = ev.data
  if (data && data.has_update) {
    updateInfo.value = data
    // 检查是否有已下载的安装包
    checkAlreadyDownloaded(data).then(() => {
      updateModalOpen.value = true
    })
  }
}

// 检查是否有已下载的安装包
// 优先使用 Go 后端扫描结果（already_downloaded_path），回退到 localStorage
async function checkAlreadyDownloaded(info: UpdateInfoData): Promise<void> {
  // 方式1：Go 后端直接扫描应用目录发现的 *_update.exe
  const backendPath = info.already_downloaded_path
  if (backendPath) {
    try {
      const exists = await FileExists(backendPath)
      if (exists) {
        updateDownloadPath.value = backendPath
        updateDownloaded.value = true
        return
      }
    } catch { /* fall through */ }
  }

  // 方式2：localStorage 缓存（兼容旧版下载状态）
  const state = loadDownloadState()
  if (!state || !state.path || !state.version) return
  if (info.latest_version && state.version !== info.latest_version) {
    clearDownloadState()
    return
  }
  try {
    const exists = await FileExists(state.path)
    if (exists) {
      updateDownloadPath.value = state.path
      updateDownloaded.value = true
    } else {
      clearDownloadState()
    }
  } catch {
    clearDownloadState()
  }
}

onMounted(async () => {
  stopDownloadProgress = onBackendEvent('update:download:progress', onDownloadProgress)
  stopUpdateAvailable = onBackendEvent('update:available', onUpdateAvailable)

  // 注册全局函数
  updateController.checkUpdate = doCheckUpdate
  updateController.openModal = () => { updateModalOpen.value = true }

  // 重试轮询待处理更新（Go 网络检查可能耗时较长）
  let pollRetries = 0
  const maxPollRetries = 15
  const pollInterval = 5000

  const pollPending = async () => {
    if (updateModalOpen.value || pollRetries >= maxPollRetries) return
    pollRetries++
    try {
      const pending = await GetPendingUpdateInfo()
      if (pending && pending.has_update) {
        updateInfo.value = pending
        // 检查是否有已下载的安装包
        await checkAlreadyDownloaded(pending)
        updateModalOpen.value = true
        return
      }
    } catch { /* ignore */ }
    if (pollRetries < maxPollRetries) {
      setTimeout(pollPending, pollInterval)
    }
  }

  // 延迟 5 秒后开始首次轮询
  setTimeout(pollPending, pollInterval)
})

onUnmounted(() => {
  stopDownloadProgress?.()
  stopUpdateAvailable?.()
  stopDownloadProgress = null
  stopUpdateAvailable = null
})
</script>

<template>
  <Modal
    :model-value="updateModalOpen"
    :title="checkFailed ? '获取更新信息失败' : (updateDownloaded ? '下载完成' : (updateDownloading ? '正在下载更新...' : '发现新版本'))"
    width="min(560px, 94vw)"
    :show-footer="true"
    @update:model-value="(v: boolean) => { if (!v) onClose() }"
  >
    <div class="modal-body">
      <!-- 检查更新失败界面 -->
      <template v-if="checkFailed">
        <div class="update-done-card cczj-flex cczj-flex-col cczj-items-center cczj-gap-8">
          <div class="update-fail-icon cczj-inline-flex cczj-items-center cczj-justify-center">
            <Icon name="alert-circle" :size="28" />
          </div>
          <div class="update-done-text">
            <p><strong>获取最新版本信息失败</strong></p>
            <p>可能是无法访问 GitHub 导致的，请尝试手动检查更新。</p>
          </div>
          <div class="update-fail-hint">
            <p>检查方法：打开 <a href="https://github.com/ws-cczj/CCZJ-Video/releases" target="_blank" class="update-link">软件发布页</a>，查看「Latest」发布的版本号与当前版本对比是否一致。</p>
            <p>若一致则不必理会，直接关闭即可；否则请手动下载新版本更新。</p>
          </div>
        </div>
      </template>

      <!-- 下载完成界面 -->
      <template v-else-if="updateDownloaded">
        <div class="update-done-card cczj-flex cczj-flex-col cczj-items-center cczj-gap-8">
          <div class="update-done-icon cczj-inline-flex cczj-items-center cczj-justify-center">
            <Icon name="check" :size="28" />
          </div>
          <div class="update-done-text">
            <p><strong>更新包已下载完成</strong></p>
            <p class="update-done-path">{{ updateDownloadPath }}</p>
          </div>
          <div class="update-done-hint">
            <p>点击「立即更新」将关闭当前程序并启动新版本安装程序。</p>
            <p>也可以点击「下次启动」，下次启动应用时再安装更新。</p>
          </div>
        </div>
      </template>

      <!-- 下载中界面 -->
      <template v-else-if="updateDownloading">
        <div class="update-downloading-card cczj-flex cczj-flex-col cczj-items-center cczj-gap-6">
          <div class="update-loading-icon cczj-inline-flex cczj-items-center cczj-justify-center">
            <Icon name="download" :size="24" />
          </div>
          <div class="update-downloading-title">
            <p><strong>正在下载更新...</strong></p>
            <p class="update-downloading-sub">下载完成后将提示安装，期间可继续使用软件。</p>
          </div>
          <!-- 进度条（始终显示） -->
          <div class="update-progress-full">
            <div class="progress-track" :class="{ indeterminate: updateDownloadProgress.total <= 0 }">
              <div class="progress-fill" :style="updateDownloadProgress.total > 0 ? { width: updateDownloadProgress.percent + '%' } : {}"></div>
            </div>
            <div class="progress-meta cczj-flex cczj-justify-between">
              <span v-if="updateDownloadProgress.downloaded === 0" class="update-connecting">
                正在连接下载源...
              </span>
              <span v-else-if="updateDownloadProgress.total > 0">
                {{ fmtSize(updateDownloadProgress.downloaded) }} / {{ fmtSize(updateDownloadProgress.total) }} ({{ updateDownloadProgress.percent }}%)
              </span>
              <span v-else>
                已下载 {{ fmtSize(updateDownloadProgress.downloaded) }}
              </span>
              <span v-if="updateDownloadProgress.speed_bps > 0">{{ fmtSpeed(updateDownloadProgress.speed_bps) }}</span>
              <span v-else-if="updateDownloadProgress.downloaded > 0" class="update-speed-placeholder">—</span>
            </div>
          </div>
        </div>
      </template>

      <!-- 更新信息界面 -->
      <template v-else-if="updateInfo">
        <div class="update-header cczj-flex cczj-items-center cczj-gap-8">
          <div class="update-icon cczj-inline-flex cczj-items-center cczj-justify-center">
            <Icon name="download" :size="22" />
          </div>
          <div>
            <h3 class="update-title">{{ updateInfo.release_name || `v${updateInfo.latest_version}` }}</h3>
            <p class="update-versions">
              <span class="update-current">{{ updateInfo.current_version }}</span>
              <span class="update-arrow">&rarr;</span>
              <span class="update-latest">{{ updateInfo.latest_version }}</span>
            </p>
          </div>
        </div>

        <!-- 更新内容 -->
        <div v-if="updateInfo.release_notes" class="update-notes">
          <h4>更新内容</h4>
          <div class="update-notes-body">{{ updateInfo.release_notes }}</div>
        </div>

        <!-- 历史版本 -->
        <div v-if="updateInfo.history && updateInfo.history.length > 0" class="update-history">
          <h4>历史版本</h4>
          <div v-for="(ver, index) in updateInfo.history" :key="index" class="update-history-item">
            <h5>v{{ ver.version }}</h5>
            <pre>{{ ver.desc }}</pre>
          </div>
        </div>

        <!-- 文件信息 -->
        <div v-if="updateInfo.asset_name" class="update-file-info">
          <span class="update-file-name">{{ updateInfo.asset_name }}</span>
          <span class="update-file-size">{{ fmtSize(updateInfo.asset_size) }}</span>
        </div>
      </template>
    </div>

    <template #footer>
      <!-- 检查失败界面按钮 -->
      <template v-if="checkFailed">
        <span style="flex: 1"></span>
        <Button variant="primary" size="md" :loading="updateChecking" @click="doCheckUpdate">
          <Icon name="refresh" :size="14" /> 重新检查更新
        </Button>
      </template>

      <!-- 下载完成界面按钮 -->
      <template v-else-if="updateDownloaded">
        <Button variant="secondary" size="md" @click="doIgnore">
          下次启动
        </Button>
        <span style="flex: 1"></span>
        <Button variant="primary" size="md" :loading="installing" @click="doInstall">
          <Icon name="zap" :size="14" /> 立即更新
        </Button>
      </template>

      <!-- 下载中界面按钮 -->
      <template v-else-if="updateDownloading">
        <span class="bg-hint">下载将在后台继续</span>
        <span style="flex: 1"></span>
        <Button variant="secondary" size="md" @click="updateModalOpen = false">
          后台下载
        </Button>
      </template>

      <!-- 更新信息界面按钮 -->
      <template v-else>
        <Button variant="secondary" size="md" @click="doIgnore">
          忽略此版本
        </Button>
        <span style="flex: 1"></span>
        <Button variant="primary" size="md" @click="doDownload">
          <Icon name="download" :size="14" /> 下载更新
        </Button>
      </template>
    </template>
  </Modal>

  <!-- 后台下载悬浮指示器（当弹窗关闭但正在下载时显示） -->
  <Teleport to="body">
    <Transition name="bg-download-fade">
      <div
        v-if="updateDownloading && !updateModalOpen"
        class="bg-download-indicator"
        @click="updateModalOpen = true"
      >
        <div class="bg-download-icon">
          <Icon name="download" :size="14" />
        </div>
        <div class="bg-download-info">
          <span class="bg-download-title">{{ updateDownloadProgress.downloaded === 0 ? '正在连接下载源...' : '正在下载更新' }}</span>
          <span v-if="updateDownloadProgress.total > 0 && updateDownloadProgress.downloaded > 0" class="bg-download-pct">{{ updateDownloadProgress.percent }}%</span>
          <span v-else class="bg-download-pct">...</span>
        </div>
        <div class="bg-download-track">
          <div
            class="bg-download-fill"
            :class="{ indeterminate: updateDownloadProgress.total <= 0 }"
            :style="updateDownloadProgress.total > 0 ? { width: updateDownloadProgress.percent + '%' } : {}"
          ></div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<style scoped>
/* 版本更新弹窗 */
.update-header { gap: 16px; padding: 8px 0 16px; border-bottom: 1px dashed var(--border); margin-bottom: 16px; }
.update-icon { width: 52px; height: 52px; border-radius: 14px; background: var(--accent); color: var(--accent-contrast); box-shadow: 0 6px 18px var(--accent-alpha-35); flex-shrink: 0; }
.update-title { font-size: 1.14rem; font-weight: 700; margin: 0 0 4px; color: var(--text-primary); }
.update-versions { margin: 0; font-size: 0.93rem; display: flex; align-items: center; gap: 10px; }
.update-current { color: var(--text-muted); font-family: ui-monospace, Menlo, Monaco, Consolas, monospace; }
.update-arrow { color: var(--text-muted); font-size: 1.14rem; }
.update-latest { color: var(--accent); font-weight: 700; font-family: ui-monospace, Menlo, Monaco, Consolas, monospace; }
.update-notes { margin-bottom: 16px; }
.update-notes h4 { font-size: 0.86rem; font-weight: 600; color: var(--text-secondary); margin: 0 0 8px; text-transform: uppercase; letter-spacing: 0.5px; }
.update-notes-body { background: var(--bg-secondary); border: 1px solid var(--border); border-radius: 10px; padding: 12px 14px; font-size: 0.93rem; line-height: 1.6; color: var(--text-secondary); max-height: 200px; overflow-y: auto; white-space: pre-wrap; word-break: break-word; }
.update-progress-full { width: 100%; max-width: 400px; }
.update-progress-full .progress-track { height: 8px; background: var(--bg-secondary); border: 1px solid var(--border); border-radius: 4px; overflow: hidden; margin-bottom: 8px; position: relative; }
.update-progress-full .progress-track.indeterminate .progress-fill {
  width: 30%;
  animation: indeterminate-slide 1.5s ease-in-out infinite;
}
.update-progress-full .progress-fill { height: 100%; background: var(--accent); border-radius: 4px; transition: width 0.3s ease; }
.update-progress-full .progress-meta { font-size: 0.86rem; color: var(--text-muted); }
.update-connecting { color: var(--text-muted); animation: connecting-pulse 1.5s ease-in-out infinite; }
@keyframes connecting-pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.4; }
}
.update-speed-placeholder { visibility: hidden; }
@keyframes indeterminate-slide {
  0% { transform: translateX(-100%); }
  100% { transform: translateX(400%); }
}
.update-file-info { display: flex; justify-content: space-between; align-items: center; padding: 10px 14px; background: var(--bg-secondary); border: 1px solid var(--border); border-radius: 8px; font-size: 0.86rem; }
.update-file-name { color: var(--text-primary); font-family: ui-monospace, Menlo, Monaco, Consolas, monospace; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; flex: 1; min-width: 0; margin-right: 12px; }
.update-file-size { color: var(--text-muted); flex-shrink: 0; }
.update-done-card { gap: 16px; padding: 16px 0; text-align: center; }
.update-done-icon { width: 64px; height: 64px; border-radius: 50%; background: var(--success); color: #fff; box-shadow: 0 6px 18px var(--success-alpha-10); }
.update-done-text p { margin: 0 0 4px; font-size: 1.07rem; color: var(--text-primary); }
.update-done-path { font-size: 0.79rem !important; color: var(--text-muted) !important; font-family: ui-monospace, Menlo, Monaco, Consolas, monospace; word-break: break-all; margin-top: 6px !important; }
.update-done-hint { font-size: 0.86rem; color: var(--text-muted); line-height: 1.6; }
.update-done-hint p { margin: 0 0 4px; }
.update-fail-icon { width: 64px; height: 64px; border-radius: 50%; background: var(--warning, #f59e0b); color: #fff; box-shadow: 0 6px 18px rgba(245, 158, 11, 0.15); }
.update-fail-hint { font-size: 0.86rem; color: var(--text-muted); line-height: 1.6; text-align: left; padding: 0 8px; }
.update-fail-hint p { margin: 0 0 4px; }
.update-link { color: var(--accent); text-decoration: underline; cursor: pointer; }
.update-link:hover { opacity: 0.8; }
.update-history { margin-top: 16px; padding-top: 12px; border-top: 1px solid var(--border); }
.update-history h4 { font-size: 0.91rem; font-weight: 600; color: var(--text-primary); margin: 0 0 10px; }
.update-history-item { margin-bottom: 12px; padding: 10px 12px; background: var(--bg-secondary); border-radius: 8px; }
.update-history-item h5 { font-size: 0.89rem; font-weight: 600; color: var(--text-primary); margin: 0 0 6px; }
.update-history-item pre { font-size: 0.82rem; color: var(--text-secondary); white-space: pre-wrap; margin: 0; line-height: 1.5; font-family: inherit; }

/* 下载中界面 */
.update-downloading-card { padding: 24px 0; text-align: center; }
.update-loading-icon {
  width: 56px; height: 56px; border-radius: 50%;
  background: var(--accent-alpha-15); color: var(--accent);
  animation: pulse-download 2s ease-in-out infinite;
}
@keyframes pulse-download {
  0%, 100% { transform: scale(1); box-shadow: 0 0 0 0 var(--accent-alpha-20); }
  50% { transform: scale(1.05); box-shadow: 0 0 0 12px transparent; }
}
.update-downloading-title p { margin: 0 0 4px; font-size: 1.07rem; color: var(--text-primary); }
.update-downloading-sub { font-size: 0.86rem !important; color: var(--text-muted) !important; }

/* 后台下载提示文字 */
.bg-hint { font-size: 0.82rem; color: var(--text-muted); }

/* ====== 后台下载悬浮指示器 ====== */
.bg-download-indicator {
  position: fixed;
  right: 24px;
  bottom: 88px;
  z-index: var(--z-fab);
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 14px;
  background: var(--bg-card);
  border: 1px solid var(--accent);
  border-radius: 24px;
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.15), 0 0 0 3px var(--accent-alpha-10);
  cursor: pointer;
  transition: transform 0.2s ease, box-shadow 0.2s ease;
  min-width: 160px;
}
.bg-download-indicator:hover {
  transform: translateY(-2px);
  box-shadow: 0 6px 20px rgba(0, 0, 0, 0.2), 0 0 0 4px var(--accent-alpha-15);
}
.bg-download-icon {
  width: 28px; height: 28px; border-radius: 50%;
  background: var(--accent); color: var(--accent-contrast);
  display: flex; align-items: center; justify-content: center;
  flex-shrink: 0;
  animation: cczj-spin 2s linear infinite;
}
.bg-download-info {
  display: flex; flex-direction: column; flex: 1; min-width: 0;
}
.bg-download-title { font-size: 0.79rem; font-weight: 600; color: var(--text-primary); }
.bg-download-pct { font-size: 0.72rem; color: var(--text-muted); font-family: ui-monospace, monospace; }
.bg-download-track {
  position: absolute; bottom: 0; left: 12px; right: 12px;
  height: 3px; background: var(--bg-secondary); border-radius: 2px; overflow: hidden;
}
.bg-download-fill {
  height: 100%; background: var(--accent); border-radius: 2px; transition: width 0.4s ease;
}
.bg-download-fill.indeterminate {
  width: 30%;
  animation: indeterminate-slide 1.5s ease-in-out infinite;
}

/* 后台指示器过渡动画 */
.bg-download-fade-enter-active { transition: all 0.3s ease; }
.bg-download-fade-leave-active { transition: all 0.2s ease; }
.bg-download-fade-enter-from, .bg-download-fade-leave-to { opacity: 0; transform: translateY(10px) scale(0.95); }
</style>
