<script setup lang="ts">
/**
 * 「这份数据比当前版本新」的启动提示。
 *
 * 触发条件只有一个：数据目录里的库被更新的程序写过（PRAGMA user_version 比本构建能认的最高
 * 版本还高）。走到这一步通常是两条路之一——拿旧 exe 开了新库，或者用「退回上一版」把老程序换了
 * 回来。两种都不是数据坏了，而是程序太旧。
 *
 * 为什么必须提：库里多出来的那些列会让本版本的 SELECT * 当场报错（sqlx 默认严格映射），用户
 * 看到的界面就是整页空白——和「数据没了」长得一模一样。Go 侧已经把这一种情形换成忽略未知列
 * （app/db/migrations.go 的 applySchemaAhead），数据读得出来了；这条提示负责说清为什么会读出
 * 来却不全，并把出口递到手边。
 *
 * 刻意不拦读写：拦下来的代价是用户连查看自己数据的入口都没了。取舍见
 * docs/adr/0011-read-newer-library.md。
 */
defineOptions({ name: 'SchemaNewerPrompt' })
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { OpenFolder, SchemaNotice } from '../api/app'
import { readStorage, writeStorage } from '../platform/storage'
import { useErrorStore } from '../stores/error'
import { legacyPromptOpen } from '../stores/legacyState'
import { licensePending } from '../stores/licenseState'
import { updateController, updateModalOpen } from '../stores/updateState'
import { Button, Modal } from './ui'

const { t } = useI18n()
const errorStore = useErrorStore()

// 记下「哪一版数据已经提过了」而不是存一个布尔：下次库被更新的程序写得更高时还得再提一次。
const DISMISS_KEY = 'cczj.schema-newer-dismissed'

const compat = ref<any | null>(null)
const open = ref(false)
const checking = ref(false)
let asked = false

// 排期：条款闸门 → 更新弹窗 → 旧数据 → 这条。同 z-index 的 Modal 同时开就只剩最后一个可见。
const clear = computed(
  () => !licensePending.value && !updateModalOpen.value && !legacyPromptOpen.value
)

function maybeOpen(): void {
  if (asked || !clear.value || !compat.value?.newer) return
  if (readStorage<number>(DISMISS_KEY, 0) >= Number(compat.value.db_version)) return
  asked = true
  open.value = true
}

async function scan(): Promise<void> {
  try {
    compat.value = await SchemaNotice()
  } catch {
    // 读不出就一句都不提：这条路径只负责解释，不负责报错，瞎猜版本号反而误导。
    return
  }
  maybeOpen()
}

async function openFolder(): Promise<void> {
  const dir = compat.value?.data_dir
  if (!dir) return
  try {
    await OpenFolder(dir)
  } catch (e: any) {
    errorStore.fromError(t('schema.openFailed'), e, 'SchemaNewerPrompt')
  }
}

// 关掉自己再叫检查更新：不关掉的话更新弹窗一冒出来就被这张压在下面（同一个 z-index）。
async function checkUpdate(): Promise<void> {
  const check = updateController.checkUpdate
  if (!check) return
  open.value = false
  checking.value = true
  try {
    await check()
  } finally {
    checking.value = false
  }
}

function dismiss(): void {
  writeStorage(DISMISS_KEY, Number(compat.value?.db_version ?? 0))
  open.value = false
}

watch(clear, (free) => { if (free) maybeOpen() })
onMounted(scan)
</script>

<template>
  <Modal
    :model-value="open"
    :title="t('schema.title')"
    width="min(560px, 92vw)"
    :closable="false"
    :mask-closable="false"
    :show-footer="true"
    @update:model-value="(v: boolean) => { if (!v) open = false }"
  >
    <div class="schema-body">
      <p class="schema-main">{{ t('schema.body', { db: compat?.db_version, build: compat?.build_version }) }}</p>
      <p>{{ t('schema.warn') }}</p>
      <p class="schema-hint">
        {{ t('schema.dir', { dir: compat?.data_dir }) }}
        <button type="button" class="schema-link" @click="openFolder">{{ t('schema.openFolder') }}</button>
      </p>
    </div>

    <template #footer>
      <Button variant="secondary" size="md" @click="dismiss">
        {{ t('schema.dismiss') }}
      </Button>
      <Button variant="primary" size="md" :loading="checking" @click="checkUpdate">
        {{ t('schema.checkUpdate') }}
      </Button>
    </template>
  </Modal>
</template>

<style scoped>
.schema-body {
  display: flex;
  flex-direction: column;
  gap: 10px;
  font-size: 0.88rem;
  line-height: 1.65;
  color: var(--text-secondary);
}
.schema-main {
  margin: 0;
  color: var(--text-primary);
}
.schema-hint {
  margin: 0;
  font-size: 0.8rem;
  color: var(--text-muted);
  word-break: break-all;
}
.schema-link {
  padding: 0;
  border: none;
  background: transparent;
  color: var(--accent);
  font-family: inherit;
  font-size: inherit;
  cursor: pointer;
  text-decoration: underline;
}
</style>
