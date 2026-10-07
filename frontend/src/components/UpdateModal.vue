<script setup lang="ts">
defineOptions({ name: 'UpdateModal' })
import { ref, watch, onMounted, onUnmounted } from 'vue'
import { tr } from '../locales'
import {
  CheckUpdate, DownloadUpdate, InstallUpdate, FileExists,
  IgnoreVersion, GetPendingUpdateInfo, ClearPendingUpdateInfo, GetLastInstallReport,
  ScheduleInstallOnExit, CancelInstallOnExit, GetPendingInstall,
} from '../api/app'
import { useErrorStore } from '../stores/error'
import {
  updateModalOpen, updateChecking,
  updateDownloading, updateDownloaded, updateDownloadPath, updateDownloadProgress,
  updateController, fmtSize, fmtSpeed,
  saveDownloadState, loadDownloadState, clearDownloadState,
} from '../stores/updateState'
import { licensePending } from '../stores/licenseState'
import Icon from './Icon.vue'
import { Button, Modal, MotionTransition } from './ui'
import { onBackendEvent } from '../api/events'

const errorStore = useErrorStore()

// 本地状态（不共享）
const installing = ref(false)
const checkFailed = ref(false)
// 「退出时安装」这份安排的现场状态：按钮写「退出时安装」还是「取消安排」取决于它。
const onExitPlanned = ref(false)
const scheduling = ref(false)
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
      errorStore.info(tr('update.checking'), tr('update.latestVersion', { version: info.current_version }))
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
    errorStore.fromError(tr('update.downloadFailed'), e, 'UpdateModal.downloadUpdate')
  } finally {
    updateDownloading.value = false
  }
}

// ---------- 安装更新（档位一：立即安装） ----------
async function doInstall(): Promise<void> {
  if (!updateDownloadPath.value) return
  installing.value = true
  try {
    clearDownloadState()
    // Go 那边一进 Install 就取代退出时安装的安排，所以不论这次成不成，界面都不该再
    // 显示「取消安排」。
    onExitPlanned.value = false
    // 注意：不要在这里调用 ClearPendingUpdateInfo()！
    // Go 的 InstallUpdate 会先读取 pendingUpdateInfo 记录版本号，再清理
    await InstallUpdate(updateDownloadPath.value)
  } catch (e: any) {
    errorStore.fromError(tr('update.installFailed'), e, 'UpdateModal.installUpdate')
    installing.value = false
  }
}

// ---------- 安装更新（档位二：退出时安装） ----------
// 这一档只在退出那一刻换程序：安排落在 Go 侧的持久记录里，界面这边只反映「有没有安排」。
// 安排是针对某一份包立的。界面上现在摊着的是另一份包时，按钮不能沿用上一条安排的状态，
// 否则用户点下去以为取消的是眼前这个。路径大小写不敏感（Windows）。
function plannedForThisArtifact(plan: any): boolean {
  const path = String(plan?.path ?? '')
  return !!plan?.available && !!path && !!updateDownloadPath.value
    && path.toLowerCase() === updateDownloadPath.value.toLowerCase()
}

async function refreshOnExitPlan(): Promise<void> {
  if (!updateDownloaded.value || !updateDownloadPath.value) {
    onExitPlanned.value = false
    return
  }
  try {
    onExitPlanned.value = plannedForThisArtifact(await GetPendingInstall())
  } catch {
    // 问不到就当没安排：按钮写「退出时安装」，点下去仍然会正确地安排或报错。
    onExitPlanned.value = false
  }
}

async function doScheduleOnExit(): Promise<void> {
  if (!updateDownloadPath.value || scheduling.value) return
  scheduling.value = true
  const wasPlanned = onExitPlanned.value
  try {
    if (wasPlanned) {
      await CancelInstallOnExit()
      onExitPlanned.value = false
      errorStore.info(tr('update.onExitCancelledTitle'), tr('update.onExitCancelledMsg'), '', 'UpdateModal')
    } else {
      await ScheduleInstallOnExit(updateDownloadPath.value, updateInfo.value?.latest_version || '')
      onExitPlanned.value = true
      const version = updateInfo.value?.latest_version
      errorStore.info(
        tr('update.onExitTitle'),
        tr('update.onExitMsg'),
        version ? tr('update.onExitTarget', { version }) : '',
        'UpdateModal',
      )
    }
  } catch (e: any) {
    errorStore.fromError(
      wasPlanned ? tr('update.onExitCancelFailed') : tr('update.onExitFailed'),
      e,
      'UpdateModal.scheduleOnExit',
    )
  } finally {
    scheduling.value = false
  }
}

// ---------- 上次安装的现场回执 ----------
// 结论文件在 Go 启动时就被读掉了，这份报告只活在这次会话里：错过就再没人知道
// 「点了安装、程序自己关了、重开还是老版本」这件事发生过。
let installReportRead = false

async function reportLastInstall(): Promise<void> {
  if (installReportRead) return
  installReportRead = true
  let report: any = null
  try {
    report = await GetLastInstallReport()
  } catch {
    return // 问不到回执就闭嘴：凭空说「上次更新失败」比沉默更糟
  }
  if (!report?.failed) return
  // 认不出的回执原样留在日志里，界面只说这件已经确定的事：没换成。
  const details: Record<string, string> = {
    swap_failed: tr('update.installReportSwap'),
    verify_failed: tr('update.installReportVerify'),
    rollback_failed: tr('update.installReportRollback'),
  }
  errorStore.info(
    tr('update.installReportTitle'),
    tr('update.installReportBody', { version: report.running || tr('common.unknown') }),
    details[String(report.result ?? '')] ?? '',
    'UpdateModal',
  )
}

// ---------- 忽略版本 ----------
async function doIgnore(): Promise<void> {
  if (!updateInfo.value?.latest_version) return
  try {
    await IgnoreVersion(updateInfo.value.latest_version)
    await ClearPendingUpdateInfo()
    // 忽略这个版本就不该再在退出时偷偷换上它：安排是隐形的（只在关停那一刻兑现），
    // 界面既然已经接受了「这个版本不要了」，就顺手把它撤掉。
    if (onExitPlanned.value) {
      await CancelInstallOnExit()
      onExitPlanned.value = false
    }
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
    // 条款还没同意时不抢先弹更新窗：一次只问用户一个必须回答的问题。
    if (licensePending.value) return
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

// 条款还没签时提示条会被闸门压在下面，所以等它让开再说这一次。
watch(licensePending, (pending) => { if (!pending) void reportLastInstall() }, { immediate: true })

// 手上一份包变成另一份包（下载完成、或后端扫出已下载的包）时重新问一次安排状态。
// immediate 是因为下载状态可能在这个组件挂载前就被恢复了，那种情况下也得显示对。
watch([updateDownloaded, updateDownloadPath], () => { void refreshOnExitPlan() }, { immediate: true })

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
    // 条款还没同意时不弹更新窗，也不消耗重试次数：等用户答复完再接着轮。
    if (licensePending.value) {
      setTimeout(pollPending, pollInterval)
      return
    }
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
    :title="checkFailed ? tr('update.fetchFailed') : (updateDownloaded ? tr('update.downloadedTitle') : (updateDownloading ? tr('update.downloading') : tr('update.foundNew')))"
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
            <p><strong>{{ tr('update.fetchFailed') }}</strong></p>
            <p>{{ tr('update.fetchFailedHint') }}</p>
          </div>
          <div class="update-fail-hint">
            <p>{{ tr('update.howToCheck') }} <a href="https://github.com/ws-cczj/CCZJ-Video/releases" target="_blank" class="update-link">{{ tr('update.releasesPage') }}</a>{{ tr('update.howToCheckTail') }}</p>
            <p>{{ tr('update.howToCheckEnd') }}</p>
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
            <p><strong>{{ tr('update.downloadedTitle') }}</strong></p>
            <p class="update-done-path">{{ updateDownloadPath }}</p>
          </div>
          <div class="update-done-hint">
            <p>{{ tr('update.downloadedMsg') }}</p>
            <p>{{ tr('update.downloadedMsg2') }}</p>
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
            <p><strong>{{ tr('update.downloading') }}</strong></p>
            <p class="update-downloading-sub">{{ tr('update.downloadingMsg') }}</p>
          </div>
          <!-- 进度条（始终显示） -->
          <div class="update-progress-full">
            <div class="progress-track" :class="{ indeterminate: updateDownloadProgress.total <= 0 }">
              <div
                class="progress-fill"
                :class="{ 'cczj-motion-progress-indeterminate': updateDownloadProgress.total <= 0 }"
                :style="updateDownloadProgress.total > 0 ? { width: updateDownloadProgress.percent + '%' } : {}"
              ></div>
            </div>
            <div class="progress-meta cczj-flex cczj-justify-between">
              <span v-if="updateDownloadProgress.downloaded === 0" class="update-connecting cczj-motion-pulse">
                {{ tr('update.connecting') }}
              </span>
              <span v-else-if="updateDownloadProgress.total > 0">
                {{ fmtSize(updateDownloadProgress.downloaded) }} / {{ fmtSize(updateDownloadProgress.total) }} ({{ updateDownloadProgress.percent }}%)
              </span>
              <span v-else>
                {{ tr('update.downloadedSize', { size: fmtSize(updateDownloadProgress.downloaded) }) }}
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
          <h4>{{ tr('update.changelog') }}</h4>
          <div class="update-notes-body">{{ updateInfo.release_notes }}</div>
        </div>

        <!-- 历史版本 -->
        <div v-if="updateInfo.history && updateInfo.history.length > 0" class="update-history">
          <h4>{{ tr('update.history') }}</h4>
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
          <Icon name="refresh" :size="14" /> {{ tr('update.recheck') }}
        </Button>
      </template>

      <!-- 下载完成界面按钮：两条安装档位，加上「这次先不管」 -->
      <template v-else-if="updateDownloaded">
        <Button variant="secondary" size="md" @click="doIgnore">
          {{ tr('update.ignore') }}
        </Button>
        <Button variant="secondary" size="md" :loading="scheduling" @click="doScheduleOnExit">
          <Icon name="clock" :size="14" /> {{ onExitPlanned ? tr('update.cancelOnExit') : tr('update.installOnExit') }}
        </Button>
        <span style="flex: 1"></span>
        <Button variant="primary" size="md" :loading="installing" @click="doInstall">
          <Icon name="zap" :size="14" /> {{ tr('update.installNow') }}
        </Button>
      </template>

      <!-- 下载中界面按钮 -->
      <template v-else-if="updateDownloading">
        <span class="bg-hint">{{ tr('update.bgNote') }}</span>
        <span style="flex: 1"></span>
        <Button variant="secondary" size="md" @click="updateModalOpen = false">
          {{ tr('update.bgDownload') }}
        </Button>
      </template>

      <!-- 更新信息界面按钮 -->
      <template v-else>
        <Button variant="secondary" size="md" @click="doIgnore">
          {{ tr('update.ignore') }}
        </Button>
        <span style="flex: 1"></span>
        <!-- 走 version.json 兜底时拿不到 download_url，点下去只会静默返回。
             与其给一个按不动的按钮，不如把原因写在按钮上。 -->
        <Button
          variant="primary"
          size="md"
          :disabled="!updateInfo?.download_url"
          :title="updateInfo?.download_url ? '' : tr('update.noDownloadUrl')"
          @click="doDownload"
        >
          <Icon name="download" :size="14" /> {{ tr('update.download') }}
        </Button>
      </template>
    </template>
  </Modal>

  <!-- 后台下载悬浮指示器（当弹窗关闭但正在下载时显示） -->
  <Teleport to="body">
    <MotionTransition preset="slide-up">
      <div
        v-if="updateDownloading && !updateModalOpen"
        class="bg-download-indicator"
        @click="updateModalOpen = true"
      >
        <div class="bg-download-icon">
          <Icon name="download" :size="14" />
        </div>
        <div class="bg-download-info">
          <span class="bg-download-title">{{ updateDownloadProgress.downloaded === 0 ? tr('update.connecting') : tr('update.downloadingShort') }}</span>
          <span v-if="updateDownloadProgress.total > 0 && updateDownloadProgress.downloaded > 0" class="bg-download-pct">{{ updateDownloadProgress.percent }}%</span>
          <span v-else class="bg-download-pct">...</span>
        </div>
        <div class="bg-download-track">
          <div
            class="bg-download-fill"
            :class="{ 'cczj-motion-progress-indeterminate': updateDownloadProgress.total <= 0 }"
            :style="updateDownloadProgress.total > 0 ? { width: updateDownloadProgress.percent + '%' } : {}"
          ></div>
        </div>
      </div>
    </MotionTransition>
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
}
.update-progress-full .progress-fill { height: 100%; background: var(--accent); border-radius: 4px; transition: width var(--cczj-motion-normal) var(--cczj-motion-ease-standard); }
.update-progress-full .progress-meta { font-size: 0.86rem; color: var(--text-muted); }
.update-connecting { color: var(--text-muted); }
.update-speed-placeholder { visibility: hidden; }
.update-file-info { display: flex; justify-content: space-between; align-items: center; padding: 10px 14px; background: var(--bg-secondary); border: 1px solid var(--border); border-radius: 8px; font-size: 0.86rem; }
.update-file-name { color: var(--text-primary); font-family: ui-monospace, Menlo, Monaco, Consolas, monospace; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; flex: 1; min-width: 0; margin-right: 12px; }
.update-file-size { color: var(--text-muted); flex-shrink: 0; }
.update-done-card { gap: 16px; padding: 16px 0; text-align: center; }
.update-done-icon { width: 64px; height: 64px; border-radius: 50%; background: var(--success); color: var(--success-contrast); box-shadow: 0 6px 18px var(--success-alpha-10); }
.update-done-text p { margin: 0 0 4px; font-size: 1.07rem; color: var(--text-primary); }
.update-done-path { font-size: 0.79rem !important; color: var(--text-muted) !important; font-family: ui-monospace, Menlo, Monaco, Consolas, monospace; word-break: break-all; margin-top: 6px !important; }
.update-done-hint { font-size: 0.86rem; color: var(--text-muted); line-height: 1.6; }
.update-done-hint p { margin: 0 0 4px; }
.update-fail-icon { width: 64px; height: 64px; border-radius: 50%; background: var(--warning); color: var(--warning-contrast); box-shadow: 0 6px 18px var(--warning-alpha-10); }
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
  /* 反馈型转圈不吃时长令牌：动画关掉时它得继续转，否则看不出还在下载。 */
  animation: cczj-spin 800ms linear infinite;
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
  height: 100%; background: var(--accent); border-radius: 2px; transition: width var(--cczj-motion-normal) var(--cczj-motion-ease-standard);
}
.bg-download-fill.cczj-motion-progress-indeterminate {
  width: 30%;
}

/* 后台指示器过渡动画 */
</style>
