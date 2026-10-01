<script setup lang="ts">
import { tr } from '../../locales'
import { useCollectStore } from '../../stores/collect'
import Icon from '../../components/Icon.vue'
import { Tag, Select as SelectDropdown } from '../../components/ui'
import { useSourceDisplay, type ParamsDoc, type ProbeSlot, type ScheduleForm, type SourceRow } from './useSourceDisplay'
import SourceCollectPanel from './SourceCollectPanel.vue'
import SourceLogPanel from './SourceLogPanel.vue'
import SourceOpsPanel from './SourceOpsPanel.vue'
import SourceSchedulePanel from './SourceSchedulePanel.vue'
import SourceParamsGuide from './SourceParamsGuide.vue'

/**
 * 一个采集源的卡片：头部（状态 + 模式 + 探测点阵）、操作行、展开后的四块面板。
 *
 * 展开与否由页面按 source_key 决定（expandedKey），这里只收一份 expanded，
 * 所以点一张卡不会连带展开邻居。探测也只往上抛事件，请求仍由页面发。
 */
const props = defineProps<{
  source: SourceRow
  sourceKey: string
  expanded: boolean
  mode: string
  hours: number
  slots: ProbeSlot[]
  cooling: string
  probeBusy: boolean
  isDefault: boolean
  scheduleVisible: boolean
  scheduleForm: ScheduleForm | null
  doc: ParamsDoc | null
  docLoading: boolean
}>()

const emit = defineEmits<{
  toggle: []
  'update-mode': [mode: string]
  'update-hours': [hours: number]
  collect: []
  'collect-incremental': []
  probe: []
  export: []
  'set-default': []
  edit: []
  delete: []
  'schedule-toggle': []
  'schedule-save': []
  'params-toggle': []
}>()

const collectStore = useCollectStore()
const {
  isRunning, isPaused, statusClass, modeLabel, modeTagVariant,
  progressFor, formatProgress, dotClass, dotTitle, scheduleStateFor,
} = useSourceDisplay()
</script>

<template>
  <div class="source-card" :class="{ expanded: props.expanded, running: isRunning(props.sourceKey) || isPaused(props.sourceKey) }">
    <!-- 卡片头部（始终可见） -->
    <div class="card-header" @click="emit('toggle')">
      <div class="card-header-left">
        <span class="status-dot" :class="statusClass(props.sourceKey)"></span>
        <h3 class="card-name">{{ props.source.name }}</h3>
        <Tag :variant="modeTagVariant(props.mode)" size="sm">{{ modeLabel(props.mode) }}</Tag>
        <span v-if="scheduleStateFor(props.sourceKey)?.enabled" class="schedule-mini" :title="tr('sources.everyNMinutes', { n: scheduleStateFor(props.sourceKey)?.interval_min })">
          <Icon name="clock" :size="10" />
          {{ scheduleStateFor(props.sourceKey)?.interval_min }} {{ tr('sources.minuteUnit') }}
        </span>
        <span v-if="props.slots.length" class="probe-dots" :title="tr('sources.probeNote')">
          <i
            v-for="(slot, idx) in props.slots"
            :key="idx"
            class="probe-dot"
            :class="dotClass(slot)"
            :title="dotTitle(slot)"
          ></i>
        </span>
        <span v-if="props.cooling" class="probe-mini cooling">
          <Icon name="globe" :size="10" />
          {{ props.cooling }}
        </span>
      </div>
      <div class="card-header-right">
        <span v-if="isRunning(props.sourceKey) || isPaused(props.sourceKey)" class="mini-progress-text">{{ formatProgress(props.sourceKey) }}</span>
        <span class="expand-arrow">{{ props.expanded ? '▴' : '▾' }}</span>
      </div>
    </div>

    <!-- 折叠时的迷你进度条（仅运行时显示） -->
    <div v-if="(isRunning(props.sourceKey) || isPaused(props.sourceKey)) && !props.expanded" class="mini-progress">
      <div class="mini-progress-track">
        <div class="mini-progress-fill" :style="{ width: progressFor(props.sourceKey) + '%' }"></div>
      </div>
      <span class="mini-progress-pct">{{ progressFor(props.sourceKey) }}%</span>
    </div>

    <!-- 操作按钮行（始终可见） -->
    <div class="card-actions">
      <!-- 模式选择下拉 -->
      <SelectDropdown :model-value="props.mode" :options="[{ value: 'full', label: tr('sources.fullCollect') }, { value: 'incremental', label: tr('sources.incrementalCollect') }, { value: 'once', label: tr('sources.onceCollect') }]" @update:model-value="(v: any) => { emit('update-mode', String(v)) }" :disabled="isRunning(props.sourceKey) || isPaused(props.sourceKey)" size="sm" />
      <input
        v-if="props.mode === 'incremental'"
        type="number"
        class="hours-input-small"
        min="1" max="168"
        :value="props.hours || props.source.collect_hours || 24"
        @input="(e: Event) => emit('update-hours', Number((e.target as HTMLInputElement).value))"
        placeholder="h"
        :disabled="isRunning(props.sourceKey) || isPaused(props.sourceKey)"
        :title="tr('sources.lookbackHours')"
      />
      <span v-if="props.mode === 'incremental'" class="hours-suffix">{{ tr('sources.hourUnit') }}</span>

      <div class="action-spacer"></div>

      <template v-if="isRunning(props.sourceKey) || isPaused(props.sourceKey)">
        <button class="icon-btn pause-btn" @click="isPaused(props.sourceKey) ? collectStore.resume(props.sourceKey) : collectStore.pause(props.sourceKey)" :title="isPaused(props.sourceKey) ? tr('sources.resume') : tr('sources.pause')">
          <Icon :name="isPaused(props.sourceKey) ? 'play' : 'pause'" :size="14" />
        </button>
        <button class="icon-btn stop-btn" @click="collectStore.stop(props.sourceKey)" :title="tr('common.stop')">
          <Icon name="stop" :size="14" />
        </button>
      </template>
      <template v-else>
        <button class="icon-btn play-btn" @click="emit('collect')" :title="tr('sources.startCollect')">
          <Icon name="play" :size="14" />
        </button>
        <button class="icon-btn incr-btn" @click="emit('collect-incremental')" :title="tr('sources.incrementalCollect')">
          <Icon name="refresh" :size="14" />
        </button>
        <button class="icon-btn export-btn" @click="emit('export')" :title="tr('common.export')">
          <Icon name="download" :size="14" />
        </button>
      </template>
      <button class="icon-btn probe-btn" :disabled="props.probeBusy" @click="emit('probe')" :title="tr('sources.probe')">
        <Icon name="globe" :size="14" />
      </button>
      <button class="icon-btn sched-btn" @click="emit('schedule-toggle')" :title="scheduleStateFor(props.sourceKey)?.enabled ? tr('sources.scheduleEnabledTitle') : tr('sources.scheduleConfigTitle')">
        <Icon name="clock" :size="14" />
      </button>
      <button class="icon-btn default-btn" :class="{ active: props.isDefault }" @click="emit('set-default')" :title="props.isDefault ? tr('sources.isDefault') : tr('sources.setAsDefault')">
        <Icon name="star" :size="14" />
      </button>
      <button class="icon-btn edit-btn" @click="emit('edit')" :title="tr('common.edit')">
        <Icon name="edit" :size="14" />
      </button>
      <button class="icon-btn del-btn" @click="emit('delete')" :title="tr('common.delete')">
        <Icon name="trash" :size="14" />
      </button>
    </div>

    <!-- 展开内容 -->
    <div v-if="props.expanded" class="card-expanded">
      <SourceCollectPanel :source-key="props.sourceKey" />
      <SourceLogPanel :source-key="props.sourceKey" />
      <SourceOpsPanel :source-key="props.sourceKey" />
      <SourceSchedulePanel
        :source-key="props.sourceKey"
        :visible="props.scheduleVisible"
        :form="props.scheduleForm"
        @toggle="emit('schedule-toggle')"
        @save="emit('schedule-save')"
      />
      <SourceParamsGuide
        :source-key="props.sourceKey"
        :api-url="props.source.api_url"
        :doc="props.doc"
        :doc-loading="props.docLoading"
        @toggle="emit('params-toggle')"
      />
    </div>
  </div>
</template>

<style scoped>

/* === 卡片 === */
.source-card {
  background: rgba(255,255,255,0.05);
  border: 1px solid var(--border);
  border-radius: 12px;
  transition: all 0.25s ease;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}
.source-card:hover {
  transform: translateY(-2px);
  box-shadow: 0 8px 24px rgba(0,0,0,0.2);
  border-color: rgba(255,255,255,0.12);
}
.source-card.expanded { border-color: var(--accent); box-shadow: 0 0 0 1px var(--accent-alpha-20), 0 8px 24px rgba(0,0,0,0.25); }
.source-card.running { border-color: var(--accent-alpha-30); }

/* === 卡片头部 === */
.card-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 16px 18px;
  cursor: pointer;
  user-select: none;
  transition: background 0.15s ease;
}
.card-header:hover { background: rgba(255,255,255,0.03); }
.card-header-left {
  display: flex;
  align-items: center;
  gap: 10px;
  flex: 1;
  min-width: 0;
}
.card-header-right {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}
.status-dot {
  width: 10px; height: 10px; border-radius: 50%;
  flex-shrink: 0;
  background: #757575;
}
.status-dot.running {
  background: #4caf50;
  box-shadow: 0 0 8px rgba(76, 175, 80, 0.6);
  animation: dotPulse 1.5s ease-in-out infinite;
}
@keyframes dotPulse {
  0%, 100% { box-shadow: 0 0 4px rgba(76, 175, 80, 0.4); }
  50% { box-shadow: 0 0 14px rgba(76, 175, 80, 0.8); }
}
.status-dot.paused { background: var(--warning); box-shadow: 0 0 6px var(--warning-alpha-10); }
.status-dot.warning { background: var(--warning); opacity: 0.75; }
.status-dot.error { background: var(--danger); box-shadow: 0 0 6px var(--danger-alpha-10); }
.status-dot.idle { background: var(--text-muted); }
.card-name {
  font-size: 15px; font-weight: 600; margin: 0;
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
}
.mode-badge {
  padding: 3px 10px; border-radius: 12px;
  font-size: 11px; font-weight: 600; flex-shrink: 0;
}
.mode-badge.full { background: var(--accent-alpha-15); color: var(--accent); }
.mode-badge.incremental { background: var(--success-alpha-10); color: var(--success); }
.mode-badge.once { background: var(--warning-alpha-10); color: var(--warning-text); }
.schedule-mini {
  display: inline-flex; align-items: center; gap: 3px;
  font-size: 10px; color: var(--success);
  background: var(--success-alpha-10);
  padding: 2px 8px; border-radius: 10px;
  flex-shrink: 0;
}
.probe-mini {
  display: inline-flex; align-items: center; gap: 3px;
  font-size: 10px;
  padding: 2px 8px; border-radius: 10px;
  flex-shrink: 0;
  font-family: 'SF Mono', Consolas, monospace;
}
.probe-mini.cooling { color: var(--text-muted); background: var(--bg-hover); }
/* 探测点阵：一个点 = 5 分钟时间窗。灰点刻意用文字灰而不是边框色——
   深色主题下边框类 token 太暗，"没探过"会糊成看不见。 */
.probe-dots {
  display: inline-flex; align-items: center; gap: 3px; flex-shrink: 0;
  padding: 4px 6px; margin: -4px -6px; border-radius: 10px;
  transition: background 0.15s ease;
}
.probe-dot {
  position: relative;
  width: 6px; height: 6px; border-radius: 50%;
  transition: transform 0.12s ease;
}
.probe-dot.none { background: var(--text-muted); opacity: 0.35; }
.probe-dot.ok { background: var(--success); }
.probe-dot.bad { background: var(--danger); }
/* 悬停单个点才放大并浮到相邻点之上：整段一起放大会连成一条香肠，
   也答不出"我现在看的是哪一格"。整段只垫底色，表示这一行在被读。 */
.probe-dots:hover { background: var(--bg-hover); }
.probe-dot:hover { transform: scale(2); z-index: 1; }
/* 点本体只有 6px，鼠标几乎点不中；用一层透明伪元素把命中区扩到整格间距
   （9px 宽 = 6px 点 + 3px 缝，18px 高），圆点的视觉大小和排布都不变。 */
.probe-dot::after {
  content: '';
  position: absolute;
  inset: -6px -1.5px;
}
.mini-progress-text {
  font-size: 12px; color: var(--accent); font-weight: 600;
  font-family: 'SF Mono', Consolas, monospace;
}
.expand-arrow {
  font-size: 13px; color: var(--text-muted);
  transition: transform 0.2s ease;
  width: 20px; text-align: center;
}

/* === 迷你进度条 === */
.mini-progress {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 0 18px 8px;
}
.mini-progress-track {
  flex: 1; height: 6px;
  background: rgba(255,255,255,0.08);
  border-radius: 3px; overflow: hidden;
}
.mini-progress-fill {
  height: 100%;
  background: linear-gradient(90deg, var(--accent), #818cf8);
  border-radius: 3px;
  transition: width 0.4s ease;
  animation: progressShimmer 2s ease-in-out infinite;
}
@keyframes progressShimmer {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.7; }
}
.mini-progress-pct {
  font-size: 12px; color: var(--accent); font-weight: 600;
  font-family: 'SF Mono', Consolas, monospace;
  min-width: 36px; text-align: right;
}

/* === 操作按钮行 === */
.card-actions {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 10px 18px 14px;
  border-top: 1px solid rgba(255,255,255,0.06);
}
.mode-select {
  padding: 5px 10px;
  border-radius: 6px;
  border: 1px solid var(--border);
  background: rgba(255,255,255,0.05);
  color: var(--text-primary);
  font-size: 12px;
  cursor: pointer;
  outline: none;
  font-weight: 500;
}
.mode-select:focus { border-color: var(--accent); }
.mode-select:disabled { opacity: 0.4; cursor: not-allowed; }
.hours-input-small {
  width: 48px; padding: 5px 8px;
  border-radius: 6px;
  border: 1px solid var(--border);
  background: rgba(255,255,255,0.05);
  color: var(--text-primary);
  font-size: 12px; text-align: center;
  font-family: 'SF Mono', Consolas, monospace;
  -moz-appearance: textfield;
}
.hours-input-small::-webkit-inner-spin-button,
.hours-input-small::-webkit-outer-spin-button {
  -webkit-appearance: none;
  margin: 0;
}
.hours-input-small:focus { border-color: var(--accent); outline: none; box-shadow: 0 0 0 2px var(--accent-alpha-10); }
.hours-input-small:disabled { opacity: 0.4; cursor: not-allowed; }
.hours-suffix { font-size: 11px; color: var(--text-muted); }
.action-spacer { flex: 1; }
.icon-btn {
  width: 34px; height: 34px;
  border-radius: 8px;
  border: 1px solid rgba(255,255,255,0.08);
  background: rgba(255,255,255,0.04);
  color: var(--text-secondary);
  cursor: pointer;
  display: inline-flex; align-items: center; justify-content: center;
  transition: all 0.15s ease;
  flex-shrink: 0;
}
.icon-btn:hover { background: rgba(255,255,255,0.1); color: var(--text-primary); }
.icon-btn:disabled { opacity: 0.4; cursor: not-allowed; }
.probe-btn:hover:not(:disabled) { border-color: var(--accent); }
.play-btn, .incr-btn, .sched-btn { color: var(--success); border-color: var(--success-alpha-10); }
.play-btn:hover, .incr-btn:hover, .sched-btn:hover { background: var(--success-alpha-10); border-color: var(--success); }
.pause-btn { color: var(--warning-text); border-color: var(--warning-alpha-10); }
.pause-btn:hover { background: var(--warning-alpha-10); border-color: var(--warning); }
.stop-btn, .del-btn { color: var(--danger); border-color: var(--danger-alpha-10); }
.stop-btn:hover, .del-btn:hover { background: var(--danger-alpha-10); border-color: var(--danger); }
.export-btn { color: var(--accent); border-color: var(--accent-alpha-25); }
.export-btn:hover { background: var(--accent-alpha-10); border-color: var(--accent); }
.edit-btn { color: var(--accent); border-color: var(--accent-alpha-25); }
.edit-btn:hover { background: var(--accent-alpha-10); border-color: var(--accent); }

/* === 展开内容 === */
.card-expanded {
  padding: 0 18px 16px;
  display: flex;
  flex-direction: column;
  gap: 14px;
  animation: expandIn 0.25s ease;
}
@keyframes expandIn {
  from { opacity: 0; transform: translateY(-6px); }
  to { opacity: 1; transform: translateY(0); }
}

</style>
