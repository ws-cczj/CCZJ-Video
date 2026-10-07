<script setup lang="ts">
/**
 * 首启的「旧数据找回」提示。
 *
 * 2.1.0 把数据目录从程序旁边挪到了用户配置目录，但更新是原地热替换 exe：新目录第一次启动
 * 就建好了空库，之后 2.3.x 的自动迁移看到「两处都有库」只能停手（猜错方向会覆盖用户这一侧
 * 的数据）。停手是对的，代价是那份旧库再没人提起——用户看到的就是「升级后打开是初始状态」。
 *
 * 这里补的就是那一次提起：只报后端算好的现场（路径、大小、能找回多少），点不点合并的是用户。
 * 刻意不自动合并：旧库里的设置会把这台机器现在的偏好盖掉，被删掉的收藏也会复活。
 * 合并走 app/backup 的只进不出通道，旧库以只读方式打开，一个字节都不改。
 */
defineOptions({ name: 'LegacyDataPrompt' })
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { LegacyDataMerge, LegacyDataNotice } from '../api/app'
import { readStorageBoolean, writeStorage } from '../platform/storage'
import { useErrorStore } from '../stores/error'
import { legacyPromptOpen } from '../stores/legacyState'
import { licensePending } from '../stores/licenseState'
import { updateModalOpen } from '../stores/updateState'
import { Button, Modal } from './ui'

const { t } = useI18n()
const errorStore = useErrorStore()

// 「暂时不用」记在 localStorage：它表达的只是这台机器的界面别再自动提，
// 而设置页那一栏不受它影响，随时还能找回（Go 侧的 legacy_data_status 只在真的处理过才写）。
const DISMISS_KEY = 'cczj.legacy-data-dismissed'

const notice = ref<any | null>(null)
// 开关放在 store 里：SchemaNewerPrompt 要排在它后面，同 z-index 不能同时开。
const open = legacyPromptOpen
const merging = ref(false)
let asked = false

// 启动时的四个全局 Modal 共用一个 z-index，叠在一起就是「更新提示神秘消失」。
// 排期：条款闸门 → 更新弹窗 → 旧数据 → 数据比程序新。
const clear = computed(() => !licensePending.value && !updateModalOpen.value)

function maybeOpen(): void {
  if (asked || !clear.value || !notice.value?.available) return
  if (readStorageBoolean(DISMISS_KEY)) return
  asked = true
  open.value = true
}

async function scan(): Promise<void> {
  try {
    notice.value = await LegacyDataNotice()
  } catch {
    // 读不出就一句都不提：这条路径的兜底是设置页里的同一块面板，它会如实报错。
    return
  }
  maybeOpen()
}

async function merge(): Promise<void> {
  if (merging.value) return
  merging.value = true
  try {
    const result: any = await LegacyDataMerge()
    errorStore.info(t('legacy.doneTitle'), t('legacy.done', {
      sources: result.sources_added,
      favorites: result.favorites_added,
      history: result.history_applied,
    }))
    notice.value = { ...notice.value, available: false, status: 'merged' }
    open.value = false
  } catch (e: any) {
    errorStore.fromError(t('legacy.mergeFailed'), e, 'LegacyDataPrompt')
  } finally {
    merging.value = false
  }
}

function dismiss(): void {
  writeStorage(DISMISS_KEY, true)
  open.value = false
}

watch(clear, (free) => { if (free) maybeOpen() })
onMounted(scan)
</script>

<template>
  <Modal
    :model-value="open"
    :title="t('legacy.promptTitle')"
    width="min(560px, 92vw)"
    :closable="false"
    :mask-closable="false"
    :show-footer="true"
    @update:model-value="(v: boolean) => { if (!v) open = false }"
  >
    <div class="legacy-body">
      <p class="legacy-main">{{ t('legacy.promptBody', { dir: notice?.legacy_dir }) }}</p>
      <p>{{ t('legacy.promptAsk') }}</p>
      <p class="legacy-hint">{{ t('legacy.promptHint') }}</p>
    </div>

    <template #footer>
      <Button variant="secondary" size="md" :disabled="merging" @click="dismiss">
        {{ t('legacy.promptDismiss') }}
      </Button>
      <Button variant="primary" size="md" :loading="merging" @click="merge">
        {{ t('legacy.promptMerge') }}
      </Button>
    </template>
  </Modal>
</template>

<style scoped>
.legacy-body {
  display: flex;
  flex-direction: column;
  gap: 10px;
  font-size: 0.88rem;
  line-height: 1.65;
  color: var(--text-secondary);
}
.legacy-main {
  margin: 0;
  color: var(--text-primary);
  word-break: break-all;
}
.legacy-hint {
  margin: 0;
  font-size: 0.8rem;
  color: var(--text-muted);
}
</style>
