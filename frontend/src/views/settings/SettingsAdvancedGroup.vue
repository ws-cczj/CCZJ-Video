<script setup lang="ts">
/**
 * 「高级」分组：播放代理的内网放行 + 数据备份 + 豆瓣队列。
 * 备份与豆瓣队列本来就是独立面板组件，这里只保留分组自己的那一节和它的即时读写。
 */
defineOptions({ name: 'SettingsAdvancedGroup' })
import { ref, onMounted } from 'vue'
import { GetAllowPrivateNetwork, SetAllowPrivateNetwork } from '../../api/app'
import { useErrorStore } from '../../stores/error'
import DataBackupPanel from '../../components/DataBackupPanel.vue'
import DoubanQueuePanel from '../../components/DoubanQueuePanel.vue'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const errorStore = useErrorStore()

// ---------- 播放代理：内网放行 ----------
// 默认关闭：代理只出公网，这是 SSRF 闸门的一部分。开关落在 Go 侧的即时生效语义，
// 所以这里失败必须报出来——静默吞掉会让用户以为已经放开，然后继续排查「为什么还放不出来」。
const allowPrivateNetwork = ref(false)

async function loadAllowPrivateNetwork(): Promise<void> {
  try { allowPrivateNetwork.value = await GetAllowPrivateNetwork() } catch { /* 忽略 */ }
}

async function saveAllowPrivateNetwork(): Promise<void> {
  const next = allowPrivateNetwork.value
  try {
    await SetAllowPrivateNetwork(next)
    errorStore.info(
      t('common.saved'),
      next ? t('advanced.proxyAllowPrivateOn') : t('advanced.proxyAllowPrivateOff'),
      '',
      'Settings.saveAllowPrivateNetwork',
    )
  } catch (e: any) {
    allowPrivateNetwork.value = !next
    errorStore.fromError(t('advanced.proxyAllowPrivateFailed'), e, 'Settings.saveAllowPrivateNetwork')
  }
}

onMounted(async () => {
  await loadAllowPrivateNetwork()
})
</script>

<template>
  <div class="panel group-card cczj-flex cczj-flex-col cczj-gap-2">
    <section class="block">
      <h3>{{ t('advanced.proxy') }}</h3>
      <p class="desc">{{ t('advanced.proxyDesc') }}</p>
      <div class="row cczj-flex cczj-items-center cczj-gap-7">
        <label class="toggle cczj-inline-flex cczj-items-center cczj-gap-4 cczj-cursor-pointer">
          <input type="checkbox" v-model="allowPrivateNetwork" @change="saveAllowPrivateNetwork" />
          <span>{{ t('advanced.proxyAllowPrivate') }}</span>
        </label>
      </div>
      <p class="desc">{{ t('advanced.proxyAllowPrivateDesc') }}</p>
    </section>

    <DataBackupPanel />

    <DoubanQueuePanel />
  </div>
</template>

<style scoped>
/* 这一组只有一节自绘的 block，其余是 DataBackupPanel / DoubanQueuePanel 自己的卡片，
   外框与压平仍由 Settings.vue 的 .panel.group-card（:deep）负责；
   h3 / .row / .toggle 这些内部元素拿不到父级 scope id，排版留在这里。 */
.block h3 {
  font-size: 0.97rem;
  font-weight: 700;
  margin: 0 0 12px;
  letter-spacing: 0.3px;
}
.row {
  gap: 14px;
}
.toggle {
  gap: 8px;
  font-size: 0.93rem;
  color: var(--text-primary);
}
.toggle input[type='checkbox'] {
  -webkit-appearance: none;
  appearance: none;
  width: 18px;
  height: 18px;
  border: 1.5px solid var(--border-strong);
  border-radius: 5px;
  background: var(--bg-card);
  cursor: pointer;
  position: relative;
  transition: all 0.15s ease;
  flex-shrink: 0;
}
.toggle input[type='checkbox']:hover {
  border-color: var(--accent);
}
.toggle input[type='checkbox']:checked {
  background: var(--accent);
  border-color: var(--accent);
}
.toggle input[type='checkbox']:checked::after {
  content: '';
  position: absolute;
  top: 3px;
  left: 5px;
  width: 4px;
  height: 8px;
  border: 2px solid var(--accent-contrast);
  border-top: 0;
  border-left: 0;
  transform: rotate(45deg);
}
.desc {
  color: var(--text-muted);
  font-size: 0.93rem;
  margin: 0 0 12px 0;
}
</style>
