<script setup lang="ts">
import { computed, ref } from 'vue'
import { tr } from '../../locales'
import { useCollectStore } from '../../stores/collect'
import { useErrorStore } from '../../stores/error'
import type { CollectScheduleConfig } from '../../stores/collect'
import Icon from '../../components/Icon.vue'
import { Button } from '../../components/ui'

/**
 * 全局调度器状态条 + 全局调度参数。
 *
 * 采集相关的节奏与节流只在这一页改：状态条读 collect store 的调度器快照，
 * 表单编辑的是一份副本，保存成功才回写 store。
 */
const collectStore = useCollectStore()
const errorStore = useErrorStore()

const globalScheduleVisible = ref(false)
const globalSchedule = ref<CollectScheduleConfig | null>(null)

const globalIntervalMinutes = computed<number>({
  get: () => Math.max(1, Math.round((globalSchedule.value?.background_interval_seconds || 60) / 60)),
  set: (minutes) => {
    if (globalSchedule.value) globalSchedule.value.background_interval_seconds = Math.max(1, minutes) * 60
  },
})

function toggleGlobalSchedule(): void {
  globalScheduleVisible.value = !globalScheduleVisible.value
  if (!globalScheduleVisible.value) return
  const cfg = collectStore.scheduleConfig
  globalSchedule.value = cfg ? { ...cfg } : null
}

async function saveGlobalSchedule(): Promise<void> {
  const cfg = globalSchedule.value
  if (!cfg) return
  try {
    await collectStore.saveSchedule(cfg)
    const saved = collectStore.scheduleConfig
    if (saved) globalSchedule.value = { ...saved }
    errorStore.info(tr('common.saved'), tr('sources.scheduleSaved'), '', 'Sources.saveGlobalSchedule')
  } catch (e) {
    errorStore.fromError(tr('sources.scheduleSaveFailed'), e, 'Sources.saveGlobalSchedule')
  }
}
</script>

<template>
  <!-- 包装层只为满足单根元素：display: contents 让它不生成盒子，
       状态条和参数面板仍按页面流的直接子元素排布，间距与拆分前一致。 -->
  <div class="global-schedule">
    <!-- 全局调度器状态条 -->
    <div v-if="collectStore.schedulerStatus" class="scheduler-bar" :class="{ active: collectStore.schedulerStatus.running }">
      <div class="scheduler-indicator" :class="{ on: collectStore.schedulerStatus.running }"></div>
      <span class="scheduler-label">{{ tr('sources.backendScheduler') }}</span>
      <span class="scheduler-state">{{ collectStore.schedulerStatus.running ? tr('sources.statusRunning') : tr('sources.statusStopped') }}</span>
      <span class="scheduler-note">{{ collectStore.schedulerStatus.note }}</span>
      <div class="scheduler-actions">
        <button v-if="collectStore.schedulerStatus.running" class="mini-btn danger" @click="collectStore.stopBackground()">
          <Icon name="stop" :size="10" /><span>{{ tr('common.stop') }}</span>
        </button>
        <button v-else class="mini-btn accent" @click="collectStore.triggerNow()">
          <Icon name="play" :size="10" /><span>{{ tr('sources.startFull') }}</span>
        </button>
        <button class="mini-btn" :class="{ accent: globalScheduleVisible }" @click="toggleGlobalSchedule">
          <Icon name="settings" :size="10" /><span>{{ tr('sources.scheduleSettings') }}</span>
        </button>
        <button class="mini-btn" @click="collectStore.loadSchedule()">
          <Icon name="refresh" :size="10" /><span>{{ tr('common.refresh') }}</span>
        </button>
      </div>
    </div>

    <!-- 全局调度参数（采集相关的节奏与节流只在这一页改） -->
    <div v-if="globalScheduleVisible && globalSchedule" class="scheduler-config">
      <div class="sc-row">
        <label class="sc-check">
          <input type="checkbox" v-model="globalSchedule.enable_background" />
          <span>{{ tr('sources.enableGlobalSchedule') }}</span>
        </label>
        <div class="sc-field">
          <span class="sc-label">{{ tr('sources.scheduleInterval') }}</span>
          <input type="range" v-model.number="globalIntervalMinutes" min="1" max="180" step="1" />
          <span class="sc-value">{{ tr('sources.minuteCount', { n: globalIntervalMinutes }) }}</span>
        </div>
      </div>
      <div class="sc-row">
        <div class="sc-field">
          <span class="sc-label">{{ tr('sources.scheduleSourceGap') }}</span>
          <input class="sc-input" type="number" min="1" max="600" v-model.number="globalSchedule.source_gap_seconds" />
        </div>
        <div class="sc-field">
          <span class="sc-label">{{ tr('sources.schedulePageGap') }}</span>
          <input class="sc-input" type="number" min="1" max="600" v-model.number="globalSchedule.page_gap_seconds" />
        </div>
        <label class="sc-check">
          <input type="checkbox" v-model="globalSchedule.enable_startup_catchup" />
          <span>{{ tr('sources.scheduleStartupCatchup') }}</span>
        </label>
        <label class="sc-check">
          <input type="checkbox" v-model="globalSchedule.enable_initial_full_collect" />
          <span>{{ tr('sources.scheduleInitialFull') }}</span>
        </label>
      </div>
      <div class="sc-row">
        <Button variant="primary" size="sm" :loading="collectStore.scheduleSaving" @click="saveGlobalSchedule">
          <Icon name="save" :size="12" /><span>{{ tr('common.save') }}</span>
        </Button>
        <small class="sc-hint">{{ tr('sources.scheduleHint') }}</small>
      </div>
    </div>
  </div>
</template>

<style scoped>

/* 包装层不生成盒子，见模板注释 */
.global-schedule {
  display: contents;
}

/* === 全局调度器条 === */
.scheduler-bar {
  @apply cczj-flex cczj-items-center cczj-gap-5;
  padding: 10px 16px;
  @apply cczj-mb-8;
  background: var(--bg-card);
  @apply cczj-border;
  border-radius: 10px;
  @apply cczj-text-13;
}
.scheduler-bar.active { border-color: var(--accent-alpha-30); }
.scheduler-indicator {
  @apply cczj-w-2 cczj-h-2 cczj-rounded-50;
  background: var(--text-muted); @apply cczj-flex-shrink-0;
}
.scheduler-indicator.on {
  background: #4caf50;
  box-shadow: 0 0 6px #4caf50;
  animation: pulse 2s ease-in-out infinite;
}
@keyframes pulse {
  0%, 100% { box-shadow: 0 0 4px #4caf50; }
  50% { box-shadow: 0 0 12px #4caf50; }
}
.scheduler-label { @apply cczj-font-semibold cczj-text-secondary; }
.scheduler-state { @apply cczj-font-semibold cczj-text-accent; }
.scheduler-note { @apply cczj-flex-1 cczj-text-muted cczj-text-sm cczj-overflow-hidden; text-overflow: ellipsis; @apply cczj-whitespace-nowrap; }
.scheduler-actions { @apply cczj-flex cczj-gap-3 cczj-flex-shrink-0; }
.scheduler-config {
  @apply cczj-flex cczj-flex-col cczj-gap-5;
  margin: -8px 0 16px; @apply cczj-px-8-py-6;
  background: var(--bg-card); border: 1px solid var(--accent-alpha-30);
  border-radius: 10px;
}
.sc-row { @apply cczj-flex cczj-items-center cczj-gap-8 cczj-flex-wrap; }
.sc-field { @apply cczj-flex cczj-items-center cczj-gap-4 cczj-text-sm cczj-text-secondary; }
.sc-label { @apply cczj-text-muted cczj-whitespace-nowrap; }
.sc-value { min-width: 52px; @apply cczj-text-primary cczj-font-semibold; }
.sc-input {
  width: 68px; padding: 5px 8px; @apply cczj-rounded cczj-border; background: var(--bg-secondary);
  @apply cczj-text-primary cczj-text-sm cczj-outline-none;
}
.sc-input:hover, .sc-input:focus { @apply cczj-border-primary; }
.sc-input:focus { box-shadow: 0 0 0 3px var(--accent-alpha-10); }
.sc-input::-webkit-inner-spin-button,
.sc-input::-webkit-outer-spin-button { -webkit-appearance: none; margin: 0; }
.sc-input[type='number'] { -moz-appearance: textfield; appearance: textfield; }
.sc-field input[type='range'] { accent-color: var(--accent); @apply cczj-cursor-pointer; }
.sc-check {
  @apply cczj-inline-flex cczj-items-center cczj-gap-3 cczj-text-sm cczj-text-secondary cczj-cursor-pointer;
}
.sc-check input { accent-color: var(--accent); @apply cczj-cursor-pointer; }
.sc-hint { @apply cczj-text-muted; }

</style>
