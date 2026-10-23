<script setup lang="ts">
import { tr } from '../../locales'
import { Select as SelectDropdown } from '../../components/ui'
import Icon from '../../components/Icon.vue'
import { useSourceDisplay, type ScheduleForm } from './useSourceDisplay'

/**
 * 卡片展开区 4：后台定时采集配置。
 * 表单对象由页面按 source_key 持有（搜源把卡片挤掉重挂也不丢改动），
 * 这里只改它的字段，开合与保存都交回页面走原来那条链路。
 */
defineProps<{ sourceKey: string; visible: boolean; form: ScheduleForm | null }>()

const emit = defineEmits<{
  toggle: []
  save: []
}>()

const { scheduleStateFor, modeLabel } = useSourceDisplay()
</script>

<template>
  <!-- 4. 后台定时采集配置 -->
  <div class="schedule-config-panel">
    <div v-if="visible && form" class="schedule-form-inline">
      <label class="sched-check">
        <input type="checkbox" v-model="form.enabled" />
        <span>{{ tr('sources.enableSchedule') }}</span>
      </label>
      <div v-if="form.enabled" class="sched-options">
        <div class="sched-row">
          <label>{{ tr('sources.scheduleMode') }}</label>
          <SelectDropdown v-model="form.mode" :options="[{ value: 'full', label: tr('sources.fullCollect') }, { value: 'incremental', label: tr('sources.incrementalCollect') }]" size="sm" />
        </div>
        <div class="sched-row">
          <label>{{ tr('sources.intervalMinutes') }}</label>
          <input type="number" v-model.number="form.interval_min" min="5" max="1440" class="sched-input" />
        </div>
      </div>
      <div class="sched-actions">
        <button class="mini-btn" @click="emit('toggle')">{{ tr('common.cancel') }}</button>
        <button class="mini-btn accent" @click="emit('save')">{{ tr('sources.saveConfig') }}</button>
      </div>
    </div>
    <div v-else class="schedule-summary">
      <span class="schedule-status-label">
        <Icon name="clock" :size="12" />
        {{ tr('sources.backendScheduleLabel') }}: {{ scheduleStateFor(sourceKey)?.enabled ? tr('sources.scheduleOnDetail', { mode: modeLabel(scheduleStateFor(sourceKey)?.mode || 'incremental'), n: scheduleStateFor(sourceKey)?.interval_min }) : tr('sources.scheduleOff') }}
      </span>
      <button class="mini-btn" @click="emit('toggle')">{{ tr('sources.configure') }}</button>
    </div>
  </div>
</template>

<style scoped>

/* === 4. 定时配置 === */
.schedule-config-panel {
  background: rgba(255,255,255,0.02);
  border: 1px solid rgba(76,175,80,0.2);
  border-radius: 10px;
  padding: 12px 14px;
}
.schedule-summary {
  @apply cczj-flex cczj-items-center cczj-justify-between;
}
.schedule-status-label {
  @apply cczj-inline-flex cczj-items-center cczj-gap-3 cczj-text-sm cczj-text-secondary;
}
.schedule-form-inline {
  @apply cczj-flex cczj-flex-col cczj-gap-5;
}
.sched-check {
  @apply cczj-inline-flex cczj-items-center cczj-gap-4 cczj-text-13 cczj-text-primary cczj-font-medium cczj-cursor-pointer;
}
.sched-check input[type='checkbox'] {
  -webkit-appearance: none; appearance: none;
  width: 18px; height: 18px;
  border: 1.5px solid var(--border-strong);
  border-radius: 5px;
  background: var(--bg-card);
  @apply cczj-cursor-pointer cczj-relative cczj-transition-fast cczj-flex-shrink-0;
}
.sched-check input[type='checkbox']:hover { @apply cczj-border-primary; }
.sched-check input[type='checkbox']:checked { background: var(--accent); @apply cczj-border-primary; }
.sched-check input[type='checkbox']:checked::after {
  content: '';
  @apply cczj-absolute;
  top: 3px; left: 5px;
  width: 4px; @apply cczj-h-2;
  border: 2px solid var(--accent-contrast);
  border-top: 0; border-left: 0;
  transform: rotate(45deg);
}
.sched-options {
  @apply cczj-flex cczj-flex-col cczj-gap-4;
  padding: 10px 12px;
  background: rgba(255,255,255,0.03);
  @apply cczj-rounded-md cczj-border;
}
.sched-row {
  @apply cczj-flex cczj-items-center cczj-gap-5;
}
.sched-row label { @apply cczj-text-sm cczj-text-secondary; min-width: 70px; @apply cczj-font-medium; }
.sched-select {
  padding: 5px 32px 5px 12px; @apply cczj-rounded;
  border: 1.5px solid var(--border-strong); background: var(--bg-card);
  @apply cczj-text-primary cczj-text-sm cczj-cursor-pointer cczj-outline-none;
  -webkit-appearance: none; appearance: none;
  background-image: url("data:image/svg+xml;charset=UTF-8,%3csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='none' stroke='%23999' stroke-width='2'%3e%3cpolyline points='6 9 12 15 18 9'/%3e%3c/svg%3e");
  background-repeat: no-repeat;
  background-position: right 6px center;
  background-size: 14px;
  @apply cczj-transition-fast;
}
.sched-select:hover { @apply cczj-border-primary; }
.sched-select:focus { @apply cczj-border-primary; box-shadow: 0 0 0 3px var(--accent-alpha-10); }
.sched-input {
  width: 80px; padding: 5px 12px; @apply cczj-rounded;
  border: 1.5px solid var(--border-strong); background: var(--bg-card);
  @apply cczj-text-primary cczj-text-sm cczj-outline-none cczj-text-center; font-family: 'SF Mono', Consolas, monospace;
  -webkit-appearance: none;
  @apply cczj-transition-fast;
}
.sched-input::-webkit-inner-spin-button,
.sched-input::-webkit-outer-spin-button { -webkit-appearance: none; margin: 0; }
.sched-input:hover { @apply cczj-border-primary; }
.sched-input:focus { @apply cczj-border-primary; box-shadow: 0 0 0 3px var(--accent-alpha-10); }
.sched-actions {
  @apply cczj-flex cczj-gap-4 cczj-justify-end cczj-mt-2;
}

</style>
