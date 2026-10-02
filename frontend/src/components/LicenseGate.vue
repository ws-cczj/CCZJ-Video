<script setup lang="ts">
/**
 * 首启许可条款闸门。
 *
 * 只做一件事：本机没记下「已同意当前版本条款」就把条款摊在用户面前，同意才放行，
 * 不同意就退出。同意状态存在 Go 侧设置表里（不是 localStorage），因为它是随数据目录
 * 活得比浏览器存储久的记录；而它被刻意排除在备份导入之外——见 app/backup/service.go，
 * 一份已同意的备份不能替别人跳过这一步。
 *
 * 原文用 `?raw` 直接把仓库根的 LICENSE 打进包里，不复制第二份文案：条款只有一处真源，
 * 界面里的要点是导读，签的是那一版全文。
 */
defineOptions({ name: 'LicenseGate' })
import { computed, onMounted, onUnmounted, ref } from 'vue'
import licenseText from '../../../LICENSE?raw'
import { tr } from '../locales'
import { GetSetting, QuitApp, SetSetting } from '../api/app'
import { useErrorStore } from '../stores/error'
import { updateController } from '../stores/updateState'
import {
  LICENSE_TERMS_VERSION, licenseModalOpen, licensePending,
} from '../stores/licenseState'
import { Button, Modal } from './ui'

// 条款要点，正文在 locales 里。改动要同时动 zh 与 en 两份文案。
const clauseKeys = ['license.c1', 'license.c2', 'license.c3', 'license.c4', 'license.c5']

const errorStore = useErrorStore()

// 没读完不让点：防的是"看都不看一路回车"，也让用户知道下面那段是要读的。
const READ_SECONDS = 10

const saving = ref(false)
const showFull = ref(false)
const remain = ref(0)
let ticker: number | undefined

const canDismiss = computed(() => !licensePending.value)
const canAccept = computed(() => remain.value <= 0)

const agreeLabel = computed(() =>
  canAccept.value ? tr('license.agree') : tr('license.agreeWaiting', { s: remain.value }))

function startReadTimer(): void {
  stopReadTimer()
  remain.value = READ_SECONDS
  ticker = window.setInterval(() => {
    if (--remain.value <= 0) {
      remain.value = 0
      stopReadTimer()
    }
  }, 1000)
}

function stopReadTimer(): void {
  if (ticker !== undefined) {
    window.clearInterval(ticker)
    ticker = undefined
  }
}

async function refreshState(): Promise<void> {
  let accepted = ''
  try {
    accepted = await GetSetting('license_terms_version')
  } catch {
    // 读失败一律按「没同意」处理：闸门不能因为一次 IPC 出错就默认放行。
    accepted = ''
  }
  licensePending.value = accepted !== LICENSE_TERMS_VERSION
  if (licensePending.value) {
    licenseModalOpen.value = true
    startReadTimer()
  }
}

async function accept(): Promise<void> {
  if (!canAccept.value || saving.value) return
  saving.value = true
  try {
    await SetSetting('license_terms_version', LICENSE_TERMS_VERSION)
    licensePending.value = false
    licenseModalOpen.value = false
    stopReadTimer()
  } catch (e: any) {
    // 写不进去就还是不合规的状态，弹窗留着，让用户知道没保存成功。
    errorStore.fromError(tr('license.saveFailed'), e, 'LicenseGate.accept')
    return
  } finally {
    saving.value = false
  }
  // 同意之前 Go 侧不会去查更新（见 app/update/service.go），这一刻补上那一次检查。
  updateController.checkUpdate?.()
}

function decline(): void {
  // 不写任何记录，直接走优雅退出：库、日志和后台任务都按关停流程收尾。
  // 刻意不"清除本机数据"——那是删用户的东西，条款没要求，代码也不该擅自做。
  stopReadTimer()
  QuitApp()
}

function onClose(): void {
  licenseModalOpen.value = false
  stopReadTimer()
}

onMounted(refreshState)
onUnmounted(stopReadTimer)
</script>

<template>
  <Modal
    :model-value="licenseModalOpen"
    :title="tr('license.title')"
    width="min(680px, 94vw)"
    :closable="canDismiss"
    :mask-closable="canDismiss"
    :show-footer="true"
    @update:model-value="(v: boolean) => { if (!v) onClose() }"
  >
    <div class="license-body">
      <p class="license-intro">{{ tr('license.intro', { v: LICENSE_TERMS_VERSION }) }}</p>
      <ol class="license-clauses">
        <li v-for="key in clauseKeys" :key="key">{{ tr(key) }}</li>
      </ol>
      <p class="license-free">{{ tr('license.free') }}</p>

      <button class="license-toggle" type="button" @click="showFull = !showFull">
        {{ showFull ? tr('license.hideFull') : tr('license.showFull') }}
      </button>
      <pre v-if="showFull" class="license-full">{{ licenseText }}</pre>
    </div>

    <template #footer>
      <template v-if="licensePending">
        <Button variant="secondary" size="md" :disabled="saving" @click="decline">
          {{ tr('license.decline') }}
        </Button>
        <Button variant="primary" size="md" :disabled="!canAccept" :loading="saving" @click="accept">
          {{ agreeLabel }}
        </Button>
      </template>
      <Button v-else variant="primary" size="md" @click="onClose">
        {{ tr('license.close') }}
      </Button>
    </template>
  </Modal>
</template>

<style scoped>
.license-body {
  display: flex;
  flex-direction: column;
  gap: 12px;
  font-size: 0.89rem;
  line-height: 1.65;
  color: var(--text-secondary);
}
.license-intro {
  margin: 0;
  color: var(--text-primary);
}
.license-clauses {
  margin: 0;
  padding-left: 20px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.license-clauses li::marker {
  color: var(--accent);
  font-weight: 700;
}
.license-free {
  margin: 0;
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-left: 3px solid var(--warning);
  border-radius: 6px;
  background: var(--bg-secondary);
  color: var(--text-primary);
}
.license-toggle {
  align-self: flex-start;
  padding: 0;
  border: none;
  background: transparent;
  color: var(--accent);
  font-family: inherit;
  font-size: 0.84rem;
  font-weight: 600;
  cursor: pointer;
  text-decoration: underline;
}
.license-full {
  margin: 0;
  max-height: 40vh;
  overflow-y: auto;
  padding: 12px 14px;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: var(--bg-secondary);
  font-family: inherit;
  font-size: 0.8rem;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-word;
  color: var(--text-secondary);
}
</style>
