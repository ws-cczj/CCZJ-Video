<script setup lang="ts">
defineOptions({ name: 'UpdateModal' })
import { computed, ref, shallowRef, watch, onMounted, onUnmounted } from 'vue'
import { tr } from '../locales'
import {
  CheckUpdate, InstallUpdate, FileExists,
  IgnoreVersion, GetPendingUpdateInfo, ClearPendingUpdateInfo, GetLastInstallReport,
  ScheduleInstallOnExit, CancelInstallOnExit, GetPendingInstall,
} from '../api/app'
import { useErrorStore } from '../stores/error'
import {
  updateModalOpen, updateChecking,
  updateDownloading, updateDownloaded, updateDownloadPath, updateDownloadProgress,
  updateDownloadError,
  updateController, fmtSize, fmtSpeed,
  loadDownloadState, clearDownloadState, downloadUpdateArtifact, stopUpdateDownload,
  applyUpdateDownloadProgress,
} from '../stores/updateState'
import { licensePending } from '../stores/licenseState'
import Icon from './Icon.vue'
import { Button, Modal, MotionTransition } from './ui'
import { onBackendEvent } from '../api/events'
import { isMobileRuntime, isIOSRuntime } from '../api/runtime'
import { openAndroidUpdateInstaller } from '../platform/updateInstaller'
import { normalizeApiError } from '../api/errors'
import { createRequestScope } from '../api/requestScope'

const errorStore = useErrorStore()
const reads = createRequestScope()

// 本地状态（不共享）
const installing = ref(false)
const checkFailed = ref(false)
// 「退出时安装」这份安排的现场状态：按钮写「退出时安装」还是「取消安排」取决于它。
const onExitPlanned = ref(false)
const scheduling = ref(false)
let stopDownloadProgress: (() => void) | null = null
let stopUpdateAvailable: (() => void) | null = null
let checkRequest: ReturnType<typeof CheckUpdate> | null = null
let pendingPollTimer: ReturnType<typeof setTimeout> | undefined
let disposed = false
let planGeneration = 0

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
const updateInfo = shallowRef<UpdateInfoData | null>(null)

const backgroundTitle = computed(() => {
  if (updateDownloadError.value) return tr('update.downloadFailed')
  if (updateDownloaded.value) return tr(onExitPlanned.value ? 'update.onExitTitle' : 'update.downloadedTitle')
  if (updateDownloadProgress.downloaded === 0) return tr('update.connecting')
  if (updateDownloadProgress.total > 0 && updateDownloadProgress.downloaded >= updateDownloadProgress.total) return tr('update.verifying')
  return tr('update.downloadingShort')
})

const backgroundDetail = computed(() => {
  if (updateDownloadError.value) return tr('update.clickToRetry')
  if (updateDownloaded.value) return onExitPlanned.value
    ? tr('update.onExitTarget', { version: updateInfo.value?.latest_version || '' })
    : tr('update.readyToInstall')
  if (updateDownloadProgress.total > 0 && updateDownloadProgress.downloaded > 0) {
    return `${updateDownloadProgress.percent}% · ${fmtSpeed(updateDownloadProgress.speed_bps) || fmtSize(updateDownloadProgress.downloaded)}`
  }
  return updateDownloadProgress.downloaded > 0 ? fmtSize(updateDownloadProgress.downloaded) : '...'
})

// ---------- 检查更新 ----------
async function doCheckUpdate(): Promise<void> {
  if (updateChecking.value || updateDownloading.value || installing.value) {
    updateModalOpen.value = true
    return
  }
  updateChecking.value = true
  checkFailed.value = false
  const request = CheckUpdate()
  checkRequest = request
  // A manual check must finish even when several upstream mirrors time out.
  const deadline = setTimeout(() => { void request.cancel() }, 30_000)
  try {
    const info = await request
    if (disposed || checkRequest !== request || !info) return
    updateInfo.value = info
    if (info.has_update) {
      // 检查是否有已下载的安装包
      await checkAlreadyDownloaded(info)
      if (disposed || checkRequest !== request) return
      updateModalOpen.value = true
    } else {
      errorStore.info(tr('update.checking'), tr('update.latestVersion', { version: info.current_version }))
    }
  } catch (e: any) {
    if (disposed || checkRequest !== request || normalizeApiError(e).code === 'CANCELLED') return
    checkFailed.value = true
    updateModalOpen.value = true
  } finally {
    clearTimeout(deadline)
    if (checkRequest === request) {
      checkRequest = null
      updateChecking.value = false
    }
  }
}

// ---------- 下载更新 ----------
async function doDownload(): Promise<void> {
  if (!updateInfo.value?.download_url || updateDownloading.value) return
  const requestedUpdate = updateInfo.value
  try {
    await downloadUpdateArtifact(requestedUpdate.download_url, requestedUpdate.latest_version)
  } catch (e: any) {
    if (disposed || normalizeApiError(e).code === 'CANCELLED') return
    errorStore.fromError(tr('update.downloadFailed'), e, 'UpdateModal.downloadUpdate')
  }
}

// ---------- 安装更新（档位一：立即安装） ----------
async function doInstall(): Promise<void> {
  if (!updateDownloadPath.value) return
  installing.value = true
  try {
    // Go 那边一进 Install 就取代退出时安装的安排，所以不论这次成不成，界面都不该再
    // 显示「取消安排」。
    onExitPlanned.value = false
    // 注意：不要在这里调用 ClearPendingUpdateInfo()！
    // 桌面 InstallUpdate 会先读取 pendingUpdateInfo 记录版本号，再清理
    await InstallUpdate(updateDownloadPath.value)
    if (isMobileRuntime()) {
      openAndroidUpdateInstaller(updateDownloadPath.value)
      installing.value = false
      errorStore.info(tr('update.androidInstallerOpened'), tr('update.androidInstallerHint'), '', 'UpdateModal')
      return
    }
    clearDownloadState()
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
  const generation = ++planGeneration
  const path = updateDownloadPath.value
  if (!updateDownloaded.value || !updateDownloadPath.value) {
    onExitPlanned.value = false
    return
  }
  try {
    const plan = await reads.run(() => GetPendingInstall())
    if (disposed || generation !== planGeneration || path !== updateDownloadPath.value) return
    onExitPlanned.value = plannedForThisArtifact(plan)
  } catch {
    if (disposed || generation !== planGeneration || path !== updateDownloadPath.value) return
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
    report = await reads.run(GetLastInstallReport)
  } catch {
    return // 问不到回执就闭嘴：凭空说「上次更新失败」比沉默更糟
  }
  if (disposed || !report?.failed) return
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
function onUpdateAvailable(data: UpdateInfoData): void {
  if (updateDownloading.value || installing.value) return
  if (data && data.has_update) {
    // 条款还没同意时不抢先弹更新窗：一次只问用户一个必须回答的问题。
    if (licensePending.value) return
    updateInfo.value = data
    // 检查是否有已下载的安装包
    checkAlreadyDownloaded(data).then(() => {
      if (disposed || updateInfo.value !== data) return
      updateModalOpen.value = true
    })
  }
}

// 检查是否有已下载的安装包
// Go 后端扫描结果是包的版本与校验状态的权威来源。
async function checkAlreadyDownloaded(info: UpdateInfoData): Promise<void> {
  if (disposed || updateInfo.value !== info || updateDownloading.value) return
  updateDownloaded.value = false
  updateDownloadPath.value = ''
  updateDownloadError.value = false
  // Go 后端只返回当前 release 的已校验包。
  const backendPath = info.already_downloaded_path
  if (backendPath && info.already_downloaded_version === info.latest_version) {
    try {
      const exists = await reads.run(() => FileExists(backendPath))
      if (disposed || updateInfo.value !== info || updateDownloading.value) return
      if (exists) {
        updateDownloadPath.value = backendPath
        updateDownloaded.value = true
        return
      }
    } catch { /* fall through */ }
  }

  // A legacy cache only proves existence, not release or integrity.
  if (disposed || updateInfo.value !== info || updateDownloading.value) return
  clearDownloadState()
}

// 条款还没签时提示条会被闸门压在下面，所以等它让开再说这一次。
watch(licensePending, (pending) => { if (!pending) void reportLastInstall() }, { immediate: true })

// 手上一份包变成另一份包（下载完成、或后端扫出已下载的包）时重新问一次安排状态。
// immediate 是因为下载状态可能在这个组件挂载前就被恢复了，那种情况下也得显示对。
watch([updateDownloaded, updateDownloadPath], () => { void refreshOnExitPlan() }, { immediate: true })

onMounted(async () => {
  if (isIOSRuntime()) return
  stopDownloadProgress = onBackendEvent('update:download:progress', applyUpdateDownloadProgress)
  stopUpdateAvailable = onBackendEvent('update:available', onUpdateAvailable)

  // 注册全局函数
  updateController.checkUpdate = doCheckUpdate
  updateController.openModal = () => { updateModalOpen.value = true }

  // 重试轮询待处理更新（Go 网络检查可能耗时较长）
  let pollRetries = 0
  const maxPollRetries = 15
  const pollInterval = 5000

  const pollPending = async () => {
    if (disposed) return
    // 条款还没同意时不弹更新窗，也不消耗重试次数：等用户答复完再接着轮。
    if (licensePending.value) {
      pendingPollTimer = setTimeout(pollPending, pollInterval)
      return
    }
    if (updateModalOpen.value || pollRetries >= maxPollRetries) return
    pollRetries++
    try {
      const pending = await reads.run(GetPendingUpdateInfo)
      if (disposed) return
      if (pending && pending.has_update) {
        updateInfo.value = pending
        // 检查是否有已下载的安装包
        await checkAlreadyDownloaded(pending)
        if (disposed || updateInfo.value !== pending) return
        updateModalOpen.value = true
        return
      }
    } catch { /* ignore */ }
    if (!disposed && pollRetries < maxPollRetries) {
      pendingPollTimer = setTimeout(pollPending, pollInterval)
    }
  }

  // 延迟 5 秒后开始首次轮询
  pendingPollTimer = setTimeout(pollPending, pollInterval)
})

onUnmounted(() => {
  disposed = true
  stopUpdateDownload()
  reads.cancel()
  if (updateController.checkUpdate === doCheckUpdate) updateController.checkUpdate = null
  if (pendingPollTimer !== undefined) clearTimeout(pendingPollTimer)
  void checkRequest?.cancel()
  if (checkRequest) updateChecking.value = false
  checkRequest = null
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
        <Button v-if="!isMobileRuntime()" variant="secondary" size="md" :loading="scheduling" @click="doScheduleOnExit">
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
        v-if="(updateDownloading || updateDownloaded || updateDownloadError) && !updateModalOpen"
        class="bg-download-indicator"
        role="button"
        tabindex="0"
        @click="updateModalOpen = true"
        @keydown.enter="updateModalOpen = true"
        @keydown.space.prevent="updateModalOpen = true"
      >
        <div class="bg-download-icon" :class="{ 'is-idle': !updateDownloading }">
          <Icon :name="updateDownloadError ? 'alert-circle' : (updateDownloaded ? 'check' : 'download')" :size="14" />
        </div>
        <div class="bg-download-info">
          <span class="bg-download-title">{{ backgroundTitle }}</span>
          <span class="bg-download-pct">{{ backgroundDetail }}</span>
        </div>
        <div v-if="updateDownloading" class="bg-download-track">
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
.update-header { @apply cczj-gap-8; padding: 8px 0 16px; border-bottom: 1px dashed var(--border); @apply cczj-mb-8; }
.update-icon { width: 52px; height: 52px; border-radius: 14px; background: var(--accent); color: var(--accent-contrast); box-shadow: 0 6px 18px var(--accent-alpha-35); @apply cczj-flex-shrink-0; }
.update-title { font-size: 1.14rem; @apply cczj-font-bold; margin: 0 0 4px; @apply cczj-text-primary; }
.update-versions { margin: 0; font-size: 0.93rem; @apply cczj-flex cczj-items-center cczj-gap-5; }
.update-current { @apply cczj-text-muted; font-family: ui-monospace, Menlo, Monaco, Consolas, monospace; }
.update-arrow { @apply cczj-text-muted; font-size: 1.14rem; }
.update-latest { @apply cczj-text-accent cczj-font-bold; font-family: ui-monospace, Menlo, Monaco, Consolas, monospace; }
.update-notes { @apply cczj-mb-8; }
.update-notes h4 { font-size: 0.86rem; @apply cczj-font-semibold cczj-text-secondary; margin: 0 0 8px; text-transform: uppercase; letter-spacing: 0.5px; }
.update-notes-body { background: var(--bg-secondary); @apply cczj-border; border-radius: 10px; padding: 12px 14px; font-size: 0.93rem; line-height: 1.6; @apply cczj-text-secondary; max-height: 200px; @apply cczj-overflow-y-auto cczj-whitespace-pre; word-break: break-word; }
.update-progress-full { @apply cczj-w-full; max-width: 400px; }
.update-progress-full .progress-track { @apply cczj-h-2; background: var(--bg-secondary); @apply cczj-border cczj-rounded-sm cczj-overflow-hidden cczj-mb-4 cczj-relative; }
.update-progress-full .progress-track.indeterminate .progress-fill {
  width: 30%;
}
.update-progress-full .progress-fill { @apply cczj-h-full; background: var(--accent); @apply cczj-rounded-sm; transition: width var(--cczj-motion-normal) var(--cczj-motion-ease-standard); }
.update-progress-full .progress-meta { font-size: 0.86rem; @apply cczj-text-muted; }
.update-connecting { @apply cczj-text-muted; }
.update-speed-placeholder { visibility: hidden; }
.update-file-info { @apply cczj-flex cczj-justify-between cczj-items-center cczj-px-7-py-5; background: var(--bg-secondary); @apply cczj-border cczj-rounded-md; font-size: 0.86rem; }
.update-file-name { @apply cczj-text-primary; font-family: ui-monospace, Menlo, Monaco, Consolas, monospace; @apply cczj-overflow-hidden; text-overflow: ellipsis; @apply cczj-whitespace-nowrap cczj-flex-1 cczj-min-w-0 cczj-mr-6; }
.update-file-size { @apply cczj-text-muted cczj-flex-shrink-0; }
.update-done-card { @apply cczj-gap-8; padding: 16px 0; @apply cczj-text-center; }
.update-done-icon { width: 64px; height: 64px; @apply cczj-rounded-50; background: var(--success); color: var(--success-contrast); box-shadow: 0 6px 18px var(--success-alpha-10); }
.update-done-text p { margin: 0 0 4px; font-size: 1.07rem; @apply cczj-text-primary; }
.update-done-path { font-size: 0.79rem !important; color: var(--text-muted) !important; font-family: ui-monospace, Menlo, Monaco, Consolas, monospace; @apply cczj-break-all; margin-top: 6px !important; }
.update-done-hint { font-size: 0.86rem; @apply cczj-text-muted; line-height: 1.6; }
.update-done-hint p { margin: 0 0 4px; }
.update-fail-icon { width: 64px; height: 64px; @apply cczj-rounded-50; background: var(--warning); color: var(--warning-contrast); box-shadow: 0 6px 18px var(--warning-alpha-10); }
.update-fail-hint { font-size: 0.86rem; @apply cczj-text-muted; line-height: 1.6; @apply cczj-text-left; padding: 0 8px; }
.update-fail-hint p { margin: 0 0 4px; }
.update-link { @apply cczj-text-accent cczj-underline cczj-cursor-pointer; }
.update-link:hover { @apply cczj-opacity-80; }
.update-history { @apply cczj-mt-8 cczj-pt-6 cczj-border-top; }
.update-history h4 { font-size: 0.91rem; @apply cczj-font-semibold cczj-text-primary; margin: 0 0 10px; }
.update-history-item { @apply cczj-mb-6; padding: 10px 12px; background: var(--bg-secondary); @apply cczj-rounded-md; }
.update-history-item h5 { font-size: 0.89rem; @apply cczj-font-semibold cczj-text-primary; margin: 0 0 6px; }
.update-history-item pre { font-size: 0.82rem; @apply cczj-text-secondary cczj-whitespace-pre; margin: 0; @apply cczj-leading-normal; font-family: inherit; }

/* 下载中界面 */
.update-downloading-card { padding: 24px 0; @apply cczj-text-center; }
.update-loading-icon {
  width: 56px; height: 56px; @apply cczj-rounded-50;
  background: var(--accent-alpha-15); @apply cczj-text-accent;
  animation: pulse-download 2s ease-in-out infinite;
}
@keyframes pulse-download {
  0%, 100% { transform: scale(1); box-shadow: 0 0 0 0 var(--accent-alpha-20); }
  50% { transform: scale(1.05); box-shadow: 0 0 0 12px transparent; }
}
.update-downloading-title p { margin: 0 0 4px; font-size: 1.07rem; @apply cczj-text-primary; }
.update-downloading-sub { font-size: 0.86rem !important; color: var(--text-muted) !important; }

/* 后台下载提示文字 */
.bg-hint { font-size: 0.82rem; @apply cczj-text-muted; }

/* ====== 后台下载悬浮指示器 ====== */
.bg-download-indicator {
  @apply cczj-fixed;
  right: 24px;
  bottom: 88px;
  z-index: var(--z-fab);
  @apply cczj-flex cczj-items-center cczj-gap-4 cczj-px-7-py-5;
  background: var(--bg-card);
  border: 1px solid var(--accent);
  border-radius: 24px;
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.15), 0 0 0 3px var(--accent-alpha-10);
  @apply cczj-cursor-pointer;
  transition: transform 0.2s ease, box-shadow 0.2s ease;
  min-width: 160px;
}
.bg-download-indicator:hover {
  transform: translateY(-2px);
  box-shadow: 0 6px 20px rgba(0, 0, 0, 0.2), 0 0 0 4px var(--accent-alpha-15);
}
.bg-download-icon {
  width: 28px; height: 28px; @apply cczj-rounded-50;
  background: var(--accent); color: var(--accent-contrast);
  @apply cczj-flex cczj-items-center cczj-justify-center cczj-flex-shrink-0;
  /* 反馈型转圈不吃时长令牌：动画关掉时它得继续转，否则看不出还在下载。 */
  animation: cczj-spin 800ms linear infinite;
}
.bg-download-info {
  @apply cczj-flex cczj-flex-col cczj-flex-1 cczj-min-w-0;
}
.bg-download-icon.is-idle { animation: none; }
.bg-download-title { font-size: 0.79rem; @apply cczj-font-semibold cczj-text-primary; }
.bg-download-pct { font-size: 0.72rem; @apply cczj-text-muted; font-family: ui-monospace, monospace; }
.bg-download-track {
  @apply cczj-absolute cczj-bottom-0; left: 12px; right: 12px;
  height: 3px; background: var(--bg-secondary); @apply cczj-rounded-xs cczj-overflow-hidden;
}
.bg-download-fill {
  @apply cczj-h-full; background: var(--accent); @apply cczj-rounded-xs; transition: width var(--cczj-motion-normal) var(--cczj-motion-ease-standard);
}
.bg-download-fill.cczj-motion-progress-indeterminate {
  width: 30%;
}

/* 后台指示器过渡动画 */
</style>
